//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// stopExec terminates the runc-reported exec process before its supervisor.
// The file is created by runc in an operation-owned private directory outside
// container roots. Parent identity and a pidfd prevent signaling a reused PID
// or an arbitrary workload's main process. There is no unsafe PID-only fallback.
func stopExec(pidFile string, supervisor int) error {
	deadline := time.Now().Add(500 * time.Millisecond)
	var data []byte
	var err error
	for {
		data, err = readCapped(pidFile, 128)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 || pid == supervisor {
		return errors.New("invalid exec PID")
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	end := strings.LastIndexByte(string(stat), ')')
	if end < 0 {
		return errors.New("invalid exec process identity")
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 2 {
		return errors.New("invalid exec process identity")
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil || parent != supervisor {
		return errors.New("exec process does not belong to supervisor")
	}
	if err := unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
		return err
	}
	// Wait for process exit (not reaping: PID 1's Reaper is the sole wait owner).
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	_, err = unix.Poll(poll, 500)
	if err != nil {
		return err
	}
	if poll[0].Revents&unix.POLLIN == 0 {
		return errors.New("exec process did not exit")
	}
	return nil
}
