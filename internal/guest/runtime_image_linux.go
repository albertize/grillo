//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/albertize/grillo/internal/guestproto"
	"golang.org/x/sys/unix"
)

const maxRuntimeFile = 64 << 20
const maxRuntimeArchive = 128 << 20

// RuntimeImageConfig consumes already-provisioned inputs. No downloads or source
// execution occur. Store entries are immutable and keyed by all input identities.
type RuntimeImageConfig struct {
	Agent, Kernel, Inputs, Store   string
	Version, Commit, KernelVersion string
}

type RuntimeBuild struct {
	SchemaVersion int        `json:"schema_version"`
	GuestABI      string     `json:"guest_abi"`
	Platform      string     `json:"platform"`
	Version       string     `json:"grillo_version"`
	Commit        string     `json:"commit"`
	KernelVersion string     `json:"kernel_version"`
	Toolchain     string     `json:"toolchain"`
	Recipe        string     `json:"recipe"`
	Components    []Artifact `json:"components"`
}

type runtimeInput struct{ path, target, version, digest string }

// Bytes selected from the previously pinned amd64 BusyBox OCI image; only the
// network utility and its ELF dependencies are used, never application roots.
// Redistribution/source/license review remains a separate release gate.
var runtimeInputs = []runtimeInput{
	{"runc", "runc", "runc/1.5.2", "599f6f94ff8c5057241eff0d54c3c74f95c34935b6457b33fe545defc61e9488"},
	{"rootfs/bin/busybox", "bin/busybox", "busybox/1.37@sha256:bdf57e528e45e4433820e045b29b4597825a1c9e38353532d90a01445013f82e", "786295804cdc6f04ce18325a96b2a7bf17ecc29d89fbb76d23b6f46370deb451"},
	{"rootfs/lib/ld-linux-x86-64.so.2", "lib/ld-linux-x86-64.so.2", "glibc/2.41-12+deb13u4", "c8438e4fde1934e61c88311633f00949ff645d5c04cdb8671fa3d78164d2f307"},
	{"rootfs/lib/libc.so.6", "lib/libc.so.6", "glibc/2.41-12+deb13u4", "9792e3cbb541c8f44c7acf5f14f4022ea62998ecc787d326bed4d8b6547dfd92"},
	{"rootfs/lib/libm.so.6", "lib/libm.so.6", "glibc/2.41-12+deb13u4", "6d567d53e895273ca14a1f9dc164fc6c8d39aed2f60aa46a733c2784228915f3"},
	{"rootfs/lib/libresolv.so.2", "lib/libresolv.so.2", "glibc/2.41-12+deb13u4", "d41dd4adacf5a35df426461dd8a92d7bdd5ba29bb61a1c3531bec0ada386f8dd"},
}

type imageEntry struct {
	name string
	mode uint32
	data []byte
}

func runtimeDirectories() []imageEntry {
	names := []string{".", "bin", "lib", "etc", "etc/grillo", "proc", "sys", "sys/fs", "sys/fs/cgroup", "dev", "tmp", "run"}
	var entries []imageEntry
	for _, name := range names {
		mode := uint32(0040755)
		if name == "tmp" {
			mode = 0041777
		}
		entries = append(entries, imageEntry{name, mode, nil})
	}
	return append(entries, imageEntry{"lib64", 0120777, []byte("lib")})
}

// BuildRuntimeImage constructs a fixture-free/key-free base and atomically
// publishes one immutable kernel/image/inventory tree. Concurrent builders may
// reuse a verified identical winner; incomplete or different entries fail closed.
func BuildRuntimeImage(ctx context.Context, cfg RuntimeImageConfig) (string, error) {
	return buildRuntimeImage(ctx, cfg, runtimeInputs)
}

