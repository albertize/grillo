//go:build linux

// SPDX-License-Identifier: Apache-2.0

// Package netns productionizes the rootless network topology proven by the F0
// experiment: one pasta user+network namespace per application, a bridge with
// the application gateway, one TAP per sandbox, and nftables forwarding. The
// supervisor runs inside the namespace and launches VMMs there, so the backend
// never needs setns.
package netns

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Request is one supervisor command (JSON line).
type Request struct {
	Op      string   `json:"op"` // "ping", "launch", "remove", "stop", "alive"
	Tap     string   `json:"tap,omitempty"`
	QEMU    string   `json:"qemu,omitempty"`
	Args    []string `json:"args,omitempty"`
	LogPath string   `json:"log,omitempty"`
	MAC     string   `json:"mac,omitempty"`
}

// Response is the supervisor reply.
type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	PID   int    `json:"pid,omitempty"`
	Alive bool   `json:"alive,omitempty"`
}

// Config configures the supervisor.
type Config struct {
	SocketPath string
	Bridge     string // defaults to "grillo0"
	Gateway    string // e.g. "10.77.0.1"
	Prefix     int    // e.g. 24
	Uplink     string // pasta namespace interface, defaults to "uplink"
}

// Supervisor runs inside the namespace.
type Supervisor struct {
	cfg Config

	listener net.Listener
	mu       sync.Mutex
	children map[string]*child
	cancel   context.CancelFunc
}

type child struct {
	cmd  *exec.Cmd
	done chan struct{}
}

// NewSupervisor returns a supervisor.
func NewSupervisor(cfg Config) *Supervisor {
	if cfg.Bridge == "" {
		cfg.Bridge = "grillo0"
	}
	if cfg.Prefix == 0 {
		cfg.Prefix = 24
	}
	if cfg.Uplink == "" {
		cfg.Uplink = "uplink"
	}
	return &Supervisor{cfg: cfg, children: map[string]*child{}}
}

// Run sets up the network and serves until ctx is canceled.
func (s *Supervisor) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	defer cancel()
	if err := s.setup(ctx); err != nil {
		return err
	}
	if err := os.Remove(s.cfg.SocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := net.Listen("unix", s.cfg.SocketPath)
	if err != nil {
		return err
	}
	s.listener = listener
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}
		go s.serveConn(conn)
	}
}

func (s *Supervisor) setup(ctx context.Context) error {
	commands := [][]string{
		{"link", "set", "lo", "up"},
		{"link", "add", s.cfg.Bridge, "type", "bridge"},
		{"addr", "add", fmt.Sprintf("%s/%d", s.cfg.Gateway, s.cfg.Prefix), "dev", s.cfg.Bridge},
		{"link", "set", s.cfg.Bridge, "up"},
	}
	for _, args := range commands {
		if err := run(ctx, "ip", args...); err != nil {
			return err
		}
	}
	if err := os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1\n"), 0o600); err != nil {
		return fmt.Errorf("netns: enable ip_forward: %w", err)
	}
	cmd := exec.CommandContext(ctx, "nft", "-f", "-")
	cmd.Stdin = strings.NewReader(s.rules())
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("netns: firewall: %w: %s", err, out)
	}
	return nil
}

// rules allows loopback, established flows, and DNS, forwards the application
// subnet to the uplink, and masquerades egress. Everything else is dropped, so
// the host's management services are not reachable.
func (s *Supervisor) rules() string {
	subnet := fmt.Sprintf("%s/%d", s.cfg.Gateway, s.cfg.Prefix)
	return fmt.Sprintf(`table inet grillo {
 chain input { type filter hook input priority 0; policy drop;
  iifname "lo" accept
  ct state established,related accept
  ip saddr %s udp dport 53 accept
  ip saddr %s tcp dport 53 accept
  counter drop
 }
 chain forward { type filter hook forward priority 0; policy drop;
  ct state established,related accept
  iifname "%s" oifname "%s" accept
  ip saddr %s oifname "%s" accept
  counter drop
 }
}
table ip grillonat {
 chain postrouting { type nat hook postrouting priority srcnat; policy accept;
  oifname "%s" ip saddr %s masquerade
 }
}
`, subnet, subnet, s.cfg.Bridge, s.cfg.Bridge, subnet, s.cfg.Uplink, s.cfg.Uplink, subnet)
}

func (s *Supervisor) serveConn(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	encoder := json.NewEncoder(conn)
	for scanner.Scan() {
		var request Request
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			_ = encoder.Encode(Response{Error: "invalid request"})
			continue
		}
		_ = encoder.Encode(s.handle(request))
	}
}

