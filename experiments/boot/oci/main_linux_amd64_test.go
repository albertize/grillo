//go:build linux && amd64 && kvm

// SPDX-License-Identifier: Apache-2.0

// Real KVM test for the T02 OCI scenarios. Requires the OCI guest artifacts
// (make oci-guest) and /dev/kvm. A missing /dev/kvm is a SKIP, not a pass.
package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/albertize/grillo/experiments/boot/spike"
)

func TestKVMOCIScenarios(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skipf("SKIP: /dev/kvm unavailable: %v (hardware gate not exercised)", err)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	opts := spike.Options{
		Firecracker: filepath.Join(root, "experiments/artifacts/dependencies-v1/bin/firecracker"),
		Kernel:      filepath.Join(root, "experiments/artifacts/t02/vmlinux"),
		Initramfs:   filepath.Join(root, "experiments/artifacts/t02/initramfs-oci.cpio.gz"),
	}
	for name, path := range map[string]string{"firecracker": opts.Firecracker, "kernel": opts.Kernel, "initramfs": opts.Initramfs} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing %s artifact %q; run 'make oci-guest': %v", name, path, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	if err := runAll(ctx, opts); err != nil {
		t.Fatalf("runAll: %v", err)
	}
}
