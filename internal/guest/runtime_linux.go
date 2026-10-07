//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
)

// maxCapturedBytes bounds output captured from one runc command.
const maxCapturedBytes = 4 << 20

// ContainerState is a container's observed runtime state.
type ContainerState struct {
	Status string `json:"status"`
	PID    int    `json:"pid"`
}

// Runtime runs and manages OCI containers in the guest. The interface keeps the
// agent's lifecycle logic testable; Runc is the production implementation.
type Runtime interface {
	// Run runs an init container to completion, capturing bounded output.
	Run(ctx context.Context, name, bundle string) (ExitStatus, []byte, []byte, error)
	// RunStreaming runs a container to completion, streaming output.
	RunStreaming(ctx context.Context, name, bundle string, stdout, stderr io.Writer) (ExitStatus, error)
	// StartDetached starts a long-lived container and returns once runc reports it.
	StartDetached(ctx context.Context, name, bundle string) error
	// Exec runs a command in a running container, capturing bounded output.
	Exec(ctx context.Context, name string, args []string) (ExitStatus, []byte, []byte, error)
	// Kill sends a signal to a container.
	Kill(ctx context.Context, name string, sig syscall.Signal) error
	// Delete removes a container, ignoring an already-absent container.
	Delete(ctx context.Context, name string, force bool) error
	// State reports a container's state.
	State(ctx context.Context, name string) (ContainerState, error)
}

// Runc is the runc-backed Runtime. It routes every child through the Reaper so
// PID 1 reaps them exactly once.
type Runc struct {
	Binary     string
	Root       string
	Reaper     *Reaper
	Logs       *ContainerLogs
	logMu      sync.Mutex
	logReaders map[string][]*os.File
}

func (r *Runc) command(args ...string) *exec.Cmd {
	return exec.Command(r.Binary, append([]string{"--root", r.Root}, args...)...)
}

// Run implements Runtime.
func (r *Runc) Run(ctx context.Context, name, bundle string) (ExitStatus, []byte, []byte, error) {
	return r.capture(ctx, r.command("run", "--no-pivot", "--bundle", bundle, name))
}

// RunStreaming implements Runtime.
func (r *Runc) RunStreaming(ctx context.Context, name, bundle string, stdout, stderr io.Writer) (ExitStatus, error) {
	cmd := r.command("run", "--no-pivot", "--bundle", bundle, name)
	// Reaper owns wait4, so never give os/exec a non-file Writer (it would
	// create copy goroutines that require cmd.Wait). Own the pipes explicitly.
	outR, outW, err := os.Pipe()
	if err != nil {
		return ExitStatus{}, err
	}
	defer outR.Close()
	defer outW.Close()
	errR, errW, err := os.Pipe()
	if err != nil {
		return ExitStatus{}, err
	}
	defer errR.Close()
	defer errW.Close()
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	done := make(chan error, 2)
	go func() { defer outR.Close(); _, err := io.Copy(stdout, outR); done <- err }()
	go func() { defer errR.Close(); _, err := io.Copy(stderr, errR); done <- err }()
	cmd.Stdout, cmd.Stderr = outW, errW
	if err := r.Reaper.Start(cmd); err != nil {
		return ExitStatus{}, err
	}
	_ = outW.Close()
	_ = errW.Close()
	status, err := r.wait(ctx, cmd.Process.Pid)
	for i := 0; i < 2; i++ {
		select {
		case copyErr := <-done:
			if copyErr != nil && err == nil {
				err = fmt.Errorf("guest: container output stream failed")
			}
		case <-ctx.Done():
			return status, ctx.Err()
		}
	}
	return status, err
}

// StartDetached implements Runtime.
func (r *Runc) StartDetached(ctx context.Context, name, bundle string) error {
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer devNull.Close()
	cmd := r.command("run", "-d", "--no-pivot", "--bundle", bundle, name)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devNull, devNull, devNull
	var writers []*os.File
	if r.Logs != nil {
		r.closeLogReaders(name)
		var readers []*os.File
		for _, stream := range []string{"stdout", "stderr"} {
			reader, writer, err := os.Pipe()
			if err != nil {
				for _, f := range append(readers, writers...) {
					_ = f.Close()
				}
				return err
			}
			readers = append(readers, reader)
			writers = append(writers, writer)
			go func(reader *os.File, stream string) {
				defer reader.Close()
				_, _ = io.Copy(r.Logs.Writer(name, stream), reader)
			}(reader, stream)
		}
		r.logMu.Lock()
		if r.logReaders == nil {
			r.logReaders = map[string][]*os.File{}
		}
		r.logReaders[name] = readers
		r.logMu.Unlock()
		cmd.Stdout, cmd.Stderr = writers[0], writers[1]
	}
	defer func() {
		for _, writer := range writers {
			_ = writer.Close()
		}
	}()
	if err := r.Reaper.Start(cmd); err != nil {
		r.closeLogReaders(name)
		return err
	}
	status, err := r.wait(ctx, cmd.Process.Pid)
	if err != nil || status.ExitCode != 0 {
		r.closeLogReaders(name)
		if err != nil {
			return err
		}
		return fmt.Errorf("guest: runc run -d %s exited %d", name, status.ExitCode)
	}
	return nil
}

