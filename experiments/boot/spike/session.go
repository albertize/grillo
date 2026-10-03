//go:build linux && amd64

// SPDX-License-Identifier: Apache-2.0

// Package spike holds the reusable T01/T02 feasibility session: it boots one
// Firecracker microVM as the calling user and exposes a vsock exec/stop session.
// It is experiment code, not the Grillo runtime, and speaks a throwaway protocol
// that T06 replaces.
package spike

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Options configures one microVM session.
type Options struct {
	Firecracker string        // path to the firecracker binary
	Kernel      string        // path to an uncompressed vmlinux
	Initramfs   string        // path to a gzip-compressed cpio initramfs
	WorkDir     string        // parent for the per-session temporary directory
	VsockPort   uint32        // guest port the agent listens on
	GuestCID    uint32        // vsock guest context id (Firecracker device)
	VsockCID    uint32        // if >0, connect via AF_VSOCK to this guest CID and skip launching Firecracker (QEMU vhost-vsock)
	VCPU        uint32        // vCPU count
	MemMiB      uint32        // guest memory
	BootTimeout time.Duration // boot + handshake deadline
	StopTimeout time.Duration // graceful-stop deadline before SIGKILL
	Command     string        // guest binary executed by RunOnce
	Args        []string      // arguments for Command
	Log         io.Writer     // firecracker console/API log sink
}

func (o Options) withDefaults() Options {
	out := o
	if out.VsockPort == 0 {
		out.VsockPort = 1024
	}
	if out.GuestCID == 0 {
		out.GuestCID = 3
	}
	if out.VCPU == 0 {
		out.VCPU = 1
	}
	if out.MemMiB == 0 {
		out.MemMiB = 256
	}
	if out.BootTimeout == 0 {
		out.BootTimeout = 30 * time.Second
	}
	if out.StopTimeout == 0 {
		out.StopTimeout = 10 * time.Second
	}
	if out.Command == "" {
		out.Command = "/bin/grillo-testcmd"
		out.Args = []string{"alpha", "beta"}
	}
	if out.Log == nil {
		out.Log = io.Discard
	}
	return out
}

// Result captures the outcome and timings of one RunOnce cycle.
type Result struct {
	BootDuration time.Duration
	ExecDuration time.Duration
	StopDuration time.Duration
	Total        time.Duration
	Stdout       string
	Stderr       string
	ExitCode     int
}

// Session is a booted microVM with a connected vsock channel.
type Session struct {
	opts     Options
	dir      string
	vm       *firecracker
	conn     *vsockConn
	bootTime time.Duration
	stopped  bool
}

// Boot connects to a guest and waits for the handshake. By default it starts and
// configures Firecracker; if Options.VsockCID is set it instead assumes the
// caller launched a VMM exposing vsock (QEMU vhost-vsock) and dials AF_VSOCK.
func Boot(ctx context.Context, raw Options) (session *Session, err error) {
	opts := raw.withDefaults()
	start := time.Now()
	dir, err := os.MkdirTemp(opts.WorkDir, "grillo-spike-")
	if err != nil {
		return nil, fmt.Errorf("create work dir: %w", err)
	}
	// s is local, not the named return, so return nil, err does not nil it out
	// before the deferred cleanup runs.
	s := &Session{opts: opts, dir: dir}
	defer func() {
		if err != nil {
			_ = s.Close()
		}
	}()

	var conn *vsockConn
	if opts.VsockCID > 0 {
		conn, err = waitVsockAF(ctx, opts.VsockCID, opts.VsockPort, opts.BootTimeout)
	} else {
		vm, verr := startFirecracker(opts, dir)
		if verr != nil {
			return nil, verr
		}
		s.vm = vm
		vsockPath := filepath.Join(dir, "vsock.sock")
		if verr := vm.configure(ctx, opts, vsockPath); verr != nil {
			return nil, verr
		}
		conn, err = waitVsockHandshake(ctx, vsockPath, opts.VsockPort, opts.BootTimeout)
	}
	if err != nil {
		return nil, err
	}
	s.conn = conn
	s.bootTime = time.Since(start)
	return s, nil
}

