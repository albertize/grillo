//go:build linux

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/oci"
)

type fakePuller struct {
	mu       sync.Mutex
	digest   string
	pulls    int
	unpacks  int
	unpacked []string
}

func (f *fakePuller) Pull(_ context.Context, _ oci.Reference) (oci.PulledImage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pulls++
	return oci.PulledImage{ManifestDigest: f.digest}, nil
}

func (f *fakePuller) Unpack(_ oci.PulledImage, root string, _ oci.UnpackOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unpacks++
	f.unpacked = append(f.unpacked, root)
	return os.WriteFile(filepath.Join(root, "marker"), []byte("x"), 0o600)
}

func TestOCIResolverCachesByManifestDigest(t *testing.T) {
	puller := &fakePuller{digest: "sha256:abc"}
	resolver := &OCIResolver{Puller: puller, CacheDir: filepath.Join(t.TempDir(), "cache")}
	image := model.ImageRef{Reference: "busybox:1.37"}
	first, err := resolver.Resolve(context.Background(), image)
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolver.Resolve(context.Background(), image)
	if err != nil {
		t.Fatal(err)
	}
	if first.HostPath != second.HostPath || first.Version != "sha256:abc" {
		t.Fatalf("resolved %+v then %+v", first, second)
	}
	if _, err := os.Stat(filepath.Join(first.HostPath, "marker")); err != nil {
		t.Fatalf("unpacked content missing: %v", err)
	}
	puller.mu.Lock()
	defer puller.mu.Unlock()
	if puller.unpacks != 1 {
		t.Fatalf("unpacked %d times, want 1 (second resolve must reuse the cache)", puller.unpacks)
	}
}

func TestOCIResolverSeparatesDigests(t *testing.T) {
	puller := &fakePuller{digest: "sha256:one"}
	resolver := &OCIResolver{Puller: puller, CacheDir: filepath.Join(t.TempDir(), "cache")}
	first, err := resolver.Resolve(context.Background(), model.ImageRef{Reference: "busybox:1.37"})
	if err != nil {
		t.Fatal(err)
	}
	puller.mu.Lock()
	puller.digest = "sha256:two"
	puller.mu.Unlock()
	second, err := resolver.Resolve(context.Background(), model.ImageRef{Reference: "busybox:1.37"})
	if err != nil {
		t.Fatal(err)
	}
	if first.HostPath == second.HostPath {
		t.Fatalf("different digests share a rootfs: %s", first.HostPath)
	}
}
