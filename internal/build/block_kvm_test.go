//go:build kvm

// SPDX-License-Identifier: Apache-2.0

package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/backend/qemu"
	"github.com/albertize/grillo/internal/guest"
	"github.com/albertize/grillo/internal/guestproto"
	linux "github.com/albertize/grillo/internal/platform/linux"
	"github.com/albertize/grillo/internal/sandbox"
)

// Remember only handles created by this test's private backend.
type blockKVMBackend struct {
	*qemu.Backend
	last    sandbox.Handle
	t       *testing.T
	dir     string
	logs    string
	created map[string]bool
}

func (b *blockKVMBackend) Create(ctx context.Context, spec sandbox.Spec, op sandbox.OperationID) (sandbox.Handle, error) {
	if !filepath.IsLocal(spec.ID) || filepath.Base(spec.ID) != spec.ID || spec.ID == "." || len(b.created) >= 32 && !b.created[spec.ID] {
		return sandbox.Handle{}, errors.New("invalid or excessive diagnostic guest fixture")
	}
	handle, err := b.Backend.Create(ctx, spec, op)
	if err == nil {
		b.last = handle
		b.created[handle.ID] = true
	}
	return handle, err
}

func (b *blockKVMBackend) Delete(ctx context.Context, handle sandbox.Handle) error {
	if b.created[handle.ID] {
		// Preserve only bounded serial/helper logs of this operation's guests.
		// Never copy sandbox.json or boot-key/snapshot credentials.
		for _, name := range []string{"serial.log", "vmm.log"} {
			f, err := os.OpenFile(filepath.Join(b.dir, handle.ID, name), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				b.t.Error("open owned diagnostic log", err)
				continue
			}
			info, statErr := f.Stat()
			if statErr != nil || !info.Mode().IsRegular() {
				_ = f.Close()
				b.t.Error("invalid owned diagnostic log", statErr)
				continue
			}
			data, readErr := io.ReadAll(io.LimitReader(f, 64<<10))
			closeErr := f.Close()
			if err := errors.Join(readErr, closeErr, os.WriteFile(filepath.Join(b.logs, handle.ID+"-"+name), data, 0600)); err != nil {
				b.t.Error("save owned diagnostic log", err)
			}
		}
	}
	return b.Backend.Delete(ctx, handle)
}

type blockKVMClient struct {
	SandboxClient
	t      *testing.T
	events string
}

