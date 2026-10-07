//go:build kvm

// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	linux "github.com/albertize/grillo/internal/platform/linux"
	"golang.org/x/sys/unix"
)

// exerciseHostTerminal uses an operation-owned host PTY and the actual CLI;
// no fake terminal, resize callback, or synthetic API response is substituted.
func exerciseHostTerminal(t *testing.T, base context.Context, cli, root string, env []string) {
	t.Helper()
	for _, interrupt := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(base, 15*time.Second)
		master, slave, err := linux.OpenPTY()
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		defer func() { cancel(); _ = master.Close(); _ = slave.Close() }()
		before, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		_ = unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 30, Col: 90})
		script := "echo HOST_TTY_READY; read line; stty size; echo HOST:$line; exit 7"
		if interrupt {
			script = "echo HOST_TTY_READY; exec /bin/sleep 300"
		}
		command := exec.CommandContext(ctx, cli, "exec", "-i", "-t", "scenario-c", "storage", "--", "/bin/sh", "-c", script)
		command.Env = env
		command.Dir = root
		command.Stdin, command.Stdout, command.Stderr = slave, slave, slave
		command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
		if err := command.Start(); err != nil {
			cancel()
			_ = master.Close()
			_ = slave.Close()
			t.Fatal(err)
		}
		chunks := make(chan string, 32)
		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := master.Read(buf)
				if n > 0 {
					select {
					case chunks <- string(buf[:n]):
					case <-ctx.Done():
						return
					}
				}
				if err != nil {
					return
				}
			}
		}()
		finished := make(chan error, 1)
		go func() { finished <- command.Wait() }()
		text := ""
		for !strings.Contains(text, "HOST_TTY_READY") {
			select {
			case chunk := <-chunks:
				text += chunk
			case <-ctx.Done():
				t.Fatal("CLI did not attach to host/guest terminals")
			case err := <-finished:
				t.Fatalf("CLI exited before attach: %v %q", err, text)
			}
		}
		if interrupt {
			if err := command.Process.Signal(os.Interrupt); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 47, Col: 111}); err != nil {
				t.Fatal(err)
			}
			// SIGWINCH travels through the real CLI's terminal-size observer.
			time.Sleep(100 * time.Millisecond)
			if _, err := master.Write([]byte("host-terminal-marker\n")); err != nil {
				t.Fatal(err)
			}
		}
		select {
		case err := <-finished:
			var exit *exec.ExitError
			if !errors.As(err, &exit) || !interrupt && exit.ExitCode() != 7 {
				t.Fatalf("CLI exit=%v", err)
			}
		case <-ctx.Done():
			t.Fatal("CLI did not finish interactive session")
		}
		deadline := time.After(100 * time.Millisecond)
	drain:
		for {
			select {
			case chunk := <-chunks:
				text += chunk
			case <-deadline:
				break drain
			}
		}
		after, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
		if err != nil || *before != *after {
			t.Fatal("actual host terminal modes were not restored")
		}
		if !interrupt && (!strings.Contains(text, "47 111") || !strings.Contains(text, "HOST:host-terminal-marker")) {
			t.Fatalf("actual CLI stdin/SIGWINCH failed: %q", text)
		}
		cancel()
		_ = master.Close()
		_ = slave.Close()
	}
}
