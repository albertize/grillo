// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"path/filepath"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func testLayout(t *testing.T) Layout {
	t.Helper()
	base := t.TempDir()
	cfg := Config{
		Getenv: env(map[string]string{
			"XDG_RUNTIME_DIR": filepath.Join(base, "run"),
			"XDG_STATE_HOME":  filepath.Join(base, "state"),
			"XDG_DATA_HOME":   filepath.Join(base, "data"),
			"XDG_CACHE_HOME":  filepath.Join(base, "cache"),
		}),
		UID:     os.Getuid(),
		Home:    base,
		TempDir: base,
	}
	layout, err := NewLayout(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return layout
}

func TestNewLayoutXDG(t *testing.T) {
	cfg := Config{
		Getenv: env(map[string]string{
			"XDG_RUNTIME_DIR": "/run/user/1000",
			"XDG_STATE_HOME":  "/home/u/.local/state",
			"XDG_DATA_HOME":   "/home/u/.local/share",
			"XDG_CACHE_HOME":  "/home/u/.cache",
		}),
		UID: 1000, Home: "/home/u", TempDir: "/tmp",
	}
	layout, err := NewLayout(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := Layout{
		Runtime: "/run/user/1000/grillo",
		State:   "/home/u/.local/state/grillo",
		Data:    "/home/u/.local/share/grillo",
		Cache:   "/home/u/.cache/grillo",
	}
	if layout != want {
		t.Errorf("layout = %+v, want %+v", layout, want)
	}
}

func TestNewLayoutFallbacks(t *testing.T) {
	home := t.TempDir()
	cfg := Config{Getenv: env(nil), UID: 999999, Home: home, TempDir: "/tmp"}
	layout, err := NewLayout(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if layout.Runtime != filepath.Join("/tmp", "grillo-999999", "grillo") {
		t.Errorf("runtime fallback = %q", layout.Runtime)
	}
	if layout.State != filepath.Join(home, ".local", "state", "grillo") {
		t.Errorf("state fallback = %q", layout.State)
	}
}

func TestPrepareCreatesPrivateDirs(t *testing.T) {
	layout := testLayout(t)
	if err := layout.Prepare(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{layout.Runtime, layout.State, layout.Data, layout.Cache} {
		fi, err := os.Lstat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o700 {
			t.Errorf("%s mode = %v, want 0700", dir, fi.Mode().Perm())
		}
	}
}

func TestPrepareRejectsSymlink(t *testing.T) {
	layout := testLayout(t)
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(layout.Data), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, layout.Data); err != nil {
		t.Fatal(err)
	}
	if err := layout.Prepare(); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestEnsurePrivateDirRejectsWrongOwner(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivateDir(dir, os.Getuid()+1); err == nil {
		t.Fatal("expected owner mismatch error")
	}
}
