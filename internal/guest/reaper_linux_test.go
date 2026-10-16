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

func TestReaperConcurrentWaitAndDeliveryNeverLosesExit(t *testing.T) {
	r := NewReaper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for pid := 1; pid <= 20000; pid++ {
		start := make(chan struct{})
		delivered := make(chan struct{})
		waited := make(chan ExitStatus, 1)
		want := ExitStatus{PID: pid, ExitCode: pid % 127}
		go func() { <-start; waited <- r.Wait(want.PID) }()
		go func() { <-start; r.deliver(want.PID, want); close(delivered) }()
		close(start)
		select {
		case <-delivered:
		case <-deadline.C:
			t.Fatalf("exit delivery itself timed out at pid %d", pid)
		}
		// Delivery must atomically choose the existing waiter or retained status.
		// If both exist after delivery, Wait has been stranded. Rescue only that
		// failed fixture's goroutine so reporting a regression does not leak it.
		r.mu.Lock()
		ch := r.waiters[pid]
		_, retained := r.retained[pid]
		lost := ch != nil && retained
		if lost {
			delete(r.waiters, pid)
			delete(r.retained, pid)
			ch <- want
		}
		r.mu.Unlock()
		select {
		case got := <-waited:
			if lost || got != want {
				t.Fatalf("exit delivery lost to concurrent Wait: pid=%d stranded=%v got=%+v", pid, lost, got)
			}
		case <-deadline.C:
			t.Fatalf("concurrent exit delivery timed out at pid %d", pid)
		}
	}
}
