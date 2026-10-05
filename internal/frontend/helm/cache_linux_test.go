//go:build linux

// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestInputOpenDoesNotBlockOnFIFOOrFollowSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(dir, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "regular"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("regular", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	start := time.Now()
	file, err := openInput(root, "pipe")
	if err == nil {
		file.Close()
		t.Fatal("FIFO accepted")
	}
	if time.Since(start) > time.Second {
		t.Fatal("FIFO refusal blocked")
	}
	if file, err := openInput(root, "link"); err == nil {
		file.Close()
		t.Fatal("input symlink followed")
	}
}
