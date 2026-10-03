//go:build kvm

// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"grillo.local/grillo/internal/backend/qemu"
	"grillo.local/grillo/internal/guestproto"
	"grillo.local/grillo/internal/sandbox"
)

// TestKVMBindPersistence is acceptance scenario G through the production stack:
// a bind volume is attached to a sandbox, the guest reads host changes and the
// host sees guest writes live, and a read-only attachment rejects writes.
//
// Missing /dev/kvm, qemu, or the built T07 image is a documented SKIP.
func TestKVMBindPersistence(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skip("SKIP: /dev/kvm is not available")
	}
	root := storageRepoRoot(t)
	kernel := filepath.Join(root, "experiments/artifacts/qemu/bzImage")
	initramfs := filepath.Join(root, "experiments/artifacts/t07/initramfs-agent.cpio.gz")
	keyPath := filepath.Join(root, "experiments/artifacts/t07/key")
	for _, path := range []string{kernel, initramfs, keyPath} {
		if _, err := os.Stat(path); err != nil {
			t.Skipf("SKIP: missing %s (run 'make t07-guest')", path)
		}
	}
	keyData, _ := os.ReadFile(keyPath)
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(keyData)))
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := Open(filepath.Join(t.TempDir(), "storage"))
	if err != nil {
		t.Fatal(err)
	}
	backend, err := qemu.Open(qemu.Config{
		Kernel:      kernel,
		Initramfs:   initramfs,
		WorkDir:     filepath.Join(t.TempDir(), "backend"),
		CIDBase:     70,
		BootTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// --- Read-write bind: live sharing both directions. ---
	hostShare := t.TempDir()
	if err := os.WriteFile(filepath.Join(hostShare, "sentinel.txt"), []byte("host-sentinel\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	createBind(t, mgr, "rw", hostShare, false)

	spec := sandbox.Spec{
		ID: "sbx-rw", Kernel: kernel, Initramfs: initramfs,
		KernelArgs: "console=ttyS0 reboot=k panic=1 rdinit=/init",
		VsockPort:  1024, GuestKey: key, VsockCID: 70,
		Shares: []sandbox.Share{{Tag: "data", HostPath: hostShare}},
	}
	handle, err := backend.Create(ctx, spec, "op-rw")
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Start(ctx, handle); err != nil {
		t.Fatalf("start rw sandbox: %v", err)
	}
	defer backend.Delete(ctx, handle)

	client := connectGuest(t, ctx, 70, key)
	defer client.Close()
	startSandbox(t, ctx, client, "sbx-rw", "data", "/volumes/data", false, false)

	if out := execGuest(t, ctx, client, "app", "/bin/cat", "/data/sentinel.txt"); !strings.Contains(out, "host-sentinel") {
		t.Fatalf("guest did not see the host sentinel: %q", out)
	}
	// Host writes after mount must be visible (a live bind, not an initial copy).
	if err := os.WriteFile(filepath.Join(hostShare, "late.txt"), []byte("late\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := execGuest(t, ctx, client, "app", "/bin/cat", "/data/late.txt"); !strings.Contains(out, "late") {
		t.Fatalf("guest did not see the late host write: %q", out)
	}
	// Guest writes must appear on the host.
	execGuest(t, ctx, client, "app", "/bin/sh", "-c", "echo guest-write > /data/from-guest.txt")
	data, err := os.ReadFile(filepath.Join(hostShare, "from-guest.txt"))
	if err != nil || !strings.Contains(string(data), "guest-write") {
		t.Fatalf("host did not see the guest write: %q err=%v", data, err)
	}

	// --- Read-only bind: writes are denied. ---
	roShare := t.TempDir()
	if err := os.WriteFile(filepath.Join(roShare, "ro.txt"), []byte("ro\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	createBind(t, mgr, "ro", roShare, true)
	roSpec := sandbox.Spec{
		ID: "sbx-ro", Kernel: kernel, Initramfs: initramfs,
		KernelArgs: "console=ttyS0 reboot=k panic=1 rdinit=/init",
		VsockPort:  1024, GuestKey: key, VsockCID: 71,
		Shares: []sandbox.Share{{Tag: "rod", HostPath: roShare, ReadOnly: true}},
	}
	roHandle, err := backend.Create(ctx, roSpec, "op-ro")
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Start(ctx, roHandle); err != nil {
		t.Fatalf("start ro sandbox: %v", err)
	}
	defer backend.Delete(ctx, roHandle)

	roClient := connectGuest(t, ctx, 71, key)
	defer roClient.Close()
	startSandbox(t, ctx, roClient, "sbx-ro", "rod", "/volumes/rod", true, true)

	if out := execGuest(t, ctx, roClient, "app", "/bin/cat", "/data/ro.txt"); !strings.Contains(out, "ro") {
		t.Fatalf("read-only share not readable: %q", out)
	}
	if out, code, err := execGuestResult(ctx, roClient, "app", "/bin/sh", "-c", "echo nope > /data/nope.txt"); err != nil || code == 0 {
		t.Fatalf("read-only write was not denied: code=%d out=%q err=%v", code, out, err)
	}
	if _, err := os.Stat(filepath.Join(roShare, "nope.txt")); !os.IsNotExist(err) {
		t.Fatal("read-only write reached the host")
	}
}

func createBind(t *testing.T, mgr *Manager, id, source string, readOnly bool) {
	t.Helper()
	ctx := context.Background()
	if _, err := mgr.Create(ctx, Volume{ID: id, Name: id, Kind: KindBind, Source: source, ReadOnly: readOnly, Owner: Owner{Application: "app"}}, "op"); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Attach(ctx, id, "sbx-"+id, readOnly, 0, 0, "op"); err != nil {
		t.Fatal(err)
	}
}

func startSandbox(t *testing.T, ctx context.Context, client *guestproto.Client, id, tag, target string, shareReadOnly, mountReadOnly bool) {
	t.Helper()
	spec := guestproto.SandboxSpec{
		ID:     id,
		Shares: []guestproto.ShareSpec{{Tag: tag, Target: target, ReadOnly: shareReadOnly}},
		Containers: []guestproto.ContainerSpec{{
			Name:   "app",
			Rootfs: "/rootfs-app",
			Args:   []string{"/bin/sh", "-c", "sleep 300"},
			Mounts: []guestproto.MountSpec{{Source: target, Target: "/data", ReadOnly: mountReadOnly}},
		}},
	}
	if _, err := client.Start(ctx, guestproto.StartRequest{Sandbox: &spec}); err != nil {
		t.Fatalf("start containers: %v", err)
	}
}

func connectGuest(t *testing.T, ctx context.Context, cid uint32, key []byte) *guestproto.Client {
	t.Helper()
	var conn guestproto.Conn
	for {
		var err error
		conn, err = guestproto.DialVsock(ctx, cid, 1024)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("dial guest: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	client, err := guestproto.NewClient(ctx, conn, guestproto.HandshakeConfig{Key: key, Agent: "t10-test"})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	return client
}

func execGuest(t *testing.T, ctx context.Context, client *guestproto.Client, container string, args ...string) string {
	t.Helper()
	out, code, err := execGuestResult(ctx, client, container, args...)
	if err != nil || code != 0 {
		t.Fatalf("exec %s %v: code=%d err=%v out=%q", container, args, code, err, out)
	}
	return out
}

func execGuestResult(ctx context.Context, client *guestproto.Client, container string, args ...string) (string, int, error) {
	var stdout, stderr bytes.Buffer
	result, err := client.Exec(ctx, guestproto.ExecRequest{Container: container, Args: args}, &stdout, &stderr)
	if err != nil {
		return "", 0, err
	}
	return stdout.String() + stderr.String(), result.ExitCode, nil
}

func storageRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate source file")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
