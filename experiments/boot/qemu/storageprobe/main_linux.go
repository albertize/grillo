//go:build linux

// SPDX-License-Identifier: Apache-2.0

// Command storageprobe is the T03/QEMU guest PID 1 for the live-bind experiment.
// It mounts a virtiofs tag shared by the host, exercises bidirectional live I/O,
// verifies read-only enforcement, prints structured results to the serial
// console, and resets the VM so QEMU (with -no-reboot) exits.
//
// It is experiment code, not the Grillo runtime.
package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

const (
	tag      = "hostshare"
	rwTarget = "/mnt/host"
	roTarget = "/mnt/ro"
	payload  = "guest-payload\n"
)

func main() {
	if err := setup(); err != nil {
		fmt.Println("STORAGE SETUP FAIL:", err)
		finish()
	}

	failed := false
	step := func(name string, fn func() error) {
		if err := fn(); err != nil {
			failed = true
			fmt.Printf("STORAGE %-10s FAIL: %v\n", name, err)
			return
		}
		fmt.Printf("STORAGE %-10s ok\n", name)
	}

	step("mount-rw", func() error { return unix.Mount(tag, rwTarget, "virtiofs", 0, "") })
	step("read-host", func() error {
		data, err := os.ReadFile(rwTarget + "/host-sentinel.txt")
		if err != nil {
			return err
		}
		fmt.Printf("STORAGE host-sentinel %q\n", string(data))
		return nil
	})
	step("write-host", func() error {
		return os.WriteFile(rwTarget+"/guest-wrote.txt", []byte(payload), 0o644)
	})
	step("rename", func() error {
		return os.Rename(rwTarget+"/guest-wrote.txt", rwTarget+"/guest-renamed.txt")
	})
	step("mount-ro", func() error {
		return unix.Mount(tag, roTarget, "virtiofs", unix.MS_RDONLY, "")
	})
	step("read-ro", func() error {
		_, err := os.ReadFile(roTarget + "/host-sentinel.txt")
		return err
	})
	step("ro-enforced", func() error {
		err := os.WriteFile(roTarget+"/should-fail.txt", []byte("x"), 0o644)
		if err == nil {
			return errors.New("write to read-only share unexpectedly succeeded")
		}
		if !errors.Is(err, unix.EROFS) {
			return fmt.Errorf("expected EROFS, got %w", err)
		}
		return nil
	})
	step("watch", watchChange)

	if failed {
		fmt.Println("STORAGE RESULT: FAIL")
	} else {
		fmt.Println("STORAGE RESULT: PASS")
	}
	finish()
}

func setup() error {
	base := []struct{ target, fstype string }{
		{"/proc", "proc"},
		{"/sys", "sysfs"},
		{"/dev", "devtmpfs"},
	}
	for _, m := range base {
		if err := unix.Mount(m.fstype, m.target, m.fstype, 0, ""); err != nil && !errors.Is(err, unix.EBUSY) {
			return fmt.Errorf("mount %s: %w", m.target, err)
		}
	}
	for _, dir := range []string{rwTarget, roTarget} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// watchChange verifies the shared filesystem delivers inotify events: it watches
// the read-write share for a creation, then creates a file and waits briefly.
func watchChange() error {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return fmt.Errorf("inotify init: %w", err)
	}
	defer unix.Close(fd)
	if _, err := unix.InotifyAddWatch(fd, rwTarget, unix.IN_CREATE); err != nil {
		return fmt.Errorf("inotify add watch: %w", err)
	}
	if err := os.WriteFile(rwTarget+"/watch-trigger.txt", []byte("x"), 0o644); err != nil {
		return err
	}
	buf := make([]byte, 4096)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if n, err := unix.Read(fd, buf); n > 0 {
			return nil
		} else if err != nil && !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EINTR) {
			return fmt.Errorf("inotify read: %w", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("no inotify event observed within 2s")
}

// finish resets the VM; with QEMU -no-reboot this exits the emulator.
func finish() {
	fmt.Println("STORAGE REBOOT")
	_ = unix.Reboot(unix.LINUX_REBOOT_CMD_RESTART)
	os.Exit(0)
}
