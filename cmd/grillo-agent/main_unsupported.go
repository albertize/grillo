//go:build !linux

// SPDX-License-Identifier: Apache-2.0

// Command grillo-agent is the Grillo guest PID 1; the guest runs Linux, so this
// build exists only to keep `go build ./...` working on other platforms.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "grillo-agent: only Linux guests are supported")
	os.Exit(1)
}
