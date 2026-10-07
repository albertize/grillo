//go:build linux

// SPDX-License-Identifier: Apache-2.0

package linux

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
)

// OpenPTY opens a master/slave pair from the caller's devpts instance.
func OpenPTY() (*os.File, *os.File, error) {
	fd, err := unix.Open("/dev/pts/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	// Some hosts deliberately use ptmxmode=000 and expose the legacy /dev/ptmx
	// device instead. This uses existing device permissions, never chmod/sudo.
	if err == unix.EACCES || err == unix.ENOENT {
		fd, err = unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	}
	if err != nil {
		return nil, nil, err
	}
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		_ = unix.Close(fd)
		return nil, nil, err
	}
	number, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		_ = unix.Close(fd)
		return nil, nil, err
	}
	master := os.NewFile(uintptr(fd), "pty-master")
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		_ = master.Close()
		return nil, nil, err
	}
	return master, slave, nil
}
