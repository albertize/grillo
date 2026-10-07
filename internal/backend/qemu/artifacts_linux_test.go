//go:build linux

// SPDX-License-Identifier: Apache-2.0

package qemu

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/albertize/grillo/internal/guest"
	"github.com/albertize/grillo/internal/sandbox"
)

func artifactFixture(t *testing.T) (Config, sandbox.Spec, string) {
	t.Helper()
	dir := t.TempDir()
	kernel := filepath.Join(dir, "kernel")
	initramfs := filepath.Join(dir, "initramfs")
	if err := os.WriteFile(kernel, []byte("fixture-kernel"), 0o600); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	writer := gzip.NewWriter(&archive)
	_, _ = writer.Write([]byte("fixture-archive"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(initramfs, archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := guest.BuildManifest([]guest.ArtifactSpec{{Name: "kernel", Version: "fixture", Path: kernel}, {Name: "initramfs", Version: "fixture", Path: initramfs}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "manifest.json")
	if err := manifest.Write(path); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(t, okDial)
	cfg.Kernel = kernel
	cfg.Initramfs = initramfs
	cfg.ArtifactManifest = path
	cfg.BootKeyOverlay = true
	spec := testSpec()
	spec.Kernel = kernel
	spec.Initramfs = initramfs
	return cfg, spec, path
}
func TestVerifiedBootCopiesPinAndRejectMutations(t *testing.T) {
	cfg, spec, path := artifactFixture(t)
	launched := false
	cfg.Launch = func(context.Context, sandbox.Spec, []string, string) (sandbox.VMM, error) {
		launched = true
		return nil, errors.New("unexpected launch")
	}
	backend, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := backend.prepareBoot(context.Background(), spec, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(snapshot.Kernel)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(snapshot.Initramfs); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("boot snapshot not private")
	}
	if err := os.WriteFile(spec.Kernel, []byte("mutated-kernel"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A rewritten manifest is not re-trusted by the open backend.
	m, err := guest.BuildManifest([]guest.ArtifactSpec{{Name: "kernel", Version: "fixture", Path: spec.Kernel}, {Name: "initramfs", Version: "fixture", Path: spec.Initramfs}})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Write(path); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.prepareBoot(context.Background(), spec, t.TempDir()); err == nil {
		t.Fatal("mutation accepted")
	}
	got, _ := os.ReadFile(snapshot.Kernel)
	if !bytes.Equal(got, original) {
		t.Fatal("snapshot changed with source")
	}
	if _, err := backend.Create(context.Background(), spec, "test"); err != nil {
		t.Fatal(err)
	}
	if err := backend.Start(context.Background(), sandbox.Handle{ID: spec.ID}); err == nil || !strings.Contains(err.Error(), "kernel artifact digest mismatch") || launched {
		t.Fatalf("mutation did not block launch: %v launched=%t", err, launched)
	}
}
func TestBootCredentialFreshOnRestartStableOnIdempotency(t *testing.T) {
	cfg, spec, _ := artifactFixture(t)
	backend, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := backend.Create(context.Background(), spec, "test")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer backend.Delete(ctx, handle)
	if err := backend.Start(ctx, handle); err != nil {
		t.Fatal(err)
	}
	_, _, first, err := backend.BootConnection(handle)
	if err != nil || bytes.Equal(first, spec.GuestKey) {
		t.Fatal("fixture key reused")
	}
	if err := backend.Start(ctx, handle); err != nil {
		t.Fatal(err)
	}
	_, _, same, _ := backend.BootConnection(handle)
	if !bytes.Equal(first, same) {
		t.Fatal("idempotent Start changed live credentials")
	}
	if err := backend.Stop(ctx, handle, 0); err != nil {
		t.Fatal(err)
	}
	if err := backend.Start(ctx, handle); err != nil {
		t.Fatal(err)
	}
	_, _, second, _ := backend.BootConnection(handle)
	if bytes.Equal(first, second) {
		t.Fatal("reboot reused key")
	}
}
func TestBootOverlayArchive(t *testing.T) {
	cfg, spec, _ := artifactFixture(t)
	backend, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := backend.prepareBoot(context.Background(), spec, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(snapshot.Initramfs)
	if err != nil {
		t.Fatal(err)
	}
	reader := bytes.NewReader(data)
	gz, err := gzip.NewReader(reader)
	if err != nil {
		t.Fatal(err)
	}
	gz.Multistream(false)
	first, err := io.ReadAll(gz)
	if err != nil || string(first) != "fixture-archive" {
		t.Fatal("base archive damaged")
	}
	_ = gz.Close()
	gz, err = gzip.NewReader(reader)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	overlay, err := io.ReadAll(gz)
	if err != nil || !bytes.HasPrefix(overlay, []byte("070701")) || !bytes.Contains(overlay, []byte("etc/grillo/key\x00")) || !bytes.Contains(overlay, []byte("TRAILER!!!\x00")) {
		t.Fatal("invalid key archive")
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if err := copyArtifact(canceled, backend.artifacts["kernel"], filepath.Join(t.TempDir(), "kernel")); err == nil {
		t.Fatal("cancelled copy succeeded")
	}
}
