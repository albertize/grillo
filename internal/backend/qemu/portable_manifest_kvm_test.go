//go:build linux && kvm

// SPDX-License-Identifier: Apache-2.0

package qemu

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/guest"
	"github.com/albertize/grillo/internal/guestproto"
	linux "github.com/albertize/grillo/internal/platform/linux"
	"github.com/albertize/grillo/internal/sandbox"
)

// This gate uses the rebuilt T07 fixture image, not a distributable guest.
// It proves portable inventory loading and real authenticated boot snapshots,
// not clean-host onboarding, workload networking or release license compliance.
func TestKVMPortableManifestBoot(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skip("SKIP: /dev/kvm unavailable")
	}
	if _, err := exec.LookPath(DefaultQEMU); err != nil {
		t.Skip("SKIP: QEMU unavailable")
	}
	root := repoRoot(t)
	sources := map[string]string{"kernel": filepath.Join(root, "experiments/artifacts/qemu/bzImage"), "initramfs": filepath.Join(root, "experiments/artifacts/t07/initramfs-agent.cpio.gz")}
	prefix := filepath.Join(t.TempDir(), "original prefix")
	directory := filepath.Join(prefix, "lib/grillo/guest", guestproto.CurrentVersion.String())
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	var specs []guest.ArtifactSpec
	for _, name := range []string{"kernel", "initramfs"} {
		source, err := os.Open(sources[name])
		if err != nil {
			t.Skipf("SKIP: missing %s; run make t07-guest", name)
		}
		info, err := source.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 512<<20 {
			source.Close()
			t.Fatal("invalid fixture file")
		}
		path := filepath.Join(directory, name)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0444)
		if err != nil {
			source.Close()
			t.Fatal(err)
		}
		size, copyErr := io.Copy(file, io.LimitReader(source, info.Size()+1))
		closeErr := file.Close()
		source.Close()
		if copyErr != nil || closeErr != nil || size != info.Size() {
			t.Fatal("fixture copy failed", copyErr, closeErr)
		}
		specs = append(specs, guest.ArtifactSpec{Name: name, Version: "rebuilt-t07-fixture", Path: path})
	}
	manifest, err := guest.BuildPortableManifest(directory, specs)
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.Write(filepath.Join(directory, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(filepath.Dir(prefix), "moved read-only prefix")
	if err := os.Rename(prefix, moved); err != nil {
		t.Fatal(err)
	}
	directory = filepath.Join(moved, "lib/grillo/guest", guestproto.CurrentVersion.String())
	if err := os.Chmod(directory, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0755) })
	t.Chdir(t.TempDir())
	workDir := filepath.Join(t.TempDir(), "backend")
	backend, err := Open(Config{Kernel: filepath.Join(directory, "kernel"), Initramfs: filepath.Join(directory, "initramfs"), ArtifactManifest: filepath.Join(directory, "manifest.json"), RequirePortableManifest: true, BootKeyOverlay: true, WorkDir: workDir, CIDBase: 9600, BootTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	spec := sandbox.Spec{ID: "portable", Kernel: filepath.Join(directory, "kernel"), Initramfs: filepath.Join(directory, "initramfs"), KernelArgs: "console=ttyS0 reboot=k panic=1 rdinit=/init", VsockPort: 1024, GuestKey: bytes.Repeat([]byte{1}, 32)}
	handle, err := backend.Create(ctx, spec, "portable-kvm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := backend.Delete(cleanup, handle); err != nil {
			t.Error("cleanup", err)
		}
	})
	var previous []byte
	var identities []linux.ProcessID
	for boot := 0; boot < 2; boot++ {
		if err := backend.Start(ctx, handle); err != nil {
			t.Fatal("real portable boot", err)
		}
		obs, err := backend.Inspect(ctx, handle)
		if err != nil || obs.State != sandbox.StateRunning || !obs.GuestAlive || obs.PID == 0 {
			t.Fatal("real guest not running", obs, err)
		}
		identity, err := linux.Identify(obs.PID)
		if err != nil {
			t.Fatal(err)
		}
		identities = append(identities, identity)
		_, _, key, err := backend.BootConnection(handle)
		if err != nil || bytes.Equal(key, spec.GuestKey) || bytes.Equal(key, previous) {
			t.Fatal("fresh credential contract failed", err)
		}
		previous = append([]byte(nil), key...)
		if err := backend.Stop(ctx, handle, 5*time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if err := backend.Delete(ctx, handle); err != nil {
		t.Fatal(err)
	}
	for _, identity := range identities {
		if identity.Alive() {
			t.Error("VMM identity survived teardown")
		}
	}
	entries, err := os.ReadDir(workDir)
	if err != nil || len(entries) != 0 {
		t.Fatal("backend resources remain", entries, err)
	}
	t.Log("PASS: moved/read-only guest directory, arbitrary CWD, two authenticated real boots with distinct fresh keys, verified teardown")
}