func readRuntimeFile(ctx context.Context, file *os.File) ([]byte, error) {
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxRuntimeFile {
		return nil, fmt.Errorf("guest: invalid runtime input file")
	}
	var buffer bytes.Buffer
	chunk := make([]byte, 64<<10)
	reader := io.LimitReader(file, info.Size()+1)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := reader.Read(chunk)
		if n > 0 {
			buffer.Write(chunk[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if int64(buffer.Len()) != info.Size() {
		return nil, fmt.Errorf("guest: runtime input changed while reading")
	}
	return buffer.Bytes(), nil
}

func buildRuntimeImage(ctx context.Context, cfg RuntimeImageConfig, pins []runtimeInput) (string, error) {
	if cfg.Version == "" || cfg.Commit == "" || cfg.KernelVersion == "" || cfg.Store == "" {
		return "", fmt.Errorf("guest: explicit runtime build identity/store required")
	}
	for _, value := range []string{cfg.Version, cfg.Commit, cfg.KernelVersion} {
		if len(value) > 128 || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n") {
			return "", fmt.Errorf("guest: invalid bounded runtime build identity")
		}
	}
	build := RuntimeBuild{SchemaVersion: 1, GuestABI: guestproto.CurrentVersion.String(), Platform: GuestPlatform, Version: cfg.Version, Commit: cfg.Commit, KernelVersion: cfg.KernelVersion, Toolchain: runtime.Version(), Recipe: "guest/runtime-image:v1"}
	entries := runtimeDirectories()
	record := func(name, version string, data []byte) {
		sum := sha256.Sum256(data)
		build.Components = append(build.Components, Artifact{Name: name, Version: version, Path: name, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))})
	}
	var kernel []byte
	for _, source := range []struct{ path, name, version string }{{cfg.Agent, "init", cfg.Version}, {cfg.Kernel, "bzImage", cfg.KernelVersion}} {
		f, err := openRegular(source.path)
		if err != nil {
			return "", err
		}
		data, err := readRuntimeFile(ctx, f)
		if err != nil {
			return "", err
		}
		record(source.name, source.version, data)
		if source.name == "bzImage" {
			kernel = data
		} else {
			entries = append(entries, imageEntry{"init", 0100755, data})
		}
	}
	inputs, err := filepath.Abs(cfg.Inputs)
	if err != nil {
		return "", err
	}
	for _, pin := range pins {
		f, err := (Artifact{Path: pin.path, directory: inputs}).Open()
		if err != nil {
			return "", fmt.Errorf("guest: missing/unsafe pinned runtime component %s", pin.target)
		}
		data, err := readRuntimeFile(ctx, f)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != pin.digest {
			return "", fmt.Errorf("guest: pinned runtime component %s digest mismatch", pin.target)
		}
		record(pin.target, pin.version, data)
		entries = append(entries, imageEntry{pin.target, 0100755, data})
	}
	sort.Slice(build.Components, func(i, j int) bool { return build.Components[i].Name < build.Components[j].Name })
	metadata, err := json.MarshalIndent(build, "", "  ")
	if err != nil {
		return "", err
	}
	id := sha256.Sum256(metadata)
	store, err := filepath.Abs(cfg.Store)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(store, 0755); err != nil {
		return "", err
	}
	if err := rejectDirectorySymlinks(store); err != nil {
		return "", err
	}
	image, err := packRuntimeImage(entries)
	if err != nil {
		return "", err
	}
	if err := VerifyRuntimeImage(image, nil); err != nil {
		return "", err
	}
	destination := filepath.Join(store, hex.EncodeToString(id[:]))
	verifyExisting := func() error {
		m, err := ReadBootManifest(filepath.Join(destination, "manifest.json"), false)
		if err != nil {
			return fmt.Errorf("guest: incomplete runtime store entry: %w", err)
		}
		if err := m.Verify(); err != nil {
			return err
		}
		expected := map[string][]byte{"kernel": kernel, "initramfs": image}
		for _, a := range m.Artifacts {
			sum := sha256.Sum256(expected[a.Name])
			if a.SHA256 != hex.EncodeToString(sum[:]) || a.Size != int64(len(expected[a.Name])) {
				return fmt.Errorf("guest: conflicting runtime store entry")
			}
		}
		file, err := openRegular(filepath.Join(destination, "runtime-build.json"))
		if err != nil {
			return err
		}
		actual, err := readRuntimeFile(ctx, file)
		if err != nil || !bytes.Equal(actual, append(metadata, '\n')) {
			return fmt.Errorf("guest: runtime build inventory mismatch")
		}
		return nil
	}
	if _, err := os.Lstat(destination); err == nil {
		return destination, verifyExisting()
	} else if !os.IsNotExist(err) {
		return "", err
	}
	stage, err := os.MkdirTemp(store, ".runtime-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage) // only this operation's unique staging directory
	for name, data := range map[string][]byte{"bzImage": kernel, "initramfs.cpio.gz": image, "runtime-build.json": append(metadata, '\n')} {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		f, err := os.OpenFile(filepath.Join(stage, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			return "", err
		}
		_, writeErr := f.Write(data)
		syncErr := f.Sync()
		closeErr := f.Close()
		if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
			return "", err
		}
	}
	m, err := BuildPortableManifest(stage, []ArtifactSpec{{Name: "kernel", Version: cfg.KernelVersion, Path: filepath.Join(stage, "bzImage")}, {Name: "initramfs", Version: cfg.Version, Path: filepath.Join(stage, "initramfs.cpio.gz")}})
	if err != nil {
		return "", err
	}
	if err := m.Write(filepath.Join(stage, "manifest.json")); err != nil {
		return "", err
	}
	if err := os.Chmod(stage, 0755); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return destination, verifyExisting()
		}
		return "", err
	}
	parent, err := os.Open(store)
	if err != nil {
		return "", err
	}
	defer parent.Close()
	return destination, parent.Sync()
}

