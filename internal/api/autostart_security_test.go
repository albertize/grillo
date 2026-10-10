//go:build linux

// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureDaemonFirstRunCreatesPrivateRuntime(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "runtime/grillo")
	// No executable is started. Failure must occur after safe first-run setup,
	// not because the runtime/log directory was missing.
	err := EnsureDaemon(context.Background(), filepath.Join(directory, "grillod.sock"), "/missing-grillod", time.Second)
	if err == nil || !strings.Contains(err.Error(), "missing-grillod") {
		t.Fatal(err)
	}
	info, err := os.Stat(directory)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal(info, err)
	}
	info, err = os.Stat(filepath.Join(directory, "grillod.sock.log"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
}

func TestEnsureDaemonRejectsLogSymlinksAndHardlinks(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			directory := t.TempDir()
			target := filepath.Join(t.TempDir(), "user-file")
			if err := os.WriteFile(target, []byte("preserve"), 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "grillod.sock.log")
			var err error
			if kind == "symlink" {
				err = os.Symlink(target, path)
			} else {
				err = os.Link(target, path)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := EnsureDaemon(context.Background(), filepath.Join(directory, "grillod.sock"), "/missing-grillod", time.Second); err == nil {
				t.Fatal("unsafe log accepted")
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "preserve" {
				t.Fatal("user file changed")
			}
		})
	}
}
