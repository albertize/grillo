//go:build linux

// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCacheGuardScanEntryDepthAndSymlinkBounds(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 300; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprint(i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := AcquireCacheGuard(context.Background(), root, CacheQuota{MaxEntries: 128}); !errors.Is(err, ErrCacheQuota) {
		t.Fatal("entry bound ignored", err)
	}
	root = t.TempDir()
	path := root
	for i := 0; i < 129; i++ {
		path = filepath.Join(path, "d")
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := AcquireCacheGuard(context.Background(), root, CacheQuota{}); err == nil {
		t.Fatal("directory depth unbounded")
	}
	root = t.TempDir()
	outside := filepath.Join(t.TempDir(), "large")
	if err := os.WriteFile(outside, bytes.Repeat([]byte("x"), 1000), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	guard, err := AcquireCacheGuard(context.Background(), root, CacheQuota{MaxBytes: 200})
	if err != nil {
		t.Fatal("symlink target followed", err)
	}
	defer guard.Close()
	if guard.Entries != 1 || guard.Bytes != int64(len(outside)) {
		t.Fatal("incorrect no-follow symlink accounting", guard.Bytes, guard.Entries)
	}
}

func TestCacheGuardCrossProcess(t *testing.T) {
	if root := os.Getenv("GRILLO_TEST_CACHE_LOCK_ROOT"); root != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		if guard, err := AcquireCacheGuard(ctx, root, CacheQuota{}); !errors.Is(err, context.DeadlineExceeded) {
			if guard != nil {
				guard.Close()
			}
			t.Fatal("cross-process lock bypassed", err)
		}
		return
	}
	root := t.TempDir()
	guard, err := AcquireCacheGuard(context.Background(), root, CacheQuota{})
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-test.run=^TestCacheGuardCrossProcess$")
	command.Env = append(os.Environ(), "GRILLO_TEST_CACHE_LOCK_ROOT="+root)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("child lock test: %v %s", err, output)
	}
}

func TestCASQuotaPersistsAcrossInstancesAndPrune(t *testing.T) {
	root := t.TempDir()
	quota := CacheQuota{MaxBytes: 6, MaxEntries: 20}
	first, err := OpenCASWithQuota(root, quota)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("1234")
	digest := "sha256:" + sha256Hex(data)
	if err := first.PutBytes(digest, data); err != nil {
		t.Fatal(err)
	}
	second, err := OpenCASWithQuota(root, quota)
	if err != nil {
		t.Fatal(err)
	}
	more := []byte("abc")
	other := "sha256:" + sha256Hex(more)
	if err := second.PutBytes(other, more); !errors.Is(err, ErrCacheQuota) {
		t.Fatal("reopen reset quota", err)
	}
	if second.Has(other) {
		t.Fatal("over-quota blob published")
	}
	if entries, _ := os.ReadDir(first.tmpDir); len(entries) != 0 {
		t.Fatal("failed temp retained")
	}
	if count, err := second.Collect(func(string) bool { return false }); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := second.PutBytes(other, more); err != nil {
		t.Fatal("prune did not release quota", err)
	}
}

func TestCacheGuardCancellationAndUnsafeLock(t *testing.T) {
	root := t.TempDir()
	guard, err := AcquireCacheGuard(context.Background(), root, CacheQuota{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := AcquireCacheGuard(ctx, root, CacheQuota{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("lock wait ignored cancellation", err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ".grillo-quota.lock")); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "preserve")
	if err := os.WriteFile(outside, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".grillo-quota.lock")); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireCacheGuard(context.Background(), root, CacheQuota{}); err == nil {
		t.Fatal("lock symlink accepted")
	}
	if data, _ := os.ReadFile(outside); string(data) != "preserve" {
		t.Fatal("unrelated file modified")
	}
}

func TestCASQuotaIncludesInterruptedTempsAndUnknownLength(t *testing.T) {
	cas, err := OpenCASWithQuota(t.TempDir(), CacheQuota{MaxBytes: 4, MaxEntries: 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cas.tmpDir, "interrupted"), []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	data := []byte("12")
	digest := "sha256:" + sha256Hex(data)
	if err := cas.Commit(digest, bytes.NewReader(data), -1, 0); !errors.Is(err, ErrCacheQuota) {
		t.Fatal("unknown-length write bypassed quota", err)
	}
	if data, _ := os.ReadFile(filepath.Join(cas.tmpDir, "interrupted")); string(data) != "abc" {
		t.Fatal("old temp was deleted")
	}
}

func TestCASQuotaSustainedConcurrentInstances(t *testing.T) {
	root := t.TempDir()
	const workers = 8
	const rounds = 100
	quota := CacheQuota{MaxBytes: 128, MaxEntries: 100}
	var successes, denied atomic.Int64
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Prepare before workers so OpenCAS directory setup is not part of the test.
	stores := make([]*CAS, workers)
	for i := range stores {
		store, err := OpenCASWithQuota(root, quota)
		if err != nil {
			t.Fatal(err)
		}
		stores[i] = store
	}
	for worker, store := range stores {
		wg.Add(1)
		go func(worker int, store *CAS) {
			defer wg.Done()
			for round := 0; round < rounds; round++ {
				data := []byte(fmt.Sprintf("%04d:%04d", worker, round))
				digest := "sha256:" + sha256Hex(data)
				err := store.commitContext(ctx, digest, bytes.NewReader(data), int64(len(data)), 0)
				if err == nil {
					successes.Add(1)
				} else if errors.Is(err, ErrCacheQuota) {
					denied.Add(1)
				} else {
					t.Error(err)
					return
				}
			}
		}(worker, store)
	}
	wg.Wait()
	if successes.Load() == 0 || denied.Load() == 0 || successes.Load()+denied.Load() != workers*rounds {
		t.Fatal("incomplete load campaign", successes.Load(), denied.Load())
	}
	guard, err := AcquireCacheGuard(ctx, root, quota)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	if guard.Bytes > quota.MaxBytes {
		t.Fatal("aggregate quota exceeded")
	}
	if entries, _ := os.ReadDir(stores[0].tmpDir); len(entries) != 0 {
		t.Fatal("temporary files retained after contention")
	}
	t.Logf("800 writes: accepted=%d denied=%d logical_bytes=%d", successes.Load(), denied.Load(), guard.Bytes)
}
