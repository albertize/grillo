//go:build linux

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/oci"
)

type fakePuller struct {
	mu        sync.Mutex
	digest    string
	pulls     int
	unpacks   int
	unpacked  []string
	unpackErr error
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
	if err := os.WriteFile(filepath.Join(root, "marker"), []byte("x"), 0600); err != nil {
		return err
	}
	return f.unpackErr
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

func TestOCIResolverQuotaFailureCleansStageAndAllowsRetry(t *testing.T) {
	puller := &fakePuller{digest: "sha256:quota", unpackErr: oci.ErrUnpackLimit}
	cache := filepath.Join(t.TempDir(), "cache")
	resolver := &OCIResolver{Puller: puller, CacheDir: cache}
	image := model.ImageRef{Reference: "busybox:1.37"}
	if _, err := resolver.Resolve(context.Background(), image); !errors.Is(err, oci.ErrUnpackLimit) {
		t.Fatal("quota failure lost", err)
	}
	entries, err := os.ReadDir(cache)
	if err != nil || len(entries) != 1 || entries[0].Name() != ".grillo-quota.lock" {
		t.Fatal("failed rootfs published or staging leaked", err)
	}
	puller.unpackErr = nil
	resolved, err := resolver.Resolve(context.Background(), image)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(resolved.HostPath, ".grillo-complete")); err != nil {
		t.Fatal(err)
	}
	if puller.unpacks != 2 {
		t.Fatal("failed extraction was cached")
	}
}

func TestOCIResolverAggregateQuotaRejectsWithoutEviction(t *testing.T) {
	puller := &fakePuller{digest: "sha256:one"}
	cache := filepath.Join(t.TempDir(), "cache")
	resolver := &OCIResolver{Puller: puller, CacheDir: cache, CacheQuota: oci.CacheQuota{MaxBytes: 12, MaxEntries: 20}}
	image := model.ImageRef{Reference: "busybox:1.37"}
	first, err := resolver.Resolve(context.Background(), image)
	if err != nil {
		t.Fatal(err)
	}
	// A separately created resolver must see existing bytes, not reset usage.
	puller.digest = "sha256:two"
	second := &OCIResolver{Puller: puller, CacheDir: cache, CacheQuota: resolver.CacheQuota}
	if _, err := second.Resolve(context.Background(), image); !errors.Is(err, oci.ErrCacheQuota) {
		t.Fatal("aggregate rootfs quota bypassed", err)
	}
	if data, err := os.ReadFile(filepath.Join(first.HostPath, "marker")); err != nil || string(data) != "x" {
		t.Fatal("existing cache evicted", err)
	}
	entries, err := os.ReadDir(cache)
	if err != nil || len(entries) != 2 {
		t.Fatal("rejected stage leaked", err)
	}
}

func TestOCIResolverIndependentInstancesDeduplicatePublication(t *testing.T) {
	puller := &fakePuller{digest: "sha256:shared"}
	cache := filepath.Join(t.TempDir(), "cache")
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resolver := &OCIResolver{Puller: puller, CacheDir: cache}
			if _, err := resolver.Resolve(context.Background(), model.ImageRef{Reference: "busybox:1.37"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	puller.mu.Lock()
	defer puller.mu.Unlock()
	if puller.unpacks != 1 {
		t.Fatal("independent cache writers duplicated extraction", puller.unpacks)
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
