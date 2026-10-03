//go:build linux && amd64

// SPDX-License-Identifier: Apache-2.0

// Command run is the T01 host-side feasibility harness. It boots a single
// Firecracker microVM as the calling user, performs a vsock handshake, executes
// a guest command, streams the output, and stops the VM. It is an experiment,
// not the Grillo runtime: it speaks a throwaway protocol and does not create a
// Pod, reconcile state, or run any OCI runtime.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Options configures one boot/exec/stop cycle.
type Options struct {
	Firecracker string        // path to the firecracker binary
	Kernel      string        // path to an uncompressed vmlinux
	Initramfs   string        // path to a gzip-compressed cpio initramfs
	WorkDir     string        // parent for the per-cycle temporary directory
	VsockPort   uint32        // guest port the agent listens on
	GuestCID    uint32        // vsock guest context id
	VCPU        uint32        // vCPU count
	MemMiB      uint32        // guest memory
	BootTimeout time.Duration // boot + handshake deadline
	StopTimeout time.Duration // graceful-stop deadline before SIGKILL
	Command     string        // guest binary to execute
	Args        []string      // arguments for Command
	Log         io.Writer     // firecracker console/API log sink
}

// Result captures the observable outcome and timings of one cycle.
type Result struct {
	BootDuration time.Duration // process start to valid handshake
	ExecDuration time.Duration // EXEC sent to EXIT received
	StopDuration time.Duration // STOP sent to process exit
	Total        time.Duration
	Stdout       string
	Stderr       string
	ExitCode     int
}

func (o *Options) withDefaults() Options {
	out := *o
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

// RunOnce performs a full boot/handshake/exec/stop cycle and always attempts to
// terminate the VM process and remove its temporary directory.
func RunOnce(ctx context.Context, raw Options) (res Result, err error) {
	opts := raw.withDefaults()
	start := time.Now()
	dir, err := os.MkdirTemp(opts.WorkDir, "grillo-t01-")
	if err != nil {
		return res, fmt.Errorf("create work dir: %w", err)
	}
	defer os.RemoveAll(dir)

	vm, err := startFirecracker(opts, dir)
	if err != nil {
		return res, err
	}
	// Stop/cleanup must run even on early error paths.
	defer func() {
		stopErr := vm.shutdown(opts.StopTimeout)
		if err == nil && stopErr != nil {
			err = stopErr
		}
	}()

	vsockPath := filepath.Join(dir, "vsock.sock")
	if err := vm.configure(ctx, opts, vsockPath); err != nil {
		return res, err
	}

	conn, err := waitVsockHandshake(ctx, vsockPath, opts.VsockPort, opts.BootTimeout)
	if err != nil {
		return res, err
	}
	defer conn.Close()
	res.BootDuration = time.Since(start)

	if err := conn.pingPong(); err != nil {
		return res, err
	}

	execStart := time.Now()
	res.Stdout, res.Stderr, res.ExitCode, err = conn.exec(opts.Command, opts.Args...)
	res.ExecDuration = time.Since(execStart)
	if err != nil {
		return res, err
	}

	stopStart := time.Now()
	if err := conn.stop(); err != nil {
		return res, err
	}
	if err := vm.shutdown(opts.StopTimeout); err != nil {
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
	cmd := exec.Command(opts.Firecracker, "--api-sock", sockPath) //nolint:gosec // fixed binary, no shell
	cmd.Stdout = opts.Log
	cmd.Stderr = opts.Log
	// Own process group so a leaked child cannot escape shutdown.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
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
				return nil // nonzero exit after CtrlAltDel is expected on some kernels
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
type vsockConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *vsockConn) readLine(timeout time.Duration) (string, error) {
	if err := c.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return "", err
	}
	line, err := c.r.ReadString('\n')
	return strings.TrimRight(line, "\n"), err
}

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
	fields := append([]string{"EXEC", path}, args...)
	if _, err := io.WriteString(c, strings.Join(fields, "\t")+"\n"); err != nil {
		return "", "", 0, fmt.Errorf("send EXEC: %w", err)
	}
	var outBuf, errBuf bytes.Buffer
	for {
		line, err := c.readLine(10 * time.Second)
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
	c := &vsockConn{Conn: conn, r: bufio.NewReader(conn)}
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

func main() {
	var (
		fcPath    = flag.String("firecracker", "experiments/artifacts/dependencies-v1/bin/firecracker", "firecracker binary")
		kernel    = flag.String("kernel", "experiments/artifacts/t01/vmlinux", "uncompressed guest kernel")
		initramfs = flag.String("initramfs", "experiments/artifacts/t01/initramfs.cpio.gz", "gzip initramfs")
		cycles    = flag.Int("cycles", 1, "boot/stop cycles")
		keep      = flag.Bool("keep", false, "keep per-cycle logs")
	)
	flag.Parse()

	logDir := ""
	if *keep {
		dir, err := os.MkdirTemp("", "grillo-t01-logs-")
		if err != nil {
			fmt.Fprintln(os.Stderr, "run: create log dir:", err)
			os.Exit(1)
		}
		logDir = dir
		fmt.Println("keeping firecracker logs in", dir)
	}

	results := make([]Result, 0, *cycles)
	for i := 0; i < *cycles; i++ {
		var log io.Writer = io.Discard
		if logDir != "" {
			path := filepath.Join(logDir, fmt.Sprintf("cycle-%02d.log", i))
			f, err := os.Create(path)
			if err != nil {
				fmt.Fprintln(os.Stderr, "run: create log:", err)
				os.Exit(1)
			}
			defer f.Close()
			log = f
		} else if *cycles == 1 {
			log = os.Stderr
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		res, err := RunOnce(ctx, Options{
			Firecracker: *fcPath,
			Kernel:      *kernel,
			Initramfs:   *initramfs,
			Log:         log,
		})
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "cycle %d: FAIL: %v\n", i+1, err)
			os.Exit(1)
		}
		if !strings.Contains(res.Stdout, "grillo-testcmd stdout") || res.ExitCode != 0 {
			fmt.Fprintf(os.Stderr, "cycle %d: unexpected payload: exit=%d stdout=%q\n", i+1, res.ExitCode, res.Stdout)
			os.Exit(1)
		}
		results = append(results, res)
		fmt.Printf("cycle %d: boot=%s exec=%s stop=%s total=%s exit=%d\n",
			i+1, res.BootDuration.Round(time.Millisecond), res.ExecDuration.Round(time.Millisecond),
			res.StopDuration.Round(time.Millisecond), res.Total.Round(time.Millisecond), res.ExitCode)
	}
	summarize(results)
}

func summarize(results []Result) {
	if len(results) < 2 {
		return
	}
	boots := make([]time.Duration, len(results))
	totals := make([]time.Duration, len(results))
	for i, r := range results {
		boots[i] = r.BootDuration
		totals[i] = r.Total
	}
	sort.Slice(boots, func(i, j int) bool { return boots[i] < boots[j] })
	sort.Slice(totals, func(i, j int) bool { return totals[i] < totals[j] })
	fmt.Printf("summary: cycles=%d boot_median=%s boot_p95=%s total_median=%s total_p95=%s\n",
		len(results),
		median(boots).Round(time.Millisecond), percentile(boots, 0.95).Round(time.Millisecond),
		median(totals).Round(time.Millisecond), percentile(totals, 0.95).Round(time.Millisecond))
}

func median(sorted []time.Duration) time.Duration {
	return percentile(sorted, 0.5)
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}
