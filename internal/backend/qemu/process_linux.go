//go:build linux

// SPDX-License-Identifier: Apache-2.0

package qemu

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"

	linux "github.com/albertize/grillo/internal/platform/linux"
)

// process supervises one child with a stable identity and a process group.
type process struct {
	cmd  *exec.Cmd
	id   linux.ProcessID
	done chan error
}

func startProcess(path string, log *os.File, args ...string) (*process, error) {
	cmd := exec.Command(path, args...)
	if log != nil {
		cmd.Stdout = log
		cmd.Stderr = log
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	id, err := linux.Identify(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	p := &process{cmd: cmd, id: id, done: make(chan error, 1)}
	go func() { p.done <- cmd.Wait() }()
	return p, nil
}

// externalProcess wraps a process started outside this process (by the network
// supervisor) using its identity only.
func externalProcess(pid int) (*process, error) {
	id, err := linux.Identify(pid)
	if err != nil {
		return nil, err
	}
	return &process{id: id}, nil
}

func (p *process) alive() bool { return p != nil && p.id.Alive() }

// Alive implements sandbox.VMM.
func (p *process) Alive() bool { return p.alive() }

// Stop implements sandbox.VMM.
func (p *process) Stop(grace time.Duration) error { return p.stop(grace) }

func processID(p *process) *persistedProcess {
	if p == nil {
		return nil
	}
	return &persistedProcess{
		PID:        p.id.PID,
		BootID:     p.id.BootID,
		StartTime:  p.id.StartTime,
		Executable: p.id.Executable,
	}
}

func processFromPersisted(pp *persistedProcess) linux.ProcessID {
	if pp == nil {
		return linux.ProcessID{}
	}
	return linux.ProcessID{PID: pp.PID, BootID: pp.BootID, StartTime: pp.StartTime, Executable: pp.Executable}
}

// stop terminates the process group: SIGTERM, then SIGKILL after grace. Signals
// are identity-verified, so a recycled PID is never killed.
func (p *process) stop(grace time.Duration) error {
	if p == nil {
		return nil
	}
	if p.done == nil {
		if !p.id.Alive() {
			return nil
		}
		if _, err := linux.SignalGroup(p.id, syscall.SIGTERM); err != nil {
			return err
		}
		deadline := time.Now().Add(grace)
		for time.Now().Before(deadline) && p.id.Alive() {
			time.Sleep(50 * time.Millisecond)
		}
		if p.id.Alive() {
			_, err := linux.SignalGroup(p.id, syscall.SIGKILL)
			return err
		}
		return nil
	}
	select {
	case <-p.done:
		return nil
	default:
	}
	if _, err := linux.SignalGroup(p.id, syscall.SIGTERM); err != nil {
		return err
	}
	select {
	case <-p.done:
		return nil
	case <-time.After(grace):
	}
	_, err := linux.SignalGroup(p.id, syscall.SIGKILL)
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
	}
	return err
}

// wait blocks until the process exits or the timeout elapses.
func (p *process) wait(ctx context.Context, timeout time.Duration) error {
	if p == nil {
		return nil
	}
	if p.done == nil {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			if !p.id.Alive() {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
		}
		return context.DeadlineExceeded
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case err := <-p.done:
		return err
	case <-tctx.Done():
		return tctx.Err()
	}
}
