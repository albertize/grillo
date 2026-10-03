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
	"encoding/binary"
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
		if string(data) != "host-sentinel-v1\n" {
			return fmt.Errorf("unexpected initial sentinel: %q", data)
		}
		return nil
	})
	step("host-update", liveHostUpdate)
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
	step("guest-watch", watchChange)

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

// liveHostUpdate verifies a host write made AFTER the guest read and installed
// its watch. Content coherence and remote inotify delivery are separate results.
func liveHostUpdate() error {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if _, err := unix.InotifyAddWatch(fd, rwTarget+"/host-sentinel.txt", unix.IN_MODIFY|unix.IN_CLOSE_WRITE); err != nil {
		return err
	}
	if err := os.WriteFile(rwTarget+"/guest-ready", []byte("ready\n"), 0o644); err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	var visibleAt time.Time
	notified := false
	buf := make([]byte, 4096)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(rwTarget + "/host-sentinel.txt")
		if err != nil {
			return err
		}
		if string(data) == "host-sentinel-v2\n" && visibleAt.IsZero() {
			visibleAt = time.Now()
		}
		n, err := unix.Read(fd, buf)
		if err != nil && !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EINTR) {
			return err
		}
		for off := 0; off+unix.SizeofInotifyEvent <= n; {
			mask := binary.NativeEndian.Uint32(buf[off+4 : off+8])
			notified = notified || mask&(unix.IN_MODIFY|unix.IN_CLOSE_WRITE) != 0
			off += unix.SizeofInotifyEvent + int(binary.NativeEndian.Uint32(buf[off+12:off+16]))
		}
		if !visibleAt.IsZero() && (notified || time.Since(visibleAt) >= 2*time.Second) {
			if notified {
				fmt.Println("STORAGE host-watch SUPPORTED: host write event observed")
			} else {
				fmt.Println("STORAGE host-watch DEGRADED: no host write event within 2s; polling required")
			}
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("host update not visible within 10s")
}

// watchChange verifies guest-originated inotify events only: it watches
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
