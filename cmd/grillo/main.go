// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"

	"github.com/albertize/grillo/internal/cli"
)

const version = "0.1.0"

func main() {
	app := &cli.App{Version: version}
	os.Exit(app.Run(context.Background(), os.Args[1:]))
}