func packRuntimeImage(entries []imageEntry) ([]byte, error) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	for i, entry := range append(entries, imageEntry{"TRAILER!!!", 0, nil}) {
		if len(entry.data) > maxRuntimeFile {
			return nil, fmt.Errorf("guest: image entry too large")
		}
		namesize := len(entry.name) + 1
		header := fmt.Sprintf("070701%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x", i+1, entry.mode, 0, 0, 1, 0, len(entry.data), 0, 0, 0, 0, namesize, 0)
		if _, err := io.WriteString(gz, header+entry.name+"\x00"); err != nil {
			return nil, err
		}
		if _, err := gz.Write(make([]byte, (4-(110+namesize)%4)%4)); err != nil {
			return nil, err
		}
		if _, err := gz.Write(entry.data); err != nil {
			return nil, err
		}
		if _, err := gz.Write(make([]byte, (4-len(entry.data)%4)%4)); err != nil {
			return nil, err
		}
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return archive.Bytes(), nil
}

// VerifyRuntimeImage inspects a bounded gzip/newc image without extraction. The
// fixed namespace excludes fixtures, keys and arbitrary paths; optional known
// credential markers add regression scanning without returning secret values.
func VerifyRuntimeImage(image []byte, forbidden [][]byte) error {
	if len(image) > maxRuntimeArchive {
		return fmt.Errorf("guest: compressed runtime image too large")
	}
	reader := bytes.NewReader(image)
	gz, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("guest: invalid runtime gzip")
	}
	if !gz.ModTime.IsZero() || gz.Name != "" || gz.Comment != "" || len(gz.Extra) != 0 || gz.OS != 255 {
		gz.Close()
		return fmt.Errorf("guest: unexpected runtime gzip metadata")
	}
	gz.Multistream(false)
	data, err := io.ReadAll(io.LimitReader(gz, maxRuntimeArchive+1))
	closeErr := gz.Close()
	if err != nil || closeErr != nil || len(data) > maxRuntimeArchive || reader.Len() != 0 {
		return fmt.Errorf("guest: invalid/oversized/multistream runtime image")
	}
	for _, marker := range forbidden {
		if len(marker) > 0 && (bytes.Contains(data, marker) || bytes.Contains(image, marker)) {
			return fmt.Errorf("guest: forbidden credential marker in runtime image")
		}
	}
	allowed := map[string]uint32{"init": 0100755}
	for _, entry := range runtimeDirectories() {
		allowed[entry.name] = entry.mode
	}
	for _, pin := range runtimeInputs {
		allowed[pin.target] = 0100755
	}
	seen := map[string]bool{}
	position := 0
	for {
		if len(data)-position < 110 || string(data[position:position+6]) != "070701" {
			return fmt.Errorf("guest: invalid runtime newc header")
		}
		header := data[position : position+110]
		position += 110
		var fields [13]uint64
		for i := range fields {
			value, err := strconv.ParseUint(string(header[6+i*8:14+i*8]), 16, 32)
			if err != nil {
				return fmt.Errorf("guest: invalid runtime newc field")
			}
			fields[i] = value
		}
		mode, size, nameSize := fields[1], fields[6], fields[11]
		if fields[0] != uint64(len(seen)+1) || fields[2] != 0 || fields[3] != 0 || fields[4] != 1 || fields[5] != 0 || fields[7] != 0 || fields[8] != 0 || fields[9] != 0 || fields[10] != 0 || fields[12] != 0 || nameSize < 1 || nameSize > 256 || size > maxRuntimeFile || uint64(len(data)-position) < nameSize {
			return fmt.Errorf("guest: invalid runtime newc metadata")
		}
		nameBytes := data[position : position+int(nameSize)]
		if nameBytes[len(nameBytes)-1] != 0 {
			return fmt.Errorf("guest: unterminated runtime path")
		}
		name := string(nameBytes[:len(nameBytes)-1])
		position += int(nameSize)
		position += (4 - position%4) % 4
		if position > len(data) || uint64(len(data)-position) < size {
			return fmt.Errorf("guest: truncated runtime entry")
		}
		contents := data[position : position+int(size)]
		position += int(size)
		position += (4 - position%4) % 4
		if position > len(data) {
			return fmt.Errorf("guest: truncated runtime padding")
		}
		if name == "TRAILER!!!" {
			if mode != 0 || size != 0 || position != len(data) {
				return fmt.Errorf("guest: unexpected data after runtime trailer")
			}
			break
		}
		wanted, ok := allowed[name]
		if !ok || seen[name] || uint64(wanted) != mode {
			return fmt.Errorf("guest: unexpected runtime image entry")
		}
		seen[name] = true
		if mode&0170000 == 0040000 && size != 0 {
			return fmt.Errorf("guest: directory contains runtime data")
		}
		if name == "lib64" && !bytes.Equal(contents, []byte("lib")) {
			return fmt.Errorf("guest: unsafe runtime library symlink")
		}
	}
	if len(seen) != len(allowed) {
		return fmt.Errorf("guest: incomplete runtime image")
	}
	return nil
}
