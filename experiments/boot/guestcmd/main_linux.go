//go:build linux

// SPDX-License-Identifier: Apache-2.0

// Command guestcmd is a deterministic T01 test payload that proves a guest can
// execute a real binary and stream both output streams. It is not shipped in the
// production guest image.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	code := 0
	for _, arg := range os.Args[1:] {
		if value, ok := strings.CutPrefix(arg, "--exit="); ok {
			if n, err := strconv.Atoi(value); err == nil {
				code = n
			}
		}
	}
	fmt.Println("grillo-testcmd stdout")
	fmt.Fprintln(os.Stderr, "grillo-testcmd stderr")
	fmt.Printf("grillo-testcmd args=%v\n", os.Args[1:])
	os.Exit(code)
}
