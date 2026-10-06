//go:build linux

// SPDX-License-Identifier: Apache-2.0

package netns

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"

	linux "github.com/albertize/grillo/internal/platform/linux"
)

// Client talks to a supervisor over its Unix socket.
type Client struct {
	SocketPath string
}

// request sends one command and reads one response.
func (c *Client) request(request Request) (Response, error) {
	return c.requestContext(context.Background(), request)
}

func (c *Client) requestContext(ctx context.Context, request Request) (Response, error) {
	response, conn, err := c.exchange(ctx, request)
	if conn != nil {
		_ = conn.Close()
	}
	return response, err
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (c *bufferedConn) CloseWrite() error {
	if c, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return c.CloseWrite()
	}
	return nil
}

func (c *Client) exchange(ctx context.Context, request Request) (Response, net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	request.Deadline = deadline.UnixMilli()
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", c.SocketPath)
	if err != nil {
		return Response{}, nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	fail := func(err error) (Response, net.Conn, error) { _ = conn.Close(); return Response{}, nil, err }
	_ = conn.SetDeadline(deadline)
	data, err := json.Marshal(request)
	if err != nil {
		return fail(err)
	}
	if len(data) > 1<<20 {
		return fail(fmt.Errorf("netns: request limit exceeded"))
	}
	if _, err := io.Copy(conn, bytes.NewReader(append(data, '\n'))); err != nil {
		return fail(err)
	}
	reader := bufio.NewReaderSize(conn, 4096)
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		if len(line)+len(part) > 1<<20 {
			return fail(fmt.Errorf("netns: response limit exceeded"))
		}
		line = append(line, part...)
		if err == nil {
			break
		}
		if err != bufio.ErrBufferFull {
			return fail(err)
		}
	}
	var response Response
	if err := json.Unmarshal(line, &response); err != nil {
		return fail(err)
	}
	if !response.OK {
		return fail(fmt.Errorf("netns: %s", response.Error))
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	_ = conn.SetDeadline(time.Time{})
	return response, &bufferedConn{Conn: conn, reader: reader}, nil
}

// UpdateServices publishes a complete, bounded application snapshot.
func (c *Client) UpdateServices(ctx context.Context, updates []ServiceUpdate) (map[string]string, error) {
	response, err := c.requestContext(ctx, Request{Op: "services", Services: updates})
	return response.VIPs, err
}

// DialService relays only a declared Service port through the private socket.
func (c *Client) DialService(ctx context.Context, address string) (net.Conn, error) {
	_, conn, err := c.exchange(ctx, Request{Op: "connect-service", Target: address})
	return conn, err
}

// Ping checks the supervisor.
func (c *Client) Ping() error {
	_, err := c.request(Request{Op: "ping"})
	return err
}

func (c *Client) PingContext(ctx context.Context) error {
	_, err := c.requestContext(ctx, Request{Op: "ping"})
	return err
}
func (c *Client) ShutdownContext(ctx context.Context) error {
	_, err := c.requestContext(ctx, Request{Op: "shutdown"})
	return err
}

// Shutdown stops an idle supervisor; it refuses to orphan live VMMs.
func (c *Client) Shutdown() error {
	_, err := c.request(Request{Op: "shutdown"})
	return err
}

// Launch creates a TAP and starts the VMM inside the namespace. It returns a
// handle that stops the VMM through the supervisor, because the VMM runs in the
// namespace's PID namespace and has no host-visible PID.
func (c *Client) Launch(tap, qemu string, args []string, logPath, mac string) (*VMM, error) {
	if _, err := c.request(Request{Op: "launch", Tap: tap, QEMU: qemu, Args: args, LogPath: logPath, MAC: mac}); err != nil {
		return nil, err
	}
	return &VMM{tap: tap, client: c}, nil
}

// VMM is a VMM running inside the namespace.
type VMM struct {
	tap    string
	client *Client
}

// Alive reports whether the VMM is still running.
func (v *VMM) Alive() bool {
	response, err := v.client.request(Request{Op: "alive", Tap: v.tap})
	return err == nil && response.Alive
}

// Stop kills the VMM and removes its TAP.
func (v *VMM) Stop(time.Duration) error {
	_, err := v.client.request(Request{Op: "stop", Tap: v.tap})
	return err
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
		"--config-net", "--no-map-gw", "--dns-forward", "169.254.1.1",
		"-t", "none", "-u", "none", "-T", "none", "-U", "none",
		"--",
		command.Bin,
	}
	args = append(args, command.Args...)
	cmd := exec.Command(command.Pasta, args...)
	if log != nil {
		cmd.Stdout, cmd.Stderr = log, log
	}
	// Go may retire the launching OS thread, and applications outlive the
	// daemon. Neither event must kill their private namespace supervisor.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
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
