//go:build !linux

// SPDX-License-Identifier: Apache-2.0

// Command grillod is the per-user service; it requires Linux.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "grillod: only Linux is supported")
	os.Exit(1)
}
