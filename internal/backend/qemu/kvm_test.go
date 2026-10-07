//go:build kvm

// SPDX-License-Identifier: Apache-2.0

package qemu

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	linux "github.com/albertize/grillo/internal/platform/linux"
	"github.com/albertize/grillo/internal/sandbox"
)

// TestKVMCreateStartStopDelete boots the real T07 guest through the backend and
// exercises create/start/inspect/stop/delete. It runs GRILLO_KVM_CYCLES cycles
// (default 3) and verifies no VMM process or sandbox directory leaks. Run with
// GRILLO_KVM_CYCLES=100 to satisfy the 100-cycle gate.
func TestKVMCreateStartStopDelete(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skip("SKIP: /dev/kvm is not available")
	}
	root := repoRoot(t)
	kernel := filepath.Join(root, "experiments/artifacts/qemu/bzImage")
	initramfs := filepath.Join(root, "experiments/artifacts/t07/initramfs-agent.cpio.gz")
	keyPath := filepath.Join(root, "experiments/artifacts/t07/key")
	for _, path := range []string{kernel, initramfs, keyPath} {
		if _, err := os.Stat(path); err != nil {
			t.Skipf("SKIP: missing %s (run 'make t07-guest')", path)
		}
	}
	if _, err := os.Stat(DefaultVirtioFSD); err != nil {
		t.Skipf("SKIP: virtiofsd not found at %s", DefaultVirtioFSD)
	}
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(keyData)))
	if err != nil {
		t.Fatal(err)
	}
	cycles := 3
	if v := os.Getenv("GRILLO_KVM_CYCLES"); v != "" {
		cycles, err = strconv.Atoi(v)
		if err != nil || cycles < 1 {
			t.Fatalf("invalid GRILLO_KVM_CYCLES=%q", v)
		}
	}

	workDir := filepath.Join(t.TempDir(), "work")
	backend, err := Open(Config{
		Kernel:      kernel,
		Initramfs:   initramfs,
		WorkDir:     workDir,
		CIDBase:     60,
		BootTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cycles)*30*time.Second)
	defer cancel()

	seenPIDs := map[int]bool{}
	// Phase measurements exclude repeated idempotency calls and inspection.
	// Warm local artifacts only: no pull, containers, or application readiness.
	phases := map[string][]time.Duration{}
	start := time.Now()
	for i := 0; i < cycles; i++ {
		spec := sandbox.Spec{
			ID:         fmt.Sprintf("sbx-%d", i),
			Kernel:     kernel,
			Initramfs:  initramfs,
			KernelArgs: "console=ttyS0 reboot=k panic=1 rdinit=/init",
			VsockPort:  1024,
			GuestKey:   key,
		}
		phaseStart := time.Now()
		handle, err := backend.Create(ctx, spec, sandbox.OperationID(fmt.Sprintf("cycle-%d", i)))
		if err != nil {
			t.Fatalf("cycle %d create: %v", i, err)
		}
		phases["create"] = append(phases["create"], time.Since(phaseStart))
		phaseStart = time.Now()
		if err := backend.Start(ctx, handle); err != nil {
			t.Fatalf("cycle %d start: %v", i, err)
		}
		phases["start-to-authenticated-agent"] = append(phases["start-to-authenticated-agent"], time.Since(phaseStart))
		obs, err := backend.Inspect(ctx, handle)
		if err != nil {
			t.Fatalf("cycle %d inspect: %v", i, err)
		}
		if obs.State != sandbox.StateRunning || !obs.GuestAlive || obs.PID == 0 {
			t.Fatalf("cycle %d observation = %+v", i, obs)
		}
		if obs.GuestZombies != 0 {
			t.Fatalf("cycle %d guest reports %d zombies", i, obs.GuestZombies)
		}
		seenPIDs[obs.PID] = true
		// Repeated operations must not duplicate resources.
		if err := backend.Start(ctx, handle); err != nil {
			t.Fatalf("cycle %d repeated start: %v", i, err)
		}
		phaseStart = time.Now()
		if err := backend.Stop(ctx, handle, 5*time.Second); err != nil {
			t.Fatalf("cycle %d stop: %v", i, err)
		}
		if err := backend.Delete(ctx, handle); err != nil {
			t.Fatalf("cycle %d delete: %v", i, err)
		}
		phases["stop-and-delete"] = append(phases["stop-and-delete"], time.Since(phaseStart))
		obs, _ = backend.Inspect(ctx, handle)
		if obs.State != sandbox.StateAbsent {
			t.Fatalf("cycle %d state after delete = %s", i, obs.State)
		}
	}
	t.Logf("%d cycles in %s (%.0f ms/cycle)", cycles, time.Since(start).Round(time.Millisecond), float64(time.Since(start).Milliseconds())/float64(cycles))

	for _, name := range []string{"create", "start-to-authenticated-agent", "stop-and-delete"} {
		values := phases[name]
		slices.Sort(values)
		// Nearest-rank percentiles; every recorded sample completed successfully.
		t.Logf("warm phase %s: samples=%d median=%s p95=%s", name, len(values), values[(len(values)-1)/2].Round(time.Microsecond), values[(95*len(values)+99)/100-1].Round(time.Microsecond))
	}

	// No VMM process may survive.
	for pid := range seenPIDs {
		if id, err := linux.Identify(pid); err == nil && id.Alive() {
			t.Errorf("vmm process %d leaked after cycles", pid)
		}
	}
	// No sandbox directory may remain.
	entries, err := os.ReadDir(workDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("work dir not empty after %d cycles: %v", cycles, entries)
	}
}

func repoRoot(t *testing.T) string {
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