// BootDuration reports the time from process start to a valid handshake.
func (s *Session) BootDuration() time.Duration { return s.bootTime }

// Exec runs one guest binary and returns its streams and exit status.
func (s *Session) Exec(path string, args ...string) (stdout, stderr string, code int, err error) {
	return s.conn.exec(path, args...)
}

// ExecDetached starts a guest binary with /dev/null stdio and returns once it
// has started, so long-lived processes do not hold the control channel open.
func (s *Session) ExecDetached(path string, args ...string) (stdout, stderr string, code int, err error) {
	return s.conn.execDetached(path, args...)
}

// Stop requests a guest shutdown and waits for the VM process to exit.
func (s *Session) Stop() error {
	if s.stopped {
		return nil
	}
	s.stopped = true
	stopErr := s.conn.stop()
	var shutdownErr error
	if s.vm != nil {
		shutdownErr = s.vm.shutdown(s.opts.StopTimeout)
	}
	_ = s.conn.Close()
	removeErr := os.RemoveAll(s.dir)
	return errors.Join(stopErr, shutdownErr, removeErr)
}

// Close force-stops the VM and removes the work directory if Stop was not called.
func (s *Session) Close() error {
	if s.stopped {
		return nil
	}
	s.stopped = true
	var err error
	if s.conn != nil {
		_ = s.conn.Close()
	}
	if s.vm != nil {
		err = s.vm.shutdown(s.opts.StopTimeout)
	}
	return errors.Join(err, os.RemoveAll(s.dir))
}

// RunOnce boots, executes Options.Command, stops, and returns timings. It is the
// T01 convenience path.
func RunOnce(ctx context.Context, raw Options) (res Result, err error) {
	start := time.Now()
	s, err := Boot(ctx, raw)
	if err != nil {
		return res, err
	}
	defer func() {
		if err == nil {
			return
		}
		_ = s.Close()
	}()
	res.BootDuration = s.bootTime

	if err := s.conn.pingPong(); err != nil {
		return res, err
	}
	execStart := time.Now()
	res.Stdout, res.Stderr, res.ExitCode, err = s.Exec(s.opts.Command, s.opts.Args...)
	res.ExecDuration = time.Since(execStart)
	if err != nil {
		return res, err
	}
	stopStart := time.Now()
	if err := s.Stop(); err != nil {
		return res, err
	}
	res.StopDuration = time.Since(stopStart)
	res.Total = time.Since(start)
	return res, nil
}

// firecracker wraps a Firecracker process and its API-over-Unix-socket client.
type firecracker struct {
	cmd    *exec.Cmd
	client *http.Client
	done   chan error
	waited bool
}

func startFirecracker(opts Options, dir string) (*firecracker, error) {
	sockPath := filepath.Join(dir, "api.sock")
	cmd := exec.Command(opts.Firecracker, "--api-sock", sockPath)
	cmd.Stdout = opts.Log
	cmd.Stderr = opts.Log
	// Own process group so a leaked child cannot escape shutdown, and PDEATHSIG
	// so the VMM dies if this harness is killed without running Stop. Firecracker
	// otherwise survives parent death and becomes an orphan (a T08 concern).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start firecracker: %w", err)
	}
	vm := &firecracker{cmd: cmd, done: make(chan error, 1)}
	go func() { vm.done <- cmd.Wait() }()
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sockPath)
		},
	}
	vm.client = &http.Client{Transport: transport}
	if err := waitForPath(sockPath, 10*time.Second); err != nil {
		_ = vm.shutdown(2 * time.Second)
		return nil, fmt.Errorf("firecracker API socket: %w", err)
	}
	return vm, nil
}

func (v *firecracker) api(ctx context.Context, method, path string, body any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode %s: %w", path, err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://localhost"+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s %s: status %d: %s", method, path, resp.StatusCode, bytes.TrimSpace(msg))
	}
	return nil
}

