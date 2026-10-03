// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCheckKVMMissing(t *testing.T) {
	err := checkKVM(filepath.Join(t.TempDir(), "missing"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got %v; want missing device error", err)
	}
}

func TestCheckKVMRejectsRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-kvm")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkKVM(path); !errors.Is(err, syscall.ENOTTY) {
		t.Fatalf("got %v; want unsupported ioctl", err)
	}
}
