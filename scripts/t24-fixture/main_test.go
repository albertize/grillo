// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProvisionRejectsOutputWithoutNetwork(t *testing.T) {
	if err := run(""); err == nil {
		t.Fatal("implicit network provision accepted")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "existing")
	if err := os.WriteFile(file, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(file); err == nil {
		t.Fatal("existing output replaced")
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "keep" {
		t.Fatal("existing bytes changed")
	}
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := run(filepath.Join(link, "new")); err == nil {
		t.Fatal("symlink parent followed")
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		t.Fatal("symlink target modified", err)
	}
}
