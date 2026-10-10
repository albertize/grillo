//go:build linux

// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/albertize/grillo/internal/guest"
	"github.com/albertize/grillo/internal/runtimeassets"
)

func TestDoctorPortableInstalledPrefix(t *testing.T) {
	prefix := filepath.Join(t.TempDir(), "stage")
	layout := runtimeassets.Layout{Prefix: prefix}
	var specs []guest.ArtifactSpec
	for _, component := range []string{"kernel", "initramfs", "daemon", "netns", "helm"} {
		path, err := layout.Path(component, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture-"+component), 0555); err != nil {
			t.Fatal(err)
		}
		if component == "kernel" || component == "initramfs" {
			specs = append(specs, guest.ArtifactSpec{Name: component, Version: "fixture", Path: path})
		}
	}
	manifestPath, _ := layout.Path("manifest", "")
	manifest, err := guest.BuildPortableManifest(filepath.Dir(manifestPath), specs)
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.Write(manifestPath); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(filepath.Dir(prefix), "moved prefix with spaces")
	if err := os.Rename(prefix, moved); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRILLO_ASSET_PREFIX", moved)
	for _, env := range []string{"GRILLO_GUEST_MANIFEST", "GRILLO_GUEST_KERNEL", "GRILLO_GUEST_INITRAMFS", "GRILLO_DAEMON_BINARY", "GRILLO_NETNS_BINARY", "GRILLO_HELM_BINARY"} {
		t.Setenv(env, "")
	}
	t.Chdir(t.TempDir())
	for _, check := range installedAssetChecks() {
		if check.Status != "ok" {
			t.Fatal(check)
		}
	}
	path, _ := (runtimeassets.Layout{Prefix: moved}).Path("manifest", "")
	if err := verifyGuestAssets(path, false); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	app := App{Stdout: &output, Stderr: io.Discard}
	if code := app.Run(context.Background(), []string{"version", "--json"}); code != 0 {
		t.Fatal(code)
	}
	var identity struct {
		Digest string `json:"asset_digest"`
		Status string `json:"asset_status"`
	}
	if err := json.Unmarshal(output.Bytes(), &identity); err != nil {
		t.Fatal(err)
	}
	loaded, err := guest.ReadBootManifest(path, false)
	if err != nil {
		t.Fatal(err)
	}
	expected, _, err := guest.HashFile(path)
	if err != nil || identity.Digest != expected || loaded.Digest() != expected {
		t.Fatal("manifest identity not reported", err)
	}
	if !bytes.Contains(output.Bytes(), []byte("artifact verification occurs before boot")) {
		t.Fatal("inventory hash overstated as artifact verification")
	}
	t.Setenv("GRILLO_GUEST_KERNEL", filepath.Join(moved, "other-kernel"))
	if err := verifyGuestAssets(path, false); err == nil {
		t.Fatal("explicit kernel override ignored")
	}
}
