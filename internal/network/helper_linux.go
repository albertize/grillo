//go:build linux

// SPDX-License-Identifier: Apache-2.0

package network

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"

	linux "grillo.local/grillo/internal/platform/linux"
)

// ErrHelperUnavailable means the rootless helper could not be started, usually
// because the host lacks pasta or user namespaces.
var ErrHelperUnavailable = errors.New("network: rootless helper unavailable")

// HelperConfig configures the rootless transport helper.
type HelperConfig struct {
	// Pasta is the pasta binary. Defaults to "pasta".
	Pasta string
	// Log receives the helper's output.
	Log *os.File
	// NamespaceInterface is the name given to the interface inside the new
	// namespace (pasta -I). Defaults to "grillo0".
	NamespaceInterface string
	// Address, when set, is assigned to the namespace interface (pasta -a),
	// giving each sandbox a unique address from the IPAM pool.
	Address string
	// Env is the child's environment. Defaults to the current environment.
	Env []string
}

func (c HelperConfig) withDefaults() HelperConfig {
	out := c
	if out.Pasta == "" {
		out.Pasta = "pasta"
	}
	if out.NamespaceInterface == "" {
		out.NamespaceInterface = "grillo0"
	}
	return out
}

// Helper runs a child process inside a new user+network namespace with rootless
// egress via pasta. The child's network namespace is isolated from the host and
// from other helpers; inbound forwarding is disabled so the host's management
// endpoints are unreachable.
type Helper struct {
	cmd  *exec.Cmd
	id   linux.ProcessID
	done chan error
}

// StartHelper starts child inside a fresh namespace. The caller must call Wait or
// Stop.
func StartHelper(cfg HelperConfig, child ...string) (*Helper, error) {
	cfg = cfg.withDefaults()
	if len(child) == 0 {
		return nil, errors.New("network: helper needs a child command")
	}
	if _, err := exec.LookPath(cfg.Pasta); err != nil {
		return nil, ErrHelperUnavailable
	}
	args := []string{
		"-f", "-4", "-I", cfg.NamespaceInterface,
		"--config-net", "--no-map-gw",
		"-t", "none", "-u", "none", "-T", "none", "-U", "none",
	}
	if cfg.Address != "" {
		args = append(args, "-a", cfg.Address)
	}
	args = append(args, "--")
	args = append(args, child...)
	cmd := exec.Command(cfg.Pasta, args...)
	if len(cfg.Env) == 0 {
		cmd.Env = os.Environ()
	} else {
		cmd.Env = cfg.Env
	}
	if cfg.Log != nil {
		cmd.Stdout, cmd.Stderr = cfg.Log, cfg.Log
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		return nil, ErrHelperUnavailable
	}
	id, err := linux.Identify(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	h := &Helper{cmd: cmd, id: id, done: make(chan error, 1)}
	go func() { h.done <- cmd.Wait() }()
	return h, nil
}

// Alive reports whether the helper is still running.
func (h *Helper) Alive() bool { return h.id.Alive() }

// PID returns the helper's process id.
func (h *Helper) PID() int { return h.id.PID }

// Wait blocks until the helper exits or the timeout elapses.
func (h *Helper) Wait(ctx context.Context, timeout time.Duration) error {
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case err := <-h.done:
		return err
	case <-tctx.Done():
		return tctx.Err()
	}
}

// Stop terminates the helper process group with identity verification.
func (h *Helper) Stop(grace time.Duration) error {
	select {
	case <-h.done:
		return nil
	default:
	}
	if _, err := linux.SignalGroup(h.id, syscall.SIGTERM); err != nil {
		return err
	}
	select {
	case <-h.done:
		return nil
	case <-time.After(grace):
	}
	_, err := linux.SignalGroup(h.id, syscall.SIGKILL)
	return err
}
