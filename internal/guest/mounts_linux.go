//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"

	"github.com/albertize/grillo/internal/guestproto"
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
	// A resolver file must exist so the container bind mount always resolves.
	if err := os.MkdirAll("/etc", 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(resolvConfPath); err != nil {
		if err := os.WriteFile(resolvConfPath, []byte("nameserver 127.0.0.1\n"), 0o644); err != nil {
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

// MountShares mounts each volume share at its target before containers start.
// Virtiofs shares use the tag as the mount source; block filesystems use the
// source path. A share already mounted is left in place.
func MountShares(shares []guestproto.ShareSpec) error {
	for _, share := range shares {
		fstype := share.FSType
		if fstype == "" {
			fstype = "virtiofs"
		}
		source := share.Source
		if fstype == "virtiofs" {
			source = share.Tag
		}
		if source == "" {
			return fmt.Errorf("guest: share %q has no source", share.Tag)
		}
		if err := os.MkdirAll(share.Target, 0o700); err != nil {
			return fmt.Errorf("guest: create share target %s: %w", share.Target, err)
		}
		flags := uintptr(0)
		if share.ReadOnly {
			flags |= unix.MS_RDONLY
		}
		if err := unix.Mount(source, share.Target, fstype, flags, ""); err != nil {
			if errors.Is(err, unix.EBUSY) {
				continue
			}
			return fmt.Errorf("guest: mount %s (%s) at %s: %w", source, fstype, share.Target, err)
		}
	}
	return nil
}