func (v *firecracker) configure(ctx context.Context, opts Options, vsockPath string) error {
	bootArgs := "console=ttyS0 reboot=k panic=1 pci=off rdinit=/init"
	if err := v.api(ctx, http.MethodPut, "/boot-source", map[string]any{
		"kernel_image_path": opts.Kernel,
		"initrd_path":       opts.Initramfs,
		"boot_args":         bootArgs,
	}); err != nil {
		return err
	}
	if err := v.api(ctx, http.MethodPut, "/machine-config", map[string]any{
		"vcpu_count":   opts.VCPU,
		"mem_size_mib": opts.MemMiB,
		"smt":          false,
	}); err != nil {
		return err
	}
	if err := v.api(ctx, http.MethodPut, "/vsock", map[string]any{
		"guest_cid": opts.GuestCID,
		"uds_path":  vsockPath,
	}); err != nil {
		return err
	}
	return v.api(ctx, http.MethodPut, "/actions", map[string]any{"action_type": "InstanceStart"})
}

// shutdown requests a graceful guest reboot and waits, escalating to SIGKILL.
func (v *firecracker) shutdown(timeout time.Duration) error {
	if v.waited {
		return nil
	}
	v.waited = true
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_ = v.api(ctx, http.MethodPut, "/actions", map[string]any{"action_type": "SendCtrlAltDel"})
	cancel()
	select {
	case err := <-v.done:
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				return nil
			}
			return fmt.Errorf("wait firecracker: %w", err)
		}
		return nil
	case <-time.After(timeout):
		if v.cmd.Process != nil {
			_ = syscall.Kill(-v.cmd.Process.Pid, syscall.SIGKILL)
		}
		<-v.done
		return fmt.Errorf("firecracker did not stop within %s", timeout)
	}
}

// vsockConn is the host side of a Firecracker vsock connection. It keeps the
// buffered reader because the guest handshake may share a read with the
// Firecracker CONNECT acknowledgement.
// deadlineConn is the subset of net.Conn used by the session; *os.File (used for
// AF_VSOCK) satisfies it as well.
type deadlineConn interface {
	io.Reader
	io.Writer
	io.Closer
	SetReadDeadline(t time.Time) error
}

type vsockConn struct {
	c deadlineConn
	r *bufio.Reader
}

func (c *vsockConn) readLine(timeout time.Duration) (string, error) {
	if err := c.c.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return "", err
	}
	line, err := c.r.ReadString('\n')
	return strings.TrimRight(line, "\n"), err
}

func (c *vsockConn) Close() error { return c.c.Close() }

func (c *vsockConn) Write(p []byte) (int, error) { return c.c.Write(p) }

func (c *vsockConn) pingPong() error {
	if _, err := io.WriteString(c, "PING\n"); err != nil {
		return fmt.Errorf("send PING: %w", err)
	}
	line, err := c.readLine(5 * time.Second)
	if err != nil {
		return fmt.Errorf("read PONG: %w", err)
	}
	if line != "PONG" {
		return fmt.Errorf("vsock ping: got %q, want PONG", line)
	}
	return nil
}

func (c *vsockConn) exec(path string, args ...string) (stdout, stderr string, code int, err error) {
	return c.runVerb("EXEC", path, args...)
}

func (c *vsockConn) execDetached(path string, args ...string) (stdout, stderr string, code int, err error) {
	return c.runVerb("EXECD", path, args...)
}