func (c blockKVMClient) record(phase string, started time.Time, err error) {
	event := struct {
		Phase      string `json:"phase"`
		DurationMS int64  `json:"duration_ms"`
		Error      string `json:"error,omitempty"`
	}{Phase: phase, DurationMS: time.Since(started).Milliseconds()}
	if err != nil {
		event.Error = err.Error()
		if len(event.Error) > 2048 {
			event.Error = event.Error[:2048]
		}
	}
	data, marshalErr := json.Marshal(event)
	f, openErr := os.OpenFile(c.events, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if openErr != nil {
		c.t.Error("open phase diagnostics", openErr)
		return
	}
	if info, err := f.Stat(); err != nil || info.Size()+int64(len(data))+1 > 64<<10 {
		_ = f.Close()
		c.t.Error("phase diagnostic limit or stat failure", err)
		return
	}
	_, writeErr := f.Write(append(data, '\n'))
	c.t.Helper()
	if err := errors.Join(marshalErr, writeErr, f.Close()); err != nil {
		c.t.Error("save phase diagnostics", err)
	}
}

func (c blockKVMClient) Run(ctx context.Context, req guestproto.RunRequest, stdout, stderr io.Writer) (guestproto.RunResult, error) {
	started := time.Now()
	result, err := c.SandboxClient.Run(ctx, req, stdout, stderr)
	c.record("run", started, err)
	return result, err
}

func (c blockKVMClient) Export(ctx context.Context, req guestproto.ExportRequest, output io.Writer) (guestproto.ExportResult, error) {
	started := time.Now()
	result, err := c.SandboxClient.(snapshotExporter).Export(ctx, req, output)
	c.record("export", started, err)
	return result, err
}

func TestBlockKVMDiagnosticsAreBoundedPrivateAndExcludeCredentials(t *testing.T) {
	root := t.TempDir()
	dir, logs := filepath.Join(root, "backend"), filepath.Join(root, "diagnostics")
	if err := os.Mkdir(logs, 0700); err != nil {
		t.Fatal(err)
	}
	backend, err := qemu.Open(qemu.Config{WorkDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	tracked := &blockKVMBackend{Backend: backend, t: t, dir: dir, logs: logs, created: map[string]bool{}}
	ctx := context.Background()
	handle, err := tracked.Create(ctx, sandbox.Spec{ID: "owned-diagnostics", Kernel: "/prepared/kernel", Initramfs: "/prepared/initrd", VsockPort: 1024, GuestKey: bytes.Repeat([]byte{1}, 32)}, "diagnostic-only")
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"serial.log": bytes.Repeat([]byte("x"), 128<<10), "vmm.log": []byte("helper log"), "boot-key": []byte("synthetic-credential-never-copy")} {
		if err := os.WriteFile(filepath.Join(dir, handle.ID, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := tracked.Delete(ctx, handle); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(logs)
	if err != nil || len(entries) != 2 {
		t.Fatal("unexpected diagnostic contents", entries, err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(logs, entry.Name()))
		if err != nil || len(data) > 64<<10 || bytes.Contains(data, []byte("synthetic-credential")) {
			t.Fatal("diagnostic bound/credential exclusion failed", err)
		}
		info, err := entry.Info()
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("diagnostic privacy failed", err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, handle.ID)); !os.IsNotExist(err) {
		t.Fatal("diagnostics prevented owned cleanup", err)
	}
}

// This gate fails rather than skips when hardware/artifacts are missing.
func TestKVMNativeBlockWorkspace(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Fatal("BLOCKED: /dev/kvm unavailable")
	}
	formatter, err := exec.LookPath("mke2fs")
	if err != nil {
		t.Fatal("BLOCKED: mke2fs unavailable")
	}
	formatter, err = filepath.Abs(formatter)
	if err != nil {
		t.Fatal(err)
	}
	version, err := exec.Command(formatter, "-V").CombinedOutput()
	if err != nil || !strings.HasPrefix(string(version), "mke2fs 1.47.3 ") {
		t.Fatal("BLOCKED: reviewed mke2fs 1.47.3 required")
	}
	root := buildRepoRoot(t)
	kernel := filepath.Join(root, "experiments/artifacts/qemu/bzImage")
	initrd := filepath.Join(root, "experiments/artifacts/t07/initramfs-agent.cpio.gz")
	keyPath := filepath.Join(root, "experiments/artifacts/t07/key")
	busybox := filepath.Join(root, "experiments/artifacts/t02/rootfs/bin/busybox")
	manifestPath := ""
	if selection := os.Getenv("GRILLO_TEST_BUILD_GUEST_SELECTION"); selection != "" {
		f, err := os.Open(selection)
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(io.LimitReader(f, 4097))
		closeErr := f.Close()
		if readErr != nil || closeErr != nil || len(data) > 4096 || !filepath.IsAbs(strings.TrimSpace(string(data))) {
			t.Fatal("invalid explicit build guest selection", readErr, closeErr)
		}
		dir := strings.TrimSpace(string(data))
		manifestPath = filepath.Join(dir, "manifest.json")
		manifest, err := guest.ReadBootManifest(manifestPath, false)
		if err != nil {
			t.Fatal(err)
		}
		if err := manifest.Verify(); err != nil {
			t.Fatal(err)
		}
		kernel, initrd = filepath.Join(dir, "bzImage"), filepath.Join(dir, "initramfs.cpio.gz")
		t.Log("MODE: explicitly selected portable guest with verified boot snapshots and fresh keys")
	}
	for _, name := range []string{kernel, initrd, busybox} {
		if _, err := os.Stat(name); err != nil {
			t.Fatalf("BLOCKED: prerequisite missing: %s", name)
		}
	}
	var key []byte
	if manifestPath == "" {
		keyData, err := os.ReadFile(keyPath)
		if err != nil {
			t.Fatal(err)
		}
		key, err = base64.StdEncoding.DecodeString(strings.TrimSpace(string(keyData)))
		if err != nil {
			t.Fatal(err)
		}
	}
	work, err := os.MkdirTemp("", "grillo-block-kvm-")
	if err != nil {
		t.Fatal(err)
	}
	success := false
	defer func() {
		if success && !t.Failed() {
			if err := os.RemoveAll(work); err != nil {
				t.Error(err)
			}
		} else {
			t.Logf("failed owned state retained: %s", work)
		}
	}()
	backend, err := qemu.Open(qemu.Config{Kernel: kernel, Initramfs: initrd, ArtifactManifest: manifestPath, BootKeyOverlay: manifestPath != "", RequirePortableManifest: manifestPath != "", WorkDir: filepath.Join(work, "backend"), CIDBase: 700, BootTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	logs := filepath.Join(work, "diagnostics")
	if err := os.Mkdir(logs, 0700); err != nil {
		t.Fatal(err)
	}
	tracked := &blockKVMBackend{Backend: backend, t: t, dir: filepath.Join(work, "backend"), logs: logs, created: map[string]bool{}}
	boot := &GuestBoot{Backend: tracked, Kernel: kernel, Initramfs: initrd, KernelArgs: "console=ttyS0 reboot=k panic=1 rdinit=/init", GuestKey: key, FreshKeys: manifestPath != "", VsockCIDBase: 700, VsockPort: 1024, MemoryMiB: 512}
	bootBlock := func(ctx context.Context, disk, input string, seed *guestproto.BuildSeedSpec, network bool) (SandboxClient, func() error, error) {
		started := time.Now()
		client, cleanup, err := boot.BootBlock(ctx, disk, input, seed, network)
		diagnostic := blockKVMClient{SandboxClient: client, t: t, events: filepath.Join(logs, "phases.jsonl")}
		diagnostic.record("boot", started, err)
		if err != nil {
			return client, cleanup, err
		}
		return diagnostic, cleanup, nil
	}
	b, cas, puller := newNativeBuilder(t, nil)
	b.ScratchDir = filepath.Join(work, "scratch")
	if err := os.Mkdir(b.ScratchDir, 0700); err != nil {
		t.Fatal(err)
	}
	b.Runner = &BlockRunner{Formatter: formatter, ImageBytes: 32 << 20, Boot: bootBlock}
	contextDir := filepath.Join(work, "context")
	if err := os.Mkdir(contextDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(busybox, filepath.Join(contextDir, "busybox"), copyOptions{}); err != nil {
		t.Fatal(err)
	}
	// The pinned BusyBox fixture needs its guest libraries in the scratch image.
	libraryCopies := ""
	for _, dir := range []string{"lib", "lib64", "usr/lib", "usr/lib64"} {
		source := filepath.Join(root, "experiments/artifacts/t02/rootfs", dir)
		if _, err := os.Stat(source); os.IsNotExist(err) {
			continue
		} else if err != nil {
			t.Fatal(err)
		}
		if err := copyTree(source, filepath.Join(contextDir, dir), copyOptions{}); err != nil {
			t.Fatal(err)
		}
		libraryCopies += "COPY " + dir + " /" + dir + "\n"
	}
	quota := []string{"/bin/busybox", "sh", "-c", `out=$(/bin/busybox dd if=/dev/zero of=/fill bs=1048576 count=64 2>&1); rc=$?; test "$rc" -ne 0 || exit 10; case "$out" in *"No space left on device"*) ;; *) exit 11;; esac; /bin/busybox rm /fill; echo quota-ok > /result`}
	second := []string{"/bin/busybox", "sh", "-c", "/bin/busybox cat /result /after > /out; /bin/busybox rm /before; /bin/busybox chown 1234:1235 /out; /bin/busybox chmod 4750 /out; /bin/busybox chmod 0750 /"}
	q, _ := json.Marshal(quota)
	s, _ := json.Marshal(second)
	dockerfile := "FROM scratch\nCOPY busybox /bin/busybox\n" + libraryCopies + "COPY before /before\nRUN " + string(q) + "\nCOPY after /after\nRUN " + string(s) + "\n"
	for name, data := range map[string]string{"Dockerfile": dockerfile, "before": "before", "after": "after"} {
		if err := os.WriteFile(filepath.Join(contextDir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	result, err := b.Build(ctx, Request{ContextDir: contextDir, Reference: "grillo.local/kvm-block:1", Network: "none"}, func(line string) { t.Log(line) })
	if err != nil {
		t.Fatal(err)
	}
	imageRoot := unpackImage(t, puller, result.Image)
	if got := readFile(t, imageRoot, "out"); got != "quota-ok\nafter" {
		t.Fatalf("unexpected interleaved result: %q", got)
	}
	if _, err := os.Stat(filepath.Join(imageRoot, "before")); !os.IsNotExist(err) {
		t.Fatal("RUN deletion lost", err)
	}
	// Inspect the published layer: ordinary host unpack deliberately clears
	// privilege bits and cannot prove numeric guest ownership.
	layers := result.Image.Manifest.Layers
	f, err := cas.Open(layers[len(layers)-1].Digest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	foundRoot, foundOutput := false, false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch h.Name {
		case ".", "./":
			foundRoot = true
			if h.Mode != 0750 || h.Uid != 0 || h.Gid != 0 {
				t.Fatalf("root metadata lost: %+v", h)
			}
		case "out":
			foundOutput = true
			if h.Mode != 04750 || h.Uid != 1234 || h.Gid != 1235 {
				t.Fatalf("RUN output metadata lost: %+v", h)
			}
		}
	}
	if !foundRoot || !foundOutput {
		t.Fatalf("published metadata missing: root=%v output=%v", foundRoot, foundOutput)
	}
	if entries, err := os.ReadDir(b.ScratchDir); err != nil || len(entries) != 0 {
		t.Fatal("successful build left workspace", err)
	}
	for _, fault := range []string{"cancel", "vmm-kill", "output-limit"} {
		if !t.Run(fault, func(t *testing.T) {
			faultCtx, faultCancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer faultCancel()
			var process linux.ProcessID
			b.Runner = &BlockRunner{Formatter: formatter, ImageBytes: 32 << 20, Boot: func(ctx context.Context, disk, input string, seed *guestproto.BuildSeedSpec, network bool) (SandboxClient, func() error, error) {
				client, cleanup, err := bootBlock(ctx, disk, input, seed, network)
				if err != nil {
					return client, cleanup, err
				}
				obs, err := backend.Inspect(ctx, tracked.last)
				if err == nil {
					process, err = linux.Identify(obs.PID)
				}
				return client, cleanup, err
			}}
			command := `/bin/busybox dd if=/dev/zero of=/dirty bs=1048576 count=1 2>/dev/null || exit 20; echo fault-ready; /bin/busybox sleep 60`
			if fault == "output-limit" {
				command = `/bin/busybox yes output-limit`
			}
			args, err := json.Marshal([]string{"/bin/busybox", "sh", "-c", command})
			if err != nil {
				t.Fatal(err)
			}
			faultDockerfile := "FROM scratch\nCOPY busybox /bin/busybox\n" + libraryCopies + "RUN " + string(args) + "\n"
			if err := os.WriteFile(filepath.Join(contextDir, "Dockerfile"), []byte(faultDockerfile), 0600); err != nil {
				t.Fatal(err)
			}
			triggered := false
			var injectionErr error
			_, err = b.Build(faultCtx, Request{ContextDir: contextDir, Reference: result.Reference, Network: "none"}, func(line string) {
				if triggered || !strings.Contains(line, "fault-ready") {
					return
				}
				triggered = true
				if fault == "cancel" {
					faultCancel()
				} else if fault == "vmm-kill" {
					var sent bool
					sent, injectionErr = linux.Signal(process, syscall.SIGKILL)
					if injectionErr == nil && !sent {
						injectionErr = errors.New("owned VMM was not killed")
					}
				}
			})
			if injectionErr != nil || (fault != "output-limit" && !triggered) {
				t.Fatalf("fault injection failed: triggered=%v error=%v", triggered, injectionErr)
			}
			if err == nil || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("fault did not fail promptly: %v", err)
			}
			if fault == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation not propagated: %v", err)
			}
			if fault == "output-limit" && !strings.Contains(err.Error(), "aggregate output") {
				t.Fatalf("wrong output-limit failure: %v", err)
			}
			t.Logf("expected %s failure: %v", fault, err)
			prior, ok, err := b.Store.Get(result.Reference)
			if err != nil || !ok || prior.ManifestDigest != result.ManifestDigest {
				t.Fatalf("failed build changed existing image reference: %+v %v", prior, err)
			}
			if process.Alive() {
				t.Fatal("owned VMM survived cleanup")
			}
			if entries, err := os.ReadDir(b.ScratchDir); err != nil || len(entries) != 0 {
				t.Fatal("failed build left scratch", err)
			}
			// Retry with a fresh block disk after the actual interrupted RUN.
			if err := os.WriteFile(filepath.Join(contextDir, "Dockerfile"), []byte(dockerfile), 0600); err != nil {
				t.Fatal(err)
			}
			b.Runner = &BlockRunner{Formatter: formatter, ImageBytes: 32 << 20, Boot: bootBlock}
			retryCtx, retryCancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer retryCancel()
			retry, err := b.Build(retryCtx, Request{ContextDir: contextDir, Reference: "grillo.local/kvm-retry:" + fault, Network: "none"}, nil)
			if err != nil {
				t.Fatalf("fresh build after fault: %v", err)
			}
			if got := readFile(t, unpackImage(t, puller, retry.Image), "out"); got != "quota-ok\nafter" {
				t.Fatalf("retry content: %q", got)
			}
		}) {
			t.Fatal("fault scenario failed; retaining owned state")
		}
	}
	success = true
}
