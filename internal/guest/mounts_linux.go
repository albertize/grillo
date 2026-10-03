//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// SetupFilesystems mounts the pseudo-filesystems PID 1 needs. It is idempotent:
// an already-mounted target returns EBUSY, which is treated as success.
func SetupFilesystems() error {
	base := []struct{ target, fstype string }{
		{"/proc", "proc"},
		{"/sys", "sysfs"},
		{"/dev", "devtmpfs"},
	}
	for _, m := range base {
		if err := mountPseudo(m.target, m.fstype, "", 0); err != nil {
			return err
		}
	}
	extra := []struct {
		target, fstype, data string
		flags                uintptr
	}{
		{"/sys/fs/cgroup", "cgroup2", "", 0},
		{"/dev/pts", "devpts", "newinstance,ptmxmode=0666,mode=0620", 0},
		{"/dev/shm", "tmpfs", "mode=1777", 0},
		{"/run", "tmpfs", "mode=0755", 0},
	}
	for _, m := range extra {
		if err := os.MkdirAll(m.target, 0o755); err != nil {
			return fmt.Errorf("guest: mkdir %s: %w", m.target, err)
		}
		if err := mountPseudo(m.target, m.fstype, m.data, m.flags); err != nil {
			return err
		}
	}
	return nil
}

func mountPseudo(target, fstype, data string, flags uintptr) error {
	err := unix.Mount("", target, fstype, flags, data)
	if err == nil || errors.Is(err, unix.EBUSY) {
		return nil
	}
	return fmt.Errorf("guest: mount %s (%s): %w", target, fstype, err)
}

// SetupLoopback enables the loopback interface so containers sharing the guest
// network namespace can use 127.0.0.1.
func SetupLoopback() error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("guest: socket: %w", err)
	}
	defer unix.Close(fd)
	ifr, err := unix.NewIfreq("lo")
	if err != nil {
		return fmt.Errorf("guest: ifreq: %w", err)
	}
	ifr.SetUint16(ifr.Uint16() | unix.IFF_UP)
	if err := unix.IoctlIfreq(fd, unix.SIOCSIFFLAGS, ifr); err != nil {
		return fmt.Errorf("guest: set lo up: %w", err)
	}
	return nil
}
