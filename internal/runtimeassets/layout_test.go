// SPDX-License-Identifier: Apache-2.0
package runtimeassets

import (
	"github.com/albertize/grillo/internal/guestproto"
	"os"
	"path/filepath"
	"testing"
)

func TestGuestABIMatchesProtocol(t *testing.T) {
	if GuestABI != guestproto.CurrentVersion.String() {
		t.Fatal("installed ABI directory must match current guest protocol")
	}
}

func TestMovedPrefixAndLauncherSymlink(t *testing.T) {
	root := t.TempDir()
	prefix := filepath.Join(root, "prefix with spaces")
	for _, relative := range []string{"bin/grillo", "libexec/grillo/grillod"} {
		exe := filepath.Join(prefix, relative)
		if err := os.MkdirAll(filepath.Dir(exe), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(exe, []byte("fixture"), 0555); err != nil {
			t.Fatal(err)
		}
	}
	moved := filepath.Join(root, "moved prefix")
	if err := os.Rename(prefix, moved); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(root, "launcher")
	if err := os.Symlink(filepath.Join(moved, "bin/grillo"), launcher); err != nil {
		t.Fatal(err)
	}
	for _, exe := range []string{launcher, filepath.Join(moved, "libexec/grillo/grillod")} {
		layout, err := FromExecutable(exe, "")
		if err != nil || layout.Prefix != moved {
			t.Fatal(layout, err)
		}
		path, err := layout.Path("daemon", "")
		if err != nil || path != filepath.Join(moved, "libexec/grillo/grillod") {
			t.Fatal(path, err)
		}
		if err := Executable(path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOverridesAndFailClosed(t *testing.T) {
	if _, err := FromExecutable("/missing", "relative"); err == nil {
		t.Fatal("relative prefix accepted")
	}
	layout, err := FromExecutable("/missing", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path, err := layout.Path("kernel", "explicit-kernel")
	wanted, _ := filepath.Abs("explicit-kernel")
	if err != nil || path != wanted {
		t.Fatal(path, err)
	}
	if _, err := layout.Path("unknown", "override"); err == nil {
		t.Fatal("unknown component")
	}
	if err := Executable(path); err == nil {
		t.Fatal("missing override silently accepted")
	}
	dir := t.TempDir()
	if err := Executable(dir); err == nil {
		t.Fatal("directory executable accepted")
	}
	file := filepath.Join(dir, "helper")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Executable(file); err == nil {
		t.Fatal("nonexecutable accepted")
	}
	if _, err := FromExecutable(file, ""); err == nil {
		t.Fatal("unrecognized layout accepted")
	}
}

func TestNoCWDOrPATHHelperFallback(t *testing.T) {
	prefix := t.TempDir()
	malicious := filepath.Join(t.TempDir(), "grillod")
	if err := os.WriteFile(malicious, []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(malicious))
	path, err := (Layout{Prefix: prefix}).Path("daemon", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := Executable(path); err == nil {
		t.Fatal("PATH helper used")
	}
}
