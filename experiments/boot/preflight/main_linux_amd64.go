// SPDX-License-Identifier: Apache-2.0

// preflight checks KVM device access only. It does not boot or validate a guest.
package main

import (
	"fmt"
	"os"
	"syscall"
)

// Linux KVM UAPI: include/uapi/linux/kvm.h, KVM_GET_API_VERSION.
const kvmGetAPIVersion = 0xae00

func checkKVM(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open KVM device: %w", err)
	}
	version, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), kvmGetAPIVersion, 0)
	closeErr := f.Close()
	if errno != 0 {
		return fmt.Errorf("KVM_GET_API_VERSION: %w", errno)
	}
	if closeErr != nil {
		return fmt.Errorf("close KVM device: %w", closeErr)
	}
	if version != 12 {
		return fmt.Errorf("KVM API version %d; expected 12", version)
	}
	return nil
}

func main() {
	if os.Getuid() == 0 || os.Geteuid() == 0 {
		fmt.Fprintln(os.Stderr, "BLOCKED: run as an unprivileged host user, not root")
		os.Exit(1)
	}
	if err := checkKVM("/dev/kvm"); err != nil {
		fmt.Fprintln(os.Stderr, "BLOCKED:", err)
		os.Exit(1)
	}
	fmt.Printf("PASS: uid=%d euid=%d; /dev/kvm opened read/write; KVM API version 12\n", os.Getuid(), os.Geteuid())
	fmt.Println("NOT CHECKED: VM creation, boot, vsock, guest command, logs, or stop cycles")
}
