//go:build linux && amd64 && kvm

// SPDX-License-Identifier: Apache-2.0

// These tests require real KVM hardware, a Firecracker binary, and built guest
// artifacts. They are excluded from ordinary builds and run only under the kvm
// build tag (see the test-kvm Make target). A skip because /dev/kvm is missing
// is NOT a passed hardware gate.
package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func requireKVM(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skipf("SKIP: /dev/kvm unavailable: %v (hardware gate not exercised)", err)
	}
}

func baseOptions(t *testing.T, logW *os.File) Options {
	t.Helper()
	root := repoRoot(t)
	opts := Options{
		Firecracker: filepath.Join(root, "experiments/artifacts/dependencies-v1/bin/firecracker"),
		Kernel:      filepath.Join(root, "experiments/artifacts/t01/vmlinux"),
		Initramfs:   filepath.Join(root, "experiments/artifacts/t01/initramfs.cpio.gz"),
		Log:         logW,
	}
	for name, path := range map[string]string{"firecracker": opts.Firecracker, "kernel": opts.Kernel, "initramfs": opts.Initramfs} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing %s artifact %q; run 'make guest': %v", name, path, err)
		}
	}
	return opts
}

// TestKVMExecBootStop boots a real microVM and verifies the full cycle.
func TestKVMExecBootStop(t *testing.T) {
	requireKVM(t)
	logW, _ := os.CreateTemp(t.TempDir(), "firecracker-*.log")
	defer logW.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	res, err := RunOnce(ctx, baseOptions(t, logW))
	if err != nil {
		log, _ := os.ReadFile(logW.Name())
		t.Fatalf("RunOnce: %v\n--- firecracker log ---\n%s", err, log)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", res.ExitCode)
	}
	if !strings.Contains(res.Stdout, "grillo-testcmd stdout") {
		t.Errorf("stdout = %q, want grillo-testcmd output", res.Stdout)
	}
	if !strings.Contains(res.Stdout, "args=[alpha beta]") {
		t.Errorf("stdout = %q, want passed arguments", res.Stdout)
	}
	if !strings.Contains(res.Stderr, "grillo-testcmd stderr") {
		t.Errorf("stderr = %q, want grillo-testcmd stderr", res.Stderr)
	}
	t.Logf("boot=%s exec=%s stop=%s total=%s", res.BootDuration, res.ExecDuration, res.StopDuration, res.Total)
}

// TestKVMExecMissingCommand verifies that a failed guest exec is reported with a
// nonzero status and a diagnostic rather than being silently hidden.
func TestKVMExecMissingCommand(t *testing.T) {
	requireKVM(t)
	logW, _ := os.CreateTemp(t.TempDir(), "firecracker-*.log")
	defer logW.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	opts := baseOptions(t, logW)
	opts.Command = "/bin/definitely-not-present"
	opts.Args = nil
	res, err := RunOnce(ctx, opts)
	if err != nil {
		log, _ := os.ReadFile(logW.Name())
		t.Fatalf("RunOnce: %v\n--- firecracker log ---\n%s", err, log)
	}
	if res.ExitCode != -1 {
		t.Errorf("exit code = %d, want -1 for missing executable", res.ExitCode)
	}
	if !strings.Contains(res.Stderr, "start:") {
		t.Errorf("stderr = %q, want a start failure diagnostic", res.Stderr)
	}
}
