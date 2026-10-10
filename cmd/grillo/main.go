// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/albertize/grillo/internal/buildinfo"
	"github.com/albertize/grillo/internal/cli"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == cli.NamespaceProbeArgument {
		return
	}
	if os.Geteuid() == 0 && requiresUnprivileged(os.Args[1:]) {
		fmt.Fprintln(os.Stderr, "grillo: runtime commands require an unprivileged user; never use sudo as a fallback")
		os.Exit(3)
	}
	app := &cli.App{Version: buildinfo.Version, DaemonPath: os.Getenv("GRILLO_DAEMON_BINARY")}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := app.Run(ctx, os.Args[1:])
	stop()
	os.Exit(code)
}

func requiresUnprivileged(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "up", "down", "status", "ps", "restart", "ui", "inspect", "logs", "events", "metrics", "exec", "shell", "build", "image":
		return true
	}
	return false
}
