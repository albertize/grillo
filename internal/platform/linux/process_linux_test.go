//go:build linux

// SPDX-License-Identifier: Apache-2.0

package linux

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestIdentifyCurrentProcess(t *testing.T) {
	id, err := Identify(1)
	if err != nil {
		t.Fatalf("identify init: %v", err)
	}
	if id.PID != 1 || id.StartTime == 0 || id.BootID == "" {
		t.Fatalf("identity = %+v", id)
	}
	if !id.Alive() {
		t.Fatal("init should be alive")
	}
}

func TestIdentifyRejectsBadPID(t *testing.T) {
	if _, err := Identify(0); err == nil {
		t.Fatal("expected error for pid 0")
	}
}

func TestRecycledPIDNotSignaled(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	id, err := Identify(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	// A stale identity with a different start time must not match.
	stale := id
	stale.StartTime++
	if stale.Alive() {
		t.Fatal("stale identity should not be alive")
	}
	delivered, err := Signal(stale, syscall.SIGKILL)
	if err != nil {
		t.Fatal(err)
	}
	if delivered {
		t.Fatal("stale identity must not deliver a signal")
	}
	if !id.Alive() {
		t.Fatal("process should still be alive after a stale signal")
	}
	// A boot ID mismatch is also rejected.
	wrongBoot := id
	wrongBoot.BootID = "not-this-boot"
	if delivered, _ := Signal(wrongBoot, syscall.SIGKILL); delivered {
		t.Fatal("boot ID mismatch must not deliver a signal")
	}
}

func TestSignalLiveProcess(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	id, err := Identify(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	delivered, err := Signal(id, syscall.SIGKILL)
	if err != nil || !delivered {
		t.Fatalf("signal delivered=%v err=%v", delivered, err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit after SIGKILL")
	}
	if id.Alive() {
		t.Fatal("identity should not be alive after exit")
	}
}
