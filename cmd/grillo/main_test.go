// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestRuntimeCommandsRequireUnprivilegedUser(t *testing.T) {
	for _, name := range []string{"up", "down", "status", "ps", "restart", "ui", "inspect", "logs", "events", "metrics", "exec", "shell", "build", "image"} {
		if !requiresUnprivileged([]string{name}) {
			t.Fatal("runtime root guard missing", name)
		}
	}
	for _, name := range []string{"version", "--version", "doctor", "plan", "help"} {
		if requiresUnprivileged([]string{name}) {
			t.Fatal("read-only command blocked", name)
		}
	}
	if requiresUnprivileged(nil) {
		t.Fatal("usage blocked")
	}
}
