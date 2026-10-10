//go:build linux

// SPDX-License-Identifier: Apache-2.0

// Package distribution stages a single experimental prefix from explicit,
// already-provisioned build outputs. It is not an installer or release publisher.
package distribution

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/albertize/grillo/internal/buildinfo"
	"github.com/albertize/grillo/internal/frontend/helm"
	"github.com/albertize/grillo/internal/guest"
	"github.com/albertize/grillo/internal/runtimeassets"
	"golang.org/x/sys/unix"
)

type StageConfig struct{ Output, Binaries, Guest, Helm, HelmSHA256, Examples, Documents string }
type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type Inventory struct {
	SchemaVersion        int                `json:"schema_version"`
	Experimental         bool               `json:"experimental"`
	RedistributionReview string             `json:"redistribution_review"`
	Identity             buildinfo.Info     `json:"identity"`
	HelmVersion          string             `json:"helm_version"`
	BinaryProvenance     []BinaryProvenance `json:"binary_provenance"`
	NoticeCoverage       string             `json:"notice_coverage"`
	Files                []File             `json:"files"`
}

// Stage executes only caller-provisioned trusted host build outputs to check
// versions, after copying them into a private stage. Workload manifests and guest
// files are never executed on the host. Publication never replaces any prefix.
func Stage(ctx context.Context, cfg StageConfig) error {
	if cfg.Output == "" || cfg.Binaries == "" || cfg.Guest == "" || cfg.Helm == "" || cfg.Examples == "" || cfg.Documents == "" {
		return fmt.Errorf("stage: explicit sources and output required")
	}
	if len(cfg.HelmSHA256) != 64 {
		return fmt.Errorf("stage: explicitly trusted Helm SHA-256 required")
	}
	if _, err := hex.DecodeString(cfg.HelmSHA256); err != nil {
		return fmt.Errorf("stage: invalid Helm SHA-256")
	}
	output, err := filepath.Abs(cfg.Output)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(output); err == nil {
		return fmt.Errorf("stage: output already exists; immutable prefixes are never overwritten")
	} else if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(output)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	if err := safeDirectory(parent); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".grillo-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage) // this invocation's unique stage only
	inventory := Inventory{SchemaVersion: 1, Experimental: true, RedistributionReview: "pending; do not publish this development payload", HelmVersion: helm.Version}
	add := func(source, target, expected string, mode os.FileMode) error {
		file, err := openSource(source)
		if err != nil {
			return err
		}
		return copyFile(ctx, file, filepath.Join(stage, target), mode, expected, target, &inventory)
	}
	for _, helper := range []struct{ name, path string }{{"grillo", "bin/grillo"}, {"grillod", "libexec/grillo/grillod"}, {"grillo-netns", "libexec/grillo/grillo-netns"}} {
		if err := add(filepath.Join(cfg.Binaries, helper.name), helper.path, "", 0555); err != nil {
			return err
		}
	}
	if err := add(cfg.Helm, "libexec/grillo/helm", strings.ToLower(cfg.HelmSHA256), 0555); err != nil {
		return err
	}
	for _, relative := range []string{"bin/grillo", "libexec/grillo/grillod", "libexec/grillo/grillo-netns", "libexec/grillo/helm"} {
		provenance, err := binaryProvenance(filepath.Join(stage, relative), relative)
		if err != nil {
			return err
		}
		inventory.BinaryProvenance = append(inventory.BinaryProvenance, provenance)
	}
	version, err := toolOutput(ctx, filepath.Join(stage, "bin/grillo"), []string{"version", "--json"}, stage)
	if err != nil {
		return fmt.Errorf("stage: CLI identity check failed")
	}
	if err := json.Unmarshal(version, &inventory.Identity); err != nil || inventory.Identity.Version == "" || inventory.Identity.Commit == "" || inventory.Identity.GuestABI != runtimeassets.GuestABI {
		return fmt.Errorf("stage: invalid CLI identity/ABI")
	}
	daemonVersion, err := toolOutput(ctx, filepath.Join(stage, "libexec/grillo/grillod"), []string{"-version"}, stage)
	var daemonIdentity buildinfo.Info
	if err != nil || json.Unmarshal(daemonVersion, &daemonIdentity) != nil || daemonIdentity != inventory.Identity {
		return fmt.Errorf("stage: CLI/daemon build identity mismatch")
	}
	renderer, err := toolOutput(ctx, filepath.Join(stage, "libexec/grillo/helm"), []string{"version", "--template", "{{.Version}}"}, stage)
	if err != nil || strings.TrimSpace(string(renderer)) != helm.Version {
		return fmt.Errorf("stage: renderer must be exact official Helm %s", helm.Version)
	}
	manifest, err := guest.ReadBootManifest(filepath.Join(cfg.Guest, "manifest.json"), false)
	if err != nil {
		return err
	}
	guestRelative := "lib/grillo/guest/" + runtimeassets.GuestABI
	for _, artifact := range manifest.Artifacts {
		name := "bzImage"
		if artifact.Name == "initramfs" {
			name = "initramfs.cpio.gz"
		}
		file, err := artifact.Open()
		if err != nil {
			return err
		}
		relative := guestRelative + "/" + name
		if err := copyFile(ctx, file, filepath.Join(stage, relative), 0444, artifact.SHA256, relative, &inventory); err != nil {
			return err
		}
	}
	image, err := os.ReadFile(filepath.Join(stage, guestRelative, "initramfs.cpio.gz"))
	if err != nil {
		return err
	}
	if err := guest.VerifyRuntimeImage(image, nil); err != nil {
		return fmt.Errorf("stage: guest is not a fixture-free runtime base: %w", err)
	}
	if err := add(filepath.Join(cfg.Guest, "runtime-build.json"), guestRelative+"/runtime-build.json", "", 0444); err != nil {
		return err
	}
	recordData, err := os.ReadFile(filepath.Join(stage, guestRelative, "runtime-build.json"))
	if err != nil || len(recordData) > 1<<20 {
		return fmt.Errorf("stage: oversized runtime build inventory")
	}
	var record guest.RuntimeBuild
	decoder := json.NewDecoder(bytes.NewReader(recordData))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil || record.SchemaVersion != 1 || record.GuestABI != runtimeassets.GuestABI || record.Platform != guest.GuestPlatform || record.Version != inventory.Identity.Version || record.Commit != inventory.Identity.Commit {
		return fmt.Errorf("stage: host/guest build identity mismatch")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("stage: trailing runtime build inventory")
	}
	var bootSpecs []guest.ArtifactSpec
	for _, a := range manifest.Artifacts {
		name := "bzImage"
		if a.Name == "initramfs" {
			name = "initramfs.cpio.gz"
		}
		bootSpecs = append(bootSpecs, guest.ArtifactSpec{Name: a.Name, Version: a.Version, Path: filepath.Join(stage, guestRelative, name)})
	}
	portable, err := guest.BuildPortableManifest(filepath.Join(stage, guestRelative), bootSpecs)
	if err != nil {
		return err
	}
	if err := portable.Write(filepath.Join(stage, guestRelative, "manifest.json")); err != nil {
		return err
	}
	if err := recordFile(stage, guestRelative+"/manifest.json", &inventory); err != nil {
		return err
	}
	if err := copyExamples(ctx, cfg.Examples, stage, &inventory); err != nil {
		return err
	}
	for _, name := range []string{"LICENSE", "NOTICE"} {
		if err := add(filepath.Join(cfg.Documents, name), "share/doc/grillo/"+name, "", 0444); err != nil {
			return err
		}
	}
	if err := add(filepath.Join(cfg.Documents, "docs/runtime-layout.md"), "share/doc/grillo/runtime-layout.md", "", 0444); err != nil {
		return err
	}
	if err := copyRuntimeNotices(ctx, cfg.Documents, stage, &inventory); err != nil {
		return err
	}
	if err := add(filepath.Join(cfg.Documents, "docs/host-support.md"), "share/doc/grillo/host-support.md", "", 0444); err != nil {
		return err
	}
	sort.Slice(inventory.Files, func(i, j int) bool { return inventory.Files[i].Path < inventory.Files[j].Path })
	data, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return err
	}
	versionPath := filepath.Join(stage, "share/doc/grillo/VERSION.json")
	if err := os.WriteFile(versionPath, append(data, '\n'), 0444); err != nil {
		return err
	}
	if err := os.Chmod(stage, 0755); err != nil {
		return err
	}
	if err := syncTree(stage); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, output, unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("stage: atomic publication refused: %w", err)
	}
	dir, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func safeDirectory(path string) error {
	for p := path; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("stage: unsafe source/output directory")
		}
		if p == filepath.Dir(p) {
			return nil
		}
	}
}
func openSource(path string) (*os.File, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := safeDirectory(filepath.Dir(absolute)); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(absolute, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("stage: expected regular source file")
	}
	return file, nil
}
func copyFile(ctx context.Context, source *os.File, target string, mode os.FileMode, expected, relative string, inventory *Inventory) error {
	defer source.Close()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > 128<<20 {
		return fmt.Errorf("stage: invalid/oversized source file")
	}
	total := info.Size()
	for _, file := range inventory.Files {
		total += file.Size
	}
	if total > 512<<20 {
		return fmt.Errorf("stage: aggregate payload limit")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	hash := sha256.New()
	reader := io.LimitReader(source, info.Size()+1)
	writer := io.MultiWriter(out, hash)
	buffer := make([]byte, 64<<10)
	var size int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := reader.Read(buffer)
		if n > 0 {
			if _, err := writer.Write(buffer[:n]); err != nil {
				return err
			}
			size += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if size != info.Size() || (expected != "" && expected != digest) {
		return fmt.Errorf("stage: source changed or digest mismatch for %s", relative)
	}
	if err := errors.Join(out.Sync(), out.Close()); err != nil {
		return err
	}
	inventory.Files = append(inventory.Files, File{relative, digest, size})
	return nil
}
func recordFile(root, relative string, inventory *Inventory) error {
	digest, size, err := guest.HashFile(filepath.Join(root, relative))
	if err != nil {
		return err
	}
	inventory.Files = append(inventory.Files, File{relative, digest, size})
	return nil
}
func copyExamples(ctx context.Context, source, stage string, inventory *Inventory) error {
	count, entries := 0, 0
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		entries++
		if entries > 256 {
			return fmt.Errorf("stage: example directory entry limit")
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("stage: example symlink rejected")
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("stage: example special file rejected")
		}
		count++
		if count > 128 {
			return fmt.Errorf("stage: example entry limit")
		}
		info, err := entry.Info()
		if err != nil || info.Size() > 1<<20 {
			return fmt.Errorf("stage: example size limit")
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || !fs.ValidPath(filepath.ToSlash(relative)) {
			return fmt.Errorf("stage: invalid example path")
		}
		file, err := openSource(path)
		if err != nil {
			return err
		}
		target := "share/grillo/examples/" + filepath.ToSlash(relative)
		return copyFile(ctx, file, filepath.Join(stage, target), 0444, "", target, inventory)
	})
}
func syncTree(root string) error {
	var paths []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, path)
		return nil
	}); err != nil {
		return err
	}
	for i := len(paths) - 1; i >= 0; i-- {
		file, err := os.Open(paths[i])
		if err != nil {
			return err
		}
		syncErr := file.Sync()
		closeErr := file.Close()
		if err := errors.Join(syncErr, closeErr); err != nil {
			return err
		}
	}
	return nil
}

type limitedOutput struct{ buffer bytes.Buffer }

func (b *limitedOutput) Write(data []byte) (int, error) {
	if b.buffer.Len()+len(data) > 4096 {
		return 0, fmt.Errorf("version output limit")
	}
	return b.buffer.Write(data)
}
func toolOutput(ctx context.Context, path string, args []string, directory string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	workspace, err := os.MkdirTemp(filepath.Dir(directory), ".grillo-identity-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workspace)
	command := exec.CommandContext(ctx, path, args...)
	command.WaitDelay = 500 * time.Millisecond // bound inherited output pipes after process exit/cancellation
	command.Dir = workspace
	directory = workspace
	command.Env = []string{"HOME=" + directory, "LANG=C", "LC_ALL=C", "HELM_PLUGINS=" + filepath.Join(directory, "no-plugins"), "HELM_CONFIG_HOME=" + filepath.Join(directory, "no-config"), "HELM_DATA_HOME=" + filepath.Join(directory, "no-data"), "HELM_CACHE_HOME=" + filepath.Join(directory, "no-cache")}
	var output limitedOutput
	command.Stdout = &output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return nil, err
	}
	return output.buffer.Bytes(), nil
}