func (s *Supervisor) handle(request Request) Response {
	switch request.Op {
	case "ping":
		return Response{OK: true}
	case "shutdown":
		s.mu.Lock()
		busy := len(s.children) != 0
		s.mu.Unlock()
		if busy {
			return Response{Error: "supervisor still owns VMMs"}
		}
		// Let serveConn flush the response before Run exits.
		time.AfterFunc(50*time.Millisecond, s.cancel)
		return Response{OK: true}
	case "launch":
		pid, err := s.launch(request)
		if err != nil {
			return Response{Error: err.Error()}
		}
		return Response{OK: true, PID: pid}
	case "remove":
		if request.Tap != "" {
			_ = run(context.Background(), "ip", "link", "del", request.Tap)
		}
		return Response{OK: true}
	case "stop":
		return s.stopTap(request.Tap)
	case "alive":
		s.mu.Lock()
		cmd := s.children[request.Tap]
		s.mu.Unlock()
		return Response{OK: true, Alive: cmd != nil}
	default:
		return Response{Error: "unknown op"}
	}
}

func (s *Supervisor) launch(request Request) (int, error) {
	if request.Tap == "" || request.QEMU == "" {
		return 0, errors.New("launch requires a tap and qemu")
	}
	// Replace any previous child for this TAP (a retried launch after a partial
	// failure) before creating a fresh one.
	s.stopTap(request.Tap)

	if err := run(context.Background(), "ip", "tuntap", "add", "dev", request.Tap, "mode", "tap", "user", "0"); err != nil {
		return 0, err
	}
	if err := run(context.Background(), "ip", "link", "set", request.Tap, "master", s.cfg.Bridge); err != nil {
		return 0, err
	}
	if err := run(context.Background(), "ip", "link", "set", request.Tap, "up"); err != nil {
		return 0, err
	}

	args := append([]string{}, request.Args...)
	args = append(args,
		"-netdev", fmt.Sprintf("tap,id=n0,ifname=%s,script=no,downscript=no", request.Tap),
		"-device", "virtio-net-device,netdev=n0"+macArg(request.MAC),
	)
	var logFile *os.File
	if request.LogPath != "" {
		f, err := os.OpenFile(request.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return 0, err
		}
		logFile = f
	}
	cmd := exec.Command(request.QEMU, args...)
	if logFile != nil {
		cmd.Stdout, cmd.Stderr = logFile, logFile
	}
	// Do not set Pdeathsig here: Go may terminate the OS thread that handled
	// this request, which would deliver SIGKILL to a healthy VMM. The backend
	// tracks and stops the VMM explicitly.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		if logFile != nil {
			logFile.Close()
		}
		_ = run(context.Background(), "ip", "link", "del", request.Tap)
		return 0, err
	}
	done := make(chan struct{})
	owned := &child{cmd: cmd, done: done}
	s.mu.Lock()
	s.children[request.Tap] = owned
	s.mu.Unlock()
	go func() {
		defer close(done)
		_ = cmd.Wait()
		if logFile != nil {
			logFile.Close()
		}
		s.mu.Lock()
		current := s.children[request.Tap]
		if current == owned {
			delete(s.children, request.Tap)
		}
		s.mu.Unlock()
		if current == owned {
			_ = run(context.Background(), "ip", "link", "del", request.Tap)
		}
	}()
	// A VMM that exits within the first moments is a failure, not a launch. The
	// PID would be unusable, so report the captured log instead.
	select {
	case <-done:
		return 0, fmt.Errorf("netns: vmm for %s exited immediately: %s", request.Tap, logTail(request.LogPath))
	case <-time.After(250 * time.Millisecond):
	}
	return cmd.Process.Pid, nil
}

// stopTap kills the VMM for a TAP and removes the device.
func (s *Supervisor) stopTap(tap string) Response {
	if tap == "" {
		return Response{OK: true}
	}
	s.mu.Lock()
	cmd := s.children[tap]
	delete(s.children, tap)
	s.mu.Unlock()
	if cmd != nil && cmd.cmd.Process != nil {
		_ = syscall.Kill(-cmd.cmd.Process.Pid, syscall.SIGKILL)
		<-cmd.done // exactly one goroutine owns Wait
	}
	_ = run(context.Background(), "ip", "link", "del", tap)
	return Response{OK: true}
}

func logTail(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(data) > 2048 {
		data = data[len(data)-2048:]
	}
	return strings.TrimSpace(string(data))
}

func macArg(mac string) string {
	if mac == "" {
		return ""
	}
	return ",mac=" + mac
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("netns: %s %s: %w: %s", name, strings.Join(args, " "), err, out)
	}
	return nil
}
