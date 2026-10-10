//go:build linux

// SPDX-License-Identifier: Apache-2.0

package qemu

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/albertize/grillo/internal/guest"
)

func TestPortableBackendMovedPrefixAndSymlinkMutation(t *testing.T) {
	cfg, spec, path := artifactFixture(t)
	directory := filepath.Dir(path)
	manifest, err := guest.BuildPortableManifest(directory, []guest.ArtifactSpec{{Name: "kernel", Version: "fixture", Path: spec.Kernel}, {Name: "initramfs", Version: "fixture", Path: spec.Initramfs}})
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.Write(path); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(filepath.Dir(directory), "moved guest prefix")
	if err := os.Rename(directory, moved); err != nil {
		t.Fatal(err)
	}
	cfg.Kernel = filepath.Join(moved, "kernel")
	spec.Kernel = cfg.Kernel
	cfg.Initramfs = filepath.Join(moved, "initramfs")
	spec.Initramfs = cfg.Initramfs
	cfg.ArtifactManifest = filepath.Join(moved, "manifest.json")
	cfg.RequirePortableManifest = true
	t.Chdir(t.TempDir())
	backend, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := backend.Create(context.Background(), spec, "portable")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Delete(context.Background(), handle)
	if err := backend.Start(context.Background(), handle); err != nil {
		t.Fatal(err)
	}
	if err := backend.Stop(context.Background(), handle, 0); err != nil {
		t.Fatal(err)
	}
	// The already-open backend retains expected bytes, but must reject a changed
	// source path before relaunch, including an identical-content symlink.
	target := filepath.Join(moved, "real-kernel")
	if err := os.Rename(spec.Kernel, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real-kernel", spec.Kernel); err != nil {
		t.Fatal(err)
	}
	if err := backend.Start(context.Background(), handle); err == nil {
		t.Fatal("symlink substitution launched")
	}
}

func TestPortableBackendRequiresMetadataAndRejectsABIFallback(t *testing.T) {
	cfg, spec, path := artifactFixture(t)
	cfg.RequirePortableManifest = true
	if _, err := Open(cfg); err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatal("installed legacy accepted", err)
	}
	cfg.RequirePortableManifest = false
	if _, err := Open(cfg); err != nil {
		t.Fatal("explicit legacy development path failed", err)
	}
	manifest, err := guest.BuildPortableManifest(filepath.Dir(path), []guest.ArtifactSpec{{Name: "kernel", Version: "fixture", Path: spec.Kernel}, {Name: "initramfs", Version: "fixture", Path: spec.Initramfs}})
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.Write(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), `"guest_abi": "1.1"`, `"guest_abi": "99.1"`, 1))
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	// Even explicitly selected development manifests cannot bypass a declared ABI.
	if _, err := Open(cfg); err == nil || !strings.Contains(err.Error(), "incompatible guest ABI") {
		t.Fatal("ABI mismatch accepted", err)
	}
	cfg.ArtifactManifest = ""
	cfg.RequirePortableManifest = true
	cfg.BootKeyOverlay = false
	if _, err := Open(cfg); err == nil {
		t.Fatal("required manifest missing")
	}
}
