//go:build linux

// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestDaemonRejectsRootRuntime(t *testing.T) {
	if err := validateRuntimeUID(0); err == nil {
		t.Fatal("root runtime accepted")
	}
	if err := validateRuntimeUID(1000); err != nil {
		t.Fatal(err)
	}
}
