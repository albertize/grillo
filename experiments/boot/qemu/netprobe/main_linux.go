//go:build linux

// SPDX-License-Identifier: Apache-2.0

// Command netprobe is the T03/QEMU guest PID 1 for the rootless-networking
// experiment. The host boots it with QEMU user-mode networking and the kernel
// cmdline `ip=dhcp`, so the kernel configures the interface. The probe then
// verifies TCP egress and DNS through QEMU's user-mode NAT and prints results to
// the serial console before resetting the VM.
//
// It is experiment code, not the Grillo runtime.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func main() {
	if err := setup(); err != nil {
		fmt.Println("NET SETUP FAIL:", err)
		finish()
	}

	failed := false
	step := func(name string, fn func() error) {
		if err := fn(); err != nil {
			failed = true
			fmt.Printf("NET %-10s FAIL: %v\n", name, err)
			return
		}
		fmt.Printf("NET %-10s ok\n", name)
	}

	step("egress-tcp", func() error {
		var last error
		for attempt := 1; attempt <= 20; attempt++ {
			conn, err := net.DialTimeout("tcp", "1.1.1.1:443", 3*time.Second)
			if err == nil {
				_ = conn.Close()
				return nil
			}
			last = err
			time.Sleep(500 * time.Millisecond)
		}
		return last
	})

	step("dns", func() error {
		resolver := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: 5 * time.Second}
				return d.DialContext(ctx, "udp", "10.0.2.3:53")
			},
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ips, err := resolver.LookupHost(ctx, "example.com")
		if err != nil {
			return err
		}
		fmt.Printf("NET resolved-example-com %v\n", ips)
		return nil
	})

	if failed {
		fmt.Println("NET RESULT: FAIL")
	} else {
		fmt.Println("NET RESULT: PASS")
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
	return nil
}

// finish resets the VM; with QEMU -no-reboot this exits the emulator.
func finish() {
	fmt.Println("NET REBOOT")
	_ = unix.Reboot(unix.LINUX_REBOOT_CMD_RESTART)
	os.Exit(0)
}