func (r *Runc) closeLogReaders(name string) {
	r.logMu.Lock()
	readers := r.logReaders[name]
	delete(r.logReaders, name)
	r.logMu.Unlock()
	for _, reader := range readers {
		_ = reader.Close()
	}
}

// Exec implements Runtime.
func (r *Runc) Exec(ctx context.Context, name string, args []string) (ExitStatus, []byte, []byte, error) {
	dir, err := os.MkdirTemp("", "grillo-exec-")
	if err != nil {
		return ExitStatus{}, nil, nil, err
	}
	defer os.RemoveAll(dir)
	pidFile := filepath.Join(dir, "pid")
	cmd := r.command(append([]string{"exec", "--pid-file", pidFile, name}, args...)...)
	return r.captureWithCancel(ctx, cmd, func() error { return stopExec(pidFile, cmd.Process.Pid) })
}

// Kill implements Runtime.
func (r *Runc) Kill(ctx context.Context, name string, sig syscall.Signal) error {
	_, _, stderr, err := r.capture(ctx, r.command("kill", name, strconv.Itoa(int(sig))))
	if err != nil {
		return err
	}
	if len(stderr) > 0 {
		return fmt.Errorf("guest: kill %s: %s", name, stderr)
	}
	return nil
}

// Delete implements Runtime.
func (r *Runc) Delete(ctx context.Context, name string, force bool) error {
	defer r.closeLogReaders(name)
	args := []string{"delete"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, name)
	_, _, stderr, err := r.capture(ctx, r.command(args...))
	if err != nil {
		return err
	}
	if len(stderr) > 0 {
		return fmt.Errorf("guest: delete %s: %s", name, stderr)
	}
	return nil
}

// State implements Runtime.
func (r *Runc) State(ctx context.Context, name string) (ContainerState, error) {
	_, stdout, stderr, err := r.capture(ctx, r.command("state", name))
	if err != nil {
		return ContainerState{}, err
	}
	var state ContainerState
	if err := json.Unmarshal(stdout, &state); err != nil {
		return ContainerState{}, fmt.Errorf("guest: parse runc state for %s: %w (stderr %s)", name, err, stderr)
	}
	return state, nil
}

// capture runs cmd, writing stdout/stderr to temporary files (bounded on read)
// and reaping the process through the Reaper.
func (r *Runc) capture(ctx context.Context, cmd *exec.Cmd) (ExitStatus, []byte, []byte, error) {
	return r.captureWithCancel(ctx, cmd, nil)
}

func (r *Runc) captureWithCancel(ctx context.Context, cmd *exec.Cmd, onCancel func() error) (ExitStatus, []byte, []byte, error) {
	outF, err := os.CreateTemp("", "grillo-stdout-*")
	if err != nil {
		return ExitStatus{}, nil, nil, err
	}
	defer os.Remove(outF.Name())
	defer outF.Close()
	errF, err := os.CreateTemp("", "grillo-stderr-*")
	if err != nil {
		return ExitStatus{}, nil, nil, err
	}
	defer os.Remove(errF.Name())
	defer errF.Close()
	cmd.Stdout = outF
	cmd.Stderr = errF
	if err := r.Reaper.Start(cmd); err != nil {
		return ExitStatus{}, nil, nil, err
	}
	status, waitErr := r.waitWithCancel(ctx, cmd.Process.Pid, onCancel)
	stdout, _ := readCapped(outF.Name(), maxCapturedBytes)
	stderr, _ := readCapped(errF.Name(), maxCapturedBytes)
	return status, stdout, stderr, waitErr
}

func (r *Runc) wait(ctx context.Context, pid int) (ExitStatus, error) {
	return r.waitWithCancel(ctx, pid, nil)
}

func (r *Runc) waitWithCancel(ctx context.Context, pid int, onCancel func() error) (ExitStatus, error) {
	done := make(chan ExitStatus, 1)
	go func() { done <- r.Reaper.Wait(pid) }()
	select {
	case status := <-done:
		return status, nil
	case <-ctx.Done():
		var cleanupErr error
		if onCancel != nil {
			cleanupErr = onCancel()
		}
		_ = syscall.Kill(pid, syscall.SIGKILL)
		status := <-done
		if cleanupErr != nil {
			return status, fmt.Errorf("guest: exec cancellation cleanup failed: %w", ctx.Err())
		}
		return status, ctx.Err()
	}
}

func readCapped(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit))
}
