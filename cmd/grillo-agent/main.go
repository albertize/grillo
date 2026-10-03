// SPDX-License-Identifier: Apache-2.0

// This binary is a build scaffold, not a usable guest init process.
package main

import (
	"os"

	"grillo.local/grillo/internal/command"
)

func main() {
	os.Exit(command.Run("grillo-agent", os.Args[1:], os.Stdout, os.Stderr))
}
