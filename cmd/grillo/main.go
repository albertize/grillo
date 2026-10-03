// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"

	"grillo.local/grillo/internal/command"
)

func main() {
	os.Exit(command.Run("grillo", os.Args[1:], os.Stdout, os.Stderr))
}
