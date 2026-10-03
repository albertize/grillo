//go:build linux && amd64 && kvm

// SPDX-License-Identifier: Apache-2.0

// Real KVM test for the OCI scenarios on the QEMU backend (vhost-vsock). Requires
// QEMU, /dev/vhost-vsock, the QEMU guest bzImage, and the OCI initramfs.
package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestKVMQEMUOCIScenarios(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skipf("SKIP: /dev/kvm unavailable: %v", err)
	}
	if _, err := os.Stat("/dev/vhost-vsock"); err != nil {
		t.Skipf("SKIP: /dev/vhost-vsock unavailable: %v", err)
	}
	qemu, err := exec.LookPath("qemu-system-x86_64")
	if err != nil {
		t.Skipf("SKIP: qemu-system-x86_64 not found: %v", err)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	bzImage := filepath.Join(root, "experiments/artifacts/qemu/bzImage")
	initramfs := filepath.Join(root, "experiments/artifacts/t02/initramfs-oci.cpio.gz")
	for name, path := range map[string]string{"bzImage": bzImage, "initramfs": initramfs} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing %s artifact %q; run 'make qemu-guest oci-guest': %v", name, path, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	if err := runQemu(ctx, qemu, bzImage, initramfs, 3, true); err != nil {
		t.Fatalf("runQemu: %v", err)
	}
}
