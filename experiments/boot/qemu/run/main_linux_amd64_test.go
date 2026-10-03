//go:build linux && amd64 && kvm

// SPDX-License-Identifier: Apache-2.0

// Real KVM test for the QEMU + virtiofs live-share experiment. Requires QEMU,
// virtiofsd, the VIRTIO_FS guest kernel, and /dev/kvm. A missing dependency is a
// SKIP, never a pass.
package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKVMQEMULiveShare(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skipf("SKIP: /dev/kvm unavailable: %v", err)
	}
	qemu, err := exec.LookPath("qemu-system-x86_64")
	if err != nil {
		t.Skipf("SKIP: qemu-system-x86_64 not found: %v", err)
	}
	virtiofsd := "/usr/libexec/virtiofsd"
	if _, err := os.Stat(virtiofsd); err != nil {
		t.Skipf("SKIP: virtiofsd not found: %v", err)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	opts := options{
		QEMU:      qemu,
		VirtioFSD: virtiofsd,
		Kernel:    filepath.Join(root, "experiments/artifacts/qemu/bzImage"),
		Initramfs: filepath.Join(root, "experiments/artifacts/qemu/initramfs-probe.cpio.gz"),
	}
	for name, path := range map[string]string{"kernel": opts.Kernel, "initramfs": opts.Initramfs} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing %s artifact %q; run 'make qemu-guest': %v", name, path, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	serial, err := runProbe(ctx, opts)
	if err != nil {
		t.Fatalf("runProbe: %v\n--- serial ---\n%s", err, serial)
	}
	if !strings.Contains(serial, "STORAGE RESULT: PASS") {
		t.Fatalf("guest did not report PASS:\n%s", serial)
	}
}

// TestKVMQEMUNet verifies rootless guest egress through QEMU user-mode networking.
func TestKVMQEMUNet(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skipf("SKIP: /dev/kvm unavailable: %v", err)
	}
	qemu, err := exec.LookPath("qemu-system-x86_64")
	if err != nil {
		t.Skipf("SKIP: qemu-system-x86_64 not found: %v", err)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	opts := options{
		QEMU:      qemu,
		Kernel:    filepath.Join(root, "experiments/artifacts/qemu/bzImage"),
		Initramfs: filepath.Join(root, "experiments/artifacts/qemu/initramfs-net.cpio.gz"),
	}
	for name, path := range map[string]string{"kernel": opts.Kernel, "initramfs": opts.Initramfs} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing %s artifact %q; run 'make qemu-guest': %v", name, path, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	serial, err := runNet(ctx, opts)
	if err != nil {
		t.Fatalf("runNet: %v\n--- serial ---\n%s", err, serial)
	}
	if !strings.Contains(serial, "NET RESULT: PASS") {
		t.Fatalf("guest did not report PASS:\n%s", serial)
	}
}
