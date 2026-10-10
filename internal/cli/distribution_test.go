//go:build linux

// SPDX-License-Identifier: Apache-2.0
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/albertize/grillo/internal/buildinfo"
	"github.com/albertize/grillo/internal/guest"
)

func TestDistributionCommandFlags(t *testing.T) {
	for _, args := range [][]string{{"version", "--json"}, {"doctor", "--json"}, {"doctor", "--verbose"}} {
		var out bytes.Buffer
		app := &App{Stdout: &out, Doctor: func() []Check { return []Check{{Name: "kvm", Status: "fail", Detail: "denied"}} }}
		code := app.Run(context.Background(), args)
		if args[0] == "version" {
			var info buildinfo.Info
			if code != 0 || json.Unmarshal(out.Bytes(), &info) != nil || info.GuestABI == "" || info.Version != "dev" {
				t.Fatal(code, out.String())
			}
		} else {
			if code != 1 {
				t.Fatal(code, out.String())
			}
			if args[1] == "--json" {
				var checks []Check
				if json.Unmarshal(out.Bytes(), &checks) != nil || len(checks) != 1 || checks[0].Fix == "" {
					t.Fatal(out.String())
				}
			} else if !strings.Contains(out.String(), "Fix:") {
				t.Fatal(out.String())
			}
		}
	}
	for _, args := range [][]string{{"version", "--unknown"}, {"doctor", "--unknown"}, {"doctor", "extra"}} {
		app := &App{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
		if code := app.Run(context.Background(), args); code != 2 {
			t.Fatal(args, code)
		}
	}
}

func TestDoctorAssetsReadOnlyAndTampering(t *testing.T) {
	dir := t.TempDir()
	kernel, image := filepath.Join(dir, "bzImage"), filepath.Join(dir, "initramfs")
	for _, path := range []string{kernel, image} {
		if err := os.WriteFile(path, []byte("fixture"), 0444); err != nil {
			t.Fatal(err)
		}
	}
	m, err := guest.BuildManifest([]guest.ArtifactSpec{{Name: "kernel", Version: "test", Path: kernel}, {Name: "initramfs", Version: "test", Path: image}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "manifest.json")
	if err := m.Write(path); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRILLO_GUEST_KERNEL", kernel)
	t.Setenv("GRILLO_GUEST_INITRAMFS", image)
	if err := verifyGuestAssets(path, true); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(image, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(image, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyGuestAssets(path, true); err == nil {
		t.Fatal("tampered guest accepted")
	}
	t.Setenv("GRILLO_GUEST_KERNEL", filepath.Join(dir, "other"))
	if err := verifyGuestAssets(path, true); err == nil {
		t.Fatal("override ignored")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 3 {
		t.Fatal("doctor created files", entries, err)
	}
}
