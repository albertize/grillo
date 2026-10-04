//go:build linux

// SPDX-License-Identifier: Apache-2.0

package qemu

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	linux "github.com/albertize/grillo/internal/platform/linux"
	"github.com/albertize/grillo/internal/sandbox"
)

func startRaw(t *testing.T, path string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func fakeScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// fakeVMM ignores its arguments and sleeps until killed.
func fakeVMM(t *testing.T) string { return fakeScript(t, "sleep 300") }

// fakeVirtioFSD captures --socket-path, creates it, then sleeps.
func fakeVirtioFSD(t *testing.T) string {
	return fakeScript(t, `sock=""
while [ $# -gt 0 ]; do
  case "$1" in
    --socket-path) sock="$2"; shift 2;;
    *) shift;;
  esac
done
[ -n "$sock" ] && : > "$sock"
sleep 300`)
}

type fakeGuest struct {
	stopped bool
}

func (f *fakeGuest) Status(context.Context) (GuestStatus, error) {
	return GuestStatus{State: "running", Containers: 3, Zombies: 0}, nil
}
func (f *fakeGuest) Stop(context.Context) (bool, error) { f.stopped = true; return true, nil }
func (f *fakeGuest) Close() error                       { return nil }

func testConfig(t *testing.T, dial func(context.Context, sandbox.Spec) (GuestConn, error)) Config {
	t.Helper()
	return Config{
		QEMU:        fakeVMM(t),
		VirtioFSD:   fakeVirtioFSD(t),
		WorkDir:     filepath.Join(t.TempDir(), "work"),
		BootTimeout: 3 * time.Second,
		DialGuest:   dial,
	}
}

func okDial(context.Context, sandbox.Spec) (GuestConn, error) { return &fakeGuest{}, nil }

func testSpec() sandbox.Spec {
	return sandbox.Spec{
		ID:         "sbx-1",
		Kernel:     "/kernel",
		Initramfs:  "/initrd",
		KernelArgs: "console=ttyS0 rdinit=/init",
		VsockPort:  1024,
		GuestKey:   bytes.Repeat([]byte{1}, 32),
	}
}

func TestCreateIdempotentByOperation(t *testing.T) {
	b, err := Open(testConfig(t, okDial))
	if err != nil {
		t.Fatal(err)
	}
	first, err := b.Create(context.Background(), testSpec(), "op-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.Create(context.Background(), testSpec(), "op-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.Operation != second.Operation {
		t.Fatalf("handles differ: %+v vs %+v", first, second)
	}
	entries, _ := os.ReadDir(b.cfg.WorkDir)
	if len(entries) != 1 {
		t.Fatalf("expected one sandbox dir, got %d", len(entries))
	}
}

func TestCreateConflictsOnID(t *testing.T) {
	b, _ := Open(testConfig(t, okDial))
	if _, err := b.Create(context.Background(), testSpec(), "op-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Create(context.Background(), testSpec(), "op-2"); !errors.Is(err, sandbox.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestLifecycle(t *testing.T) {
	b, _ := Open(testConfig(t, okDial))
	ctx := context.Background()
	handle, err := b.Create(ctx, testSpec(), "op-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Start(ctx, handle); err != nil {
		t.Fatal(err)
	}
	if err := b.Start(ctx, handle); err != nil {
		t.Fatalf("idempotent start: %v", err)
	}
	obs, err := b.Inspect(ctx, handle)
	if err != nil {
		t.Fatal(err)
	}
	if obs.State != sandbox.StateRunning || !obs.GuestAlive || obs.GuestContainers != 3 || obs.PID == 0 {
		t.Fatalf("observation = %+v", obs)
	}
	if err := b.Stop(ctx, handle, time.Second); err != nil {
		t.Fatal(err)
	}
	obs, _ = b.Inspect(ctx, handle)
	if obs.State != sandbox.StateStopped {
		t.Fatalf("state after stop = %s", obs.State)
	}
	if err := b.Delete(ctx, handle); err != nil {
		t.Fatal(err)
	}
	obs, _ = b.Inspect(ctx, handle)
	if obs.State != sandbox.StateAbsent {
		t.Fatalf("state after delete = %s", obs.State)
	}
}

func TestAbsentOperationsSucceed(t *testing.T) {
	b, _ := Open(testConfig(t, okDial))
	handle := sandbox.Handle{ID: "does-not-exist"}
	if err := b.Stop(context.Background(), handle, time.Second); err != nil {
		t.Fatalf("stop absent: %v", err)
	}
	if err := b.Delete(context.Background(), handle); err != nil {
		t.Fatalf("delete absent: %v", err)
	}
	obs, err := b.Inspect(context.Background(), handle)
	if err != nil || obs.State != sandbox.StateAbsent {
		t.Fatalf("inspect absent = %+v err=%v", obs, err)
	}
}

func TestStartHandshakeFailureCleansUp(t *testing.T) {
	dial := func(context.Context, sandbox.Spec) (GuestConn, error) {
		return nil, errors.New("no guest")
	}
	cfg := testConfig(t, dial)
	cfg.BootTimeout = 300 * time.Millisecond
	b, _ := Open(cfg)
	ctx := context.Background()
	handle, err := b.Create(ctx, testSpec(), "op-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Start(ctx, handle); err == nil {
		t.Fatal("expected handshake failure")
	}
	entry, err := b.entry(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	pid := entry.persisted.QEMU.PID
	time.Sleep(200 * time.Millisecond)
	if _, err := linux.Identify(pid); err == nil {
		t.Fatalf("failed start left the vmm process %d running", pid)
	}
	obs, _ := b.Inspect(ctx, handle)
	if obs.State != sandbox.StateFailed {
		t.Fatalf("state = %s, want failed", obs.State)
	}
}

func TestReopenLoadsPersistedSandbox(t *testing.T) {
	cfg := testConfig(t, okDial)
	b, _ := Open(cfg)
	ctx := context.Background()
	if _, err := b.Create(ctx, testSpec(), "op-1"); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	obs, err := reopened.Inspect(ctx, sandbox.Handle{ID: "sbx-1"})
	if err != nil {
		t.Fatal(err)
	}
	if obs.State != sandbox.StateCreated {
		t.Fatalf("reopened state = %s, want created", obs.State)
	}
}

func TestStopPersistedRejectsStaleIdentity(t *testing.T) {
	path := fakeScript(t, "sleep 300")
	cmd := startRaw(t, path)
	id, err := linux.Identify(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	stale := persistedProcess{PID: id.PID, BootID: id.BootID, StartTime: id.StartTime + 1}
	stopPersisted(stale, 100*time.Millisecond)
	if !id.Alive() {
		t.Fatal("stale identity must not kill a live process")
	}
	stopPersisted(persistedProcess{PID: id.PID, BootID: id.BootID, StartTime: id.StartTime, Executable: id.Executable}, time.Second)
	time.Sleep(100 * time.Millisecond)
	if id.Alive() {
		t.Fatal("matching identity should have been killed")
	}
	_ = cmd.Wait()
}

func TestCyclesDoNotLeak(t *testing.T) {
	cfg := testConfig(t, okDial)
	b, _ := Open(cfg)
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		spec := testSpec()
		handle, err := b.Create(ctx, spec, sandbox.OperationID("cycle"))
		if err != nil {
			t.Fatalf("cycle %d create: %v", i, err)
		}
		if err := b.Start(ctx, handle); err != nil {
			t.Fatalf("cycle %d start: %v", i, err)
		}
		if err := b.Stop(ctx, handle, time.Second); err != nil {
			t.Fatalf("cycle %d stop: %v", i, err)
		}
		if err := b.Delete(ctx, handle); err != nil {
			t.Fatalf("cycle %d delete: %v", i, err)
		}
	}
	entries, err := os.ReadDir(cfg.WorkDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("work dir not empty after cycles: %s", strings.Join(names, ", "))
	}
}

func TestCapabilitiesReported(t *testing.T) {
	b, _ := Open(testConfig(t, okDial))
	caps, err := b.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// virtiofsd is the fake script and must be reported; KVM depends on the host.
	if !caps.VirtioFS.Supported {
		t.Fatalf("virtiofsd should be supported: %+v", caps.VirtioFS)
	}
}
