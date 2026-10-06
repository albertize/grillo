// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/albertize/grillo/internal/cli"
)

const version = "0.1.0"

func main() {
	app := &cli.App{Version: version}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := app.Run(ctx, os.Args[1:])
	stop()
	os.Exit(code)
}
