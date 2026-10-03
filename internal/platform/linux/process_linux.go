//go:build linux

// SPDX-License-Identifier: Apache-2.0

// Package linux holds Linux primitives that must stay out of portable packages:
// robust process identity, signalling that refuses recycled PIDs, and small
// kernel-interface helpers.
package linux

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// ProcessID identifies a process strongly enough to survive PID reuse. A bare
// PID never authorizes termination; callers must keep the boot ID and start time
// captured when the process was created.
type ProcessID struct {
	BootID     string
	PID        int
	StartTime  uint64
	Executable string
}

// Identify reads the identity of pid from /proc.
func Identify(pid int) (ProcessID, error) {
	if pid <= 0 {
		return ProcessID{}, fmt.Errorf("platform: invalid pid %d", pid)
	}
	bootID, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ProcessID{}, fmt.Errorf("platform: read boot id: %w", err)
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ProcessID{}, err
	}
	start, err := startTimeFromStat(stat)
	if err != nil {
		return ProcessID{}, err
	}
	// /proc/<pid>/exe can be unreadable for a zombie or a process we do not own;
	// identity still works from the start time in that case.
	exe, _ := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	return ProcessID{
		BootID:     strings.TrimSpace(string(bootID)),
		PID:        pid,
		StartTime:  start,
		Executable: exe,
	}, nil
}

// startTimeFromStat extracts field 22 (starttime) from /proc/<pid>/stat. The
// comm field may contain spaces and parentheses, so parsing starts after the
// final ')'.
func startTimeFromStat(stat []byte) (uint64, error) {
	s := string(stat)
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+1 >= len(s) {
		return 0, errors.New("platform: malformed proc stat")
	}
	fields := strings.Fields(s[i+1:])
	// fields[0] is the state (field 3); starttime (field 22) is fields[19].
	const startTimeIndex = 19
	if len(fields) <= startTimeIndex {
		return 0, errors.New("platform: short proc stat")
	}
	start, err := strconv.ParseUint(fields[startTimeIndex], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("platform: parse starttime: %w", err)
	}
	return start, nil
}

// Alive reports whether pid still refers to the same running process as id. A
// zombie holds the PID but is not running, so it is not alive.
func (id ProcessID) Alive() bool {
	if id.PID <= 0 {
		return false
	}
	current, err := Identify(id.PID)
	if err != nil {
		return false
	}
	if current.BootID != id.BootID || current.StartTime != id.StartTime {
		return false
	}
	if id.Executable != "" && current.Executable != "" && current.Executable != id.Executable {
		return false
	}
	return !isZombie(id.PID)
}

func isZombie(pid int) bool {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	s := string(stat)
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+1 >= len(s) {
		return false
	}
	fields := strings.Fields(s[i+1:])
	return len(fields) > 0 && fields[0] == "Z"
}

// Signal sends sig only when pid still matches id, so a recycled PID is never
// killed. It reports whether a signal was delivered.
func Signal(id ProcessID, sig syscall.Signal) (bool, error) {
	if !id.Alive() {
		return false, nil
	}
	err := syscall.Kill(id.PID, sig)
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("platform: signal pid %d: %w", id.PID, err)
	}
	return true, nil
}

// SignalGroup is like Signal but targets the process group led by id.
func SignalGroup(id ProcessID, sig syscall.Signal) (bool, error) {
	if !id.Alive() {
		return false, nil
	}
	err := syscall.Kill(-id.PID, sig)
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("platform: signal group %d: %w", id.PID, err)
	}
	return true, nil
}
