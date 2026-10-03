//go:build linux

// SPDX-License-Identifier: Apache-2.0

// Command grillo-agent is the Grillo guest PID 1. It mounts the pseudo
// filesystems, enables cgroup v2 controllers, reaps every child, listens on
// AF_VSOCK, and serves the versioned host/guest protocol by running OCI
// containers with runc. It is not a general-purpose init and not a kubelet.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"grillo.local/grillo/internal/guest"
	"grillo.local/grillo/internal/guestproto"

	"golang.org/x/sys/unix"
)

const agentVersion = "0.1.0-t07"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "grillo-agent:", err)
		os.Exit(1)
	}
}

func run() error {
	port := flag.Uint("vsock-port", 1024, "vsock port to listen on")
	workDir := flag.String("work-dir", "/run/grillo", "agent work directory")
	runcPath := flag.String("runc", "/runc", "runc binary")
	runcRoot := flag.String("runc-root", "/run/runc", "runc state root")
	keyFile := flag.String("key-file", "/run/grillo/key", "file containing the base64 per-boot key")
	sandboxID := flag.String("sandbox", "", "expected sandbox id (optional)")
	flag.Parse()

	if err := guest.SetupFilesystems(); err != nil {
		return err
	}
	if err := guest.SetupLoopback(); err != nil {
		fmt.Fprintln(os.Stderr, "grillo-agent: loopback:", err)
	}
	if err := guest.EnableCgroupControllers("cpu", "memory", "pids"); err != nil {
		fmt.Fprintln(os.Stderr, "grillo-agent: cgroups:", err)
	}
	key, err := loadKey(*keyFile)
	if err != nil {
		return err
	}

	reaper := guest.NewReaper()
	reaper.Run()
	rt := &guest.Runc{Binary: *runcPath, Root: *runcRoot, Reaper: reaper}
	agent := guest.NewAgent(rt, *workDir)
	if err := agent.Prepare(); err != nil {
		return err
	}

	ln, err := guestproto.ListenVsock(uint32(*port))
	if err != nil {
		return err
	}
	defer ln.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	fmt.Printf("grillo-agent: ready version=%s port=%d\n", agentVersion, *port)

	var wg sync.WaitGroup
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			fmt.Fprintln(os.Stderr, "grillo-agent: accept:", err)
			time.Sleep(50 * time.Millisecond)
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			serve(ctx, conn, key, *sandboxID, agent)
		}()
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	agent.Shutdown(shutdownCtx)
	cancelShutdown()
	wg.Wait()
	return unix.Reboot(unix.LINUX_REBOOT_CMD_RESTART)
}

func serve(ctx context.Context, conn net.Conn, key []byte, sandboxID string, agent *guest.Agent) {
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	cfg := guestproto.HandshakeConfig{
		Sandbox:      sandboxID,
		Key:          key,
		Agent:        agentVersion,
		Capabilities: []guestproto.Capability{guestproto.CapExec, guestproto.CapProbe},
	}
	srv, err := guestproto.NewServer(ctx, conn, cfg, agent)
	if err != nil {
		fmt.Fprintln(os.Stderr, "grillo-agent: handshake:", err)
		return
	}
	defer srv.Close()
	if err := srv.Serve(ctx); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "grillo-agent: serve:", err)
	}
}

func loadKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read per-boot key: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("decode per-boot key: %w", err)
	}
	if len(key) < 16 {
		return nil, errors.New("per-boot key is too short")
	}
	return key, nil
}
