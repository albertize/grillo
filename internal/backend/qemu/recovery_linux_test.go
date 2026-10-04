//go:build linux

// SPDX-License-Identifier: Apache-2.0

package qemu

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	linux "github.com/albertize/grillo/internal/platform/linux"
	"github.com/albertize/grillo/internal/sandbox"
)

func TestQMPCredentialsUseHostPID(t *testing.T) {
	dir := t.TempDir()
	listener, err := net.Listen("unix", filepath.Join(dir, "qmp.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	pp, err := identifyVMM(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if pp.PID != os.Getpid() || pp.StartTime == 0 || pp.BootID == "" {
		t.Fatalf("invalid identity: %+v", pp)
	}
}

func TestRecoveryRefusesMismatchedIdentity(t *testing.T) {
	id, err := linux.Identify(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	pp := persistedProcess{PID: id.PID, BootID: id.BootID, StartTime: id.StartTime + 1, Executable: id.Executable}
	if err := stopVerified(context.Background(), pp); err == nil {
		t.Fatal("recycled PID treated as recovered")
	}
}

func TestBackendRecoveryRemovesVerifiedProcessesAndState(t *testing.T) {
	cfg := testConfig(t, func(context.Context, sandbox.Spec) (GuestConn, error) { return &fakeGuest{}, nil })
	b, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h, err := b.Create(context.Background(), testSpec(), "recovery")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Delete(context.Background(), h) })
	if err := b.Start(context.Background(), h); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := reopened.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sandboxDir(cfg.WorkDir, h.ID)); !os.IsNotExist(err) {
		t.Fatalf("metadata not removed: %v", err)
	}
}
