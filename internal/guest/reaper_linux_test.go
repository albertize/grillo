//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func startThroughReaper(t *testing.T, r *Reaper, name string, args ...string) int {
	t.Helper()
	cmd := exec.Command(name, args...)
	if err := r.Start(cmd); err != nil {
		t.Fatalf("start %s: %v", name, err)
	}
	return cmd.Process.Pid
}

func TestReaperExitCodes(t *testing.T) {
	r := NewReaper()
	r.Run()
	defer r.Stop()

	pid := startThroughReaper(t, r, "/bin/sh", "-c", "exit 3")
	if status := r.Wait(pid); status.ExitCode != 3 {
		t.Fatalf("exit code = %d, want 3 (%+v)", status.ExitCode, status)
	}
	pid = startThroughReaper(t, r, "/bin/true")
	if status := r.Wait(pid); status.ExitCode != 0 || status.Signaled {
		t.Fatalf("true status = %+v", status)
	}
}

func TestReaperSignal(t *testing.T) {
	r := NewReaper()
	r.Run()
	defer r.Stop()

	pid := startThroughReaper(t, r, "/bin/sleep", "30")
	time.Sleep(50 * time.Millisecond)
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	status := r.Wait(pid)
	if !status.Signaled || status.Signal != syscall.SIGKILL {
		t.Fatalf("status = %+v, want SIGKILL", status)
	}
	if status.ExitCode != 128+int(syscall.SIGKILL) {
		t.Fatalf("exit code = %d, want %d", status.ExitCode, 128+int(syscall.SIGKILL))
	}
}

func TestReaperWaitAfterReap(t *testing.T) {
	r := NewReaper()
	r.Run()
	defer r.Stop()

	pid := startThroughReaper(t, r, "/bin/true")
	time.Sleep(100 * time.Millisecond) // let the loop reap and retain it
	if status := r.Wait(pid); status.ExitCode != 0 {
		t.Fatalf("late wait status = %+v", status)
	}
}

func TestReaperConcurrentChildren(t *testing.T) {
	r := NewReaper()
	r.Run()
	defer r.Stop()

	pids := []int{
		startThroughReaper(t, r, "/bin/true"),
		startThroughReaper(t, r, "/bin/sh", "-c", "exit 0"),
		startThroughReaper(t, r, "/bin/sh", "-c", "exit 7"),
	}
	codes := map[int]int{}
	for _, pid := range pids {
		status := r.Wait(pid)
		codes[status.PID] = status.ExitCode
	}
	if codes[pids[0]] != 0 || codes[pids[1]] != 0 || codes[pids[2]] != 7 {
		t.Fatalf("codes = %v", codes)
	}
}
