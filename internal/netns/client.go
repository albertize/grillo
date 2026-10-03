//go:build linux

// SPDX-License-Identifier: Apache-2.0

package netns

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"

	linux "grillo.local/grillo/internal/platform/linux"
)

// Client talks to a supervisor over its Unix socket.
type Client struct {
	SocketPath string
}

// request sends one command and reads one response.
func (c *Client) request(request Request) (Response, error) {
	conn, err := net.DialTimeout("unix", c.SocketPath, 5*time.Second)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	data, err := json.Marshal(request)
	if err != nil {
		return Response{}, err
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return Response{}, err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return Response{}, err
	}
	var response Response
	if err := json.Unmarshal(line, &response); err != nil {
		return Response{}, err
	}
	if !response.OK {
		return response, fmt.Errorf("netns: %s", response.Error)
	}
	return response, nil
}

// Ping checks the supervisor.
func (c *Client) Ping() error {
	_, err := c.request(Request{Op: "ping"})
	return err
}

// Launch creates a TAP and starts the VMM inside the namespace, returning its PID.
func (c *Client) Launch(tap, qemu string, args []string, logPath, mac string) (int, error) {
	response, err := c.request(Request{Op: "launch", Tap: tap, QEMU: qemu, Args: args, LogPath: logPath, MAC: mac})
	if err != nil {
		return 0, err
	}
	return response.PID, nil
}

// Remove deletes a TAP.
func (c *Client) Remove(tap string) error {
	_, err := c.request(Request{Op: "remove", Tap: tap})
	return err
}

// Helper is a running application network supervisor (a pasta process hosting
// the supervisor).
type Helper struct {
	cmd  *exec.Cmd
	id   linux.ProcessID
	done chan error
}

// SupervisorCommand describes how to run the supervisor binary.
type SupervisorCommand struct {
	Pasta string   // defaults to "pasta"
	Bin   string   // supervisor binary
	Args  []string // extra supervisor arguments
}

// StartSupervisor starts pasta with the supervisor inside and waits for its
// socket.
func StartSupervisor(ctx context.Context, cfg Config, command SupervisorCommand, log *os.File, timeout time.Duration) (*Helper, error) {
	if command.Pasta == "" {
		command.Pasta = "pasta"
	}
	if command.Bin == "" {
		return nil, fmt.Errorf("netns: supervisor binary is required")
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	args := []string{
		"-f", "-4", "-I", cfg.Uplink,
		"--config-net", "--no-map-gw",
		"-t", "none", "-u", "none", "-T", "none", "-U", "none",
		"--",
		command.Bin,
	}
	args = append(args, command.Args...)
	cmd := exec.Command(command.Pasta, args...)
	if log != nil {
		cmd.Stdout, cmd.Stderr = log, log
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
	helper := &Helper{cmd: cmd, id: id, done: make(chan error, 1)}
	go func() { helper.done <- cmd.Wait() }()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(cfg.SocketPath); err == nil {
			if err := (&Client{SocketPath: cfg.SocketPath}).Ping(); err == nil {
				return helper, nil
			}
		}
		select {
		case <-ctx.Done():
			_ = helper.Stop(2 * time.Second)
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	_ = helper.Stop(2 * time.Second)
	return nil, fmt.Errorf("netns: supervisor did not become ready within %s", timeout)
}

// PID returns the pasta process id.
func (h *Helper) PID() int { return h.id.PID }

// Alive reports whether the helper is running.
func (h *Helper) Alive() bool { return h.id.Alive() }

// Stop terminates the helper process group.
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
