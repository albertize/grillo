//go:build linux

// SPDX-License-Identifier: Apache-2.0

// Command grillo-netns is the application network supervisor. It runs inside a
// pasta user+network namespace, sets up the application bridge and firewall, and
// launches VMMs attached to per-sandbox TAP devices. It is started by the
// runtime, never by a user.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/albertize/grillo/internal/netns"
)

func main() {
	var cfg netns.Config
	flag.StringVar(&cfg.SocketPath, "socket", "", "supervisor Unix socket")
	flag.StringVar(&cfg.Bridge, "bridge", "grillo0", "bridge name")
	flag.StringVar(&cfg.Gateway, "gateway", "", "bridge gateway address")
	flag.IntVar(&cfg.Prefix, "prefix", 24, "bridge prefix length")
	flag.StringVar(&cfg.Uplink, "uplink", "uplink", "pasta namespace interface")
	flag.Parse()
	if cfg.SocketPath == "" || cfg.Gateway == "" {
		fmt.Fprintln(os.Stderr, "grillo-netns: --socket and --gateway are required")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	if err := netns.NewSupervisor(cfg).Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "grillo-netns:", err)
		os.Exit(1)
	}
}