func (c *vsockConn) runVerb(verb, path string, args ...string) (stdout, stderr string, code int, err error) {
	fields := append([]string{verb, path}, args...)
	if _, err := io.WriteString(c, strings.Join(fields, "\t")+"\n"); err != nil {
		return "", "", 0, fmt.Errorf("send %s: %w", verb, err)
	}
	var outBuf, errBuf bytes.Buffer
	for {
		line, err := c.readLine(30 * time.Second)
		if err != nil {
			return "", "", 0, fmt.Errorf("read exec result: %w", err)
		}
		kind, payload, ok := strings.Cut(line, "\t")
		if !ok {
			return "", "", 0, fmt.Errorf("malformed exec line: %q", line)
		}
		switch kind {
		case "OUT", "ERR":
			decoded, err := base64.StdEncoding.DecodeString(payload)
			if err != nil {
				return "", "", 0, fmt.Errorf("decode %s: %w", kind, err)
			}
			if kind == "OUT" {
				outBuf.Write(decoded)
			} else {
				errBuf.Write(decoded)
			}
		case "PID":
			// Detached-start acknowledgement; the pid is not needed here.
		case "EXIT":
			if _, err := fmt.Sscanf(payload, "%d", &code); err != nil {
				return "", "", 0, fmt.Errorf("parse exit code %q: %w", payload, err)
			}
			return outBuf.String(), errBuf.String(), code, nil
		default:
			return "", "", 0, fmt.Errorf("unexpected %q line", kind)
		}
	}
}

func (c *vsockConn) stop() error {
	if _, err := io.WriteString(c, "STOP\n"); err != nil {
		return fmt.Errorf("send STOP: %w", err)
	}
	line, err := c.readLine(5 * time.Second)
	if err != nil {
		return fmt.Errorf("read BYE: %w", err)
	}
	if line != "BYE" {
		return fmt.Errorf("stop: got %q, want BYE", line)
	}
	return nil
}

// waitVsockHandshake retries the Firecracker CONNECT handshake until the guest
// agent accepts, because the guest is not listening immediately after boot.
func waitVsockHandshake(ctx context.Context, path string, port uint32, timeout time.Duration) (*vsockConn, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		conn, err := tryVsock(path, port)
		if err == nil {
			line, err := conn.readLine(10 * time.Second)
			if err == nil && strings.HasPrefix(line, "GRILLO-T01 ") {
				return conn, nil
			}
			conn.Close()
			lastErr = fmt.Errorf("unexpected handshake %q (%v)", line, err)
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("vsock handshake timed out: %w", lastErr)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func tryVsock(path string, port uint32) (*vsockConn, error) {
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(conn, "CONNECT %d\n", port); err != nil {
		conn.Close()
		return nil, err
	}
	c := &vsockConn{c: conn, r: bufio.NewReader(conn)}
	line, err := c.readLine(5 * time.Second)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !strings.HasPrefix(line, "OK ") {
		conn.Close()
		return nil, fmt.Errorf("vsock CONNECT rejected: %q", line)
	}
	return c, nil
}

// waitVsockAF retries an AF_VSOCK connection to a QEMU guest (vhost-vsock) until
// the agent handshakes or the deadline passes.
func waitVsockAF(ctx context.Context, cid, port uint32, timeout time.Duration) (*vsockConn, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		conn, err := dialAFVSock(cid, port)
		if err == nil {
			line, herr := conn.readLine(10 * time.Second)
			if herr == nil && strings.HasPrefix(line, "GRILLO-T01 ") {
				return conn, nil
			}
			conn.Close()
			lastErr = fmt.Errorf("unexpected handshake %q (%v)", line, herr)
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("vsock AF handshake timed out: %w", lastErr)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func dialAFVSock(cid, port uint32) (*vsockConn, error) {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	if err := unix.Connect(fd, &unix.SockaddrVM{CID: cid, Port: port}); err != nil {
		if !errors.Is(err, unix.EINPROGRESS) {
			_ = unix.Close(fd)
			return nil, err
		}
		pfd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLOUT}}
		if _, perr := unix.Poll(pfd, 5000); perr != nil {
			_ = unix.Close(fd)
			return nil, perr
		}
		soErr, serr := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ERROR)
		if serr != nil {
			_ = unix.Close(fd)
			return nil, serr
		}
		if soErr != 0 {
			_ = unix.Close(fd)
			return nil, unix.Errno(soErr)
		}
	}
	// A non-blocking fd lets os.File register with the runtime poller, so
	// SetReadDeadline works for line reads.
	f := os.NewFile(uintptr(fd), "vsock")
	return &vsockConn{c: f, r: bufio.NewReader(f)}, nil
}

func waitForPath(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
