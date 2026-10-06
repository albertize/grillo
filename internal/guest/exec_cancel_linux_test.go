//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestExecCancelChild(t *testing.T) {
	if os.Getenv("GRILLO_EXEC_CANCEL_CHILD") != "1" {
		return
	}
	for {
		time.Sleep(time.Second)
	}
}

func TestStopExecUsesOwnedPidfd(t *testing.T) {
	fd, err := unix.PidfdOpen(os.Getpid(), 0)
	if errors.Is(err, unix.ENOSYS) {
		t.Skip("pidfds unavailable on this host kernel")
	}
	if err != nil {
		t.Fatal(err)
	}
	unix.Close(fd)
	cmd := exec.Command(os.Args[0], "-test.run=^TestExecCancelChild$")
	cmd.Env = append(os.Environ(), "GRILLO_EXEC_CANCEL_CHILD=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("test child did not exit")
		}
	})
	pidFile := filepath.Join(t.TempDir(), "pid")
	if err := os.WriteFile(pidFile, []byte(fmt.Sprint(cmd.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := stopExec(pidFile, os.Getpid()+1000000); err == nil {
		t.Fatal("wrong supervisor accepted")
	}
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("unowned identity was signaled", err)
	}
	if err := stopExec(pidFile, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	// Wait belongs to cmd's single owner, not to stopExec.
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected signal exit")
		}
		done <- err
	case <-time.After(time.Second):
		t.Fatal("exec child remained alive")
	}
}

func TestStopExecRejectsInvalidPidFiles(t *testing.T) {
	file := filepath.Join(t.TempDir(), "pid")
	for _, value := range []string{"not-a-pid", "0", "1", fmt.Sprint(os.Getpid()), "-1"} {
		if err := os.WriteFile(file, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if err := stopExec(file, os.Getpid()); err == nil {
			t.Fatalf("accepted invalid PID %q", value)
		}
	}
	if err := stopExec(file+"-missing", os.Getpid()); err == nil {
		t.Fatal("missing PID file accepted")
	}
}
