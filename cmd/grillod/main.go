//go:build linux

// SPDX-License-Identifier: Apache-2.0

// Command grillod is the foreground per-user service. It holds a single-instance
// lock, serves the local Unix-socket API, and reconciles desired state. It is
// started on demand by the CLI and outlives the CLI.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"grillo.local/grillo/internal/api"
	"grillo.local/grillo/internal/observe"
	"grillo.local/grillo/internal/reconcile"
	"grillo.local/grillo/internal/state"
)

const version = "0.1.0"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "grillod:", err)
		os.Exit(1)
	}
}

func run() error {
	flag.Parse()

	layout, err := state.NewLayout(state.DefaultConfig())
	if err != nil {
		return err
	}
	if err := layout.Prepare(); err != nil {
		return err
	}
	// Single instance: hold the state writer lock for the daemon's lifetime. A
	// second daemon fails here before touching the socket.
	store, err := state.Open(layout, state.Ops{})
	if err != nil {
		return fmt.Errorf("another grillod is already running: %w", err)
	}
	defer store.Close()

	events, err := observe.OpenEvents(filepath.Join(layout.State, "events"), 0, 0)
	if err != nil {
		return err
	}
	defer events.Close()
	logs, err := observe.OpenLogSpool(filepath.Join(layout.State, "logs"), 0, 0)
	if err != nil {
		return err
	}
	defer logs.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	core := &reconcile.Reconciler{
		Store:    reconcile.NewMemoryStore(),
		Executor: &reconcile.NativeExecutor{},
	}
	server := api.NewServer(api.Options{
		Core:     core,
		Events:   events,
		Logs:     logs,
		Version:  version,
		Shutdown: cancel,
	})
	socket := filepath.Join(layout.Runtime, "grillod.sock")
	fmt.Fprintf(os.Stderr, "grillod: serving %s (version %s)\n", socket, version)
	return server.Serve(ctx, socket)
}
