//go:build linux

// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func blockTestOptions(t *testing.T) BlockOptions {
	t.Helper()
	path, err := exec.LookPath("mke2fs")
	if err != nil {
		t.Skip("reviewed mke2fs prerequisite unavailable")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(path, "-V").CombinedOutput()
	if err != nil || !strings.HasPrefix(string(out), "mke2fs 1.47.3 ") {
		t.Skip("reviewed mke2fs 1.47.3 prerequisite unavailable")
	}
	return BlockOptions{Formatter: path, DefaultBytes: 32 << 20, MaxReservedBytes: 64 << 20, MaxImages: 4}
}
func testBlockVolume(id string) Volume {
	return Volume{ID: id, Name: id, Kind: KindManaged, Owner: Owner{Application: "app"}}
}

func TestBlockQuotaConcurrentManagersAndRetry(t *testing.T) {
	opts := blockTestOptions(t)
	dir := t.TempDir()
	var managers []*Manager
	for range 12 {
		m, err := OpenBounded(dir, opts)
		if err != nil {
			t.Fatal(err)
		}
		managers = append(managers, m)
	}
	var accepted, denied atomic.Int32
	var wg sync.WaitGroup
	for i, m := range managers {
		wg.Go(func() {
			_, err := m.Create(context.Background(), testBlockVolume(fmt.Sprintf("v-%d", i)), "op")
			if err == nil {
				accepted.Add(1)
			} else if errors.Is(err, ErrQuota) {
				denied.Add(1)
			} else {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 2 || denied.Load() != 10 {
		t.Fatal(accepted.Load(), denied.Load())
	}
	volumes, err := managers[0].List()
	if err != nil || len(volumes) != 2 {
		t.Fatal(volumes, err)
	}
	if err := managers[0].Delete(context.Background(), volumes[0].ID); err != nil {
		t.Fatal(err)
	}
	volume, err := managers[1].Create(context.Background(), testBlockVolume("retry"), "op")
	if err != nil {
		t.Fatal(err)
	}
	if volume.Filesystem != "ext4" || volume.CapacityBytes != 32<<20 {
		t.Fatal(volume)
	}
	changed := testBlockVolume(volume.ID)
	changed.CapacityBytes = 64 << 20
	if _, err := managers[1].Create(context.Background(), changed, "op"); !errors.Is(err, ErrUnsupported) {
		t.Fatal("existing image resized silently", err)
	}
	if _, err := managers[1].Attach(context.Background(), volume.ID, "pod", false, 0, 0, "attach"); err != nil {
		t.Fatal(err)
	}
	if err := managers[0].Delete(context.Background(), volume.ID); !errors.Is(err, ErrBusy) {
		t.Fatal("leased image removed", err)
	}
}

func TestBlockRejectsLegacyGeometryAndInterruptedReservation(t *testing.T) {
	opts := blockTestOptions(t)
	dir := t.TempDir()
	ctx := context.Background()
	legacy, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	old, err := legacy.Create(ctx, testBlockVolume("legacy"), "op")
	if err != nil {
		t.Fatal(err)
	}
	token := filepath.Join(old.Source, "keep")
	if err := os.WriteFile(token, []byte("existing-data"), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := OpenBounded(dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(ctx, testBlockVolume("legacy"), "op"); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(token); err != nil || string(data) != "existing-data" {
		t.Fatal("legacy changed")
	}
	volume, err := m.Create(ctx, testBlockVolume("new"), "op")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(volume.Source, 33<<20); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(ctx, testBlockVolume("new"), "op"); err == nil {
		t.Fatal("changed geometry accepted")
	}
	if err := m.Delete(ctx, "new"); err != nil {
		t.Fatal(err)
	}
	interrupted, err := os.Create(filepath.Join(m.block.dir, ".image-interrupted"))
	if err != nil {
		t.Fatal(err)
	}
	if err := interrupted.Truncate(opts.MaxReservedBytes); err != nil {
		t.Fatal(err)
	}
	interrupted.Close()
	if _, err := m.Create(ctx, testBlockVolume("another"), "op"); !errors.Is(err, ErrQuota) {
		t.Fatal("interrupted file ignored", err)
	}
	if _, err := os.Stat(interrupted.Name()); err != nil {
		t.Fatal("unowned interrupted image deleted")
	}
}

func TestBlockQuotaCrossProcess(t *testing.T) {
	if dir := os.Getenv("GRILLO_BLOCK_QUOTA_CHILD"); dir != "" {
		m, err := OpenBounded(dir, blockTestOptions(t))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		if _, err := m.Create(ctx, testBlockVolume("child"), "op"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("cross-process admission bypass", err)
		}
		return
	}
	m, err := OpenBounded(t.TempDir(), blockTestOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := m.block.lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBlockQuotaCrossProcess$")
	command.Env = append(os.Environ(), "GRILLO_BLOCK_QUOTA_CHILD="+m.dir)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("child process: %v %s", err, output)
	}
}

func TestBlockFormattingFailureNeverPublishes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "formatter")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nif [ \"$1\" = -V ]; then echo 'mke2fs 1.47.3 (fixture)'; exit 0; fi\nexit 9\n"), 0700); err != nil {
		t.Fatal(err)
	}
	m, err := OpenBounded(t.TempDir(), BlockOptions{Formatter: path, DefaultBytes: 32 << 20, MaxReservedBytes: 64 << 20, MaxImages: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(context.Background(), testBlockVolume("failed"), "op"); err == nil {
		t.Fatal("failed formatter accepted")
	}
	entries, err := os.ReadDir(m.block.dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != ".quota.lock" {
		t.Fatal("failed stage leaked", entries, err)
	}
	if _, err := m.load("failed"); !errors.Is(err, ErrNotFound) {
		t.Fatal("failed volume record published", err)
	}
}

func TestBlockNegativeCapacityCancellationAndPaths(t *testing.T) {
	opts := blockTestOptions(t)
	m, err := OpenBounded(t.TempDir(), opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int64{1, 16<<20 + 1, -1} {
		v := testBlockVolume("bad")
		v.CapacityBytes = size
		if _, err := m.Create(context.Background(), v, "op"); err == nil {
			t.Fatal("bad capacity", size)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Create(ctx, testBlockVolume("canceled"), "op"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, id := range []string{".", "..", "../outside", "x/y", "x\\y"} {
		if _, err := m.Create(context.Background(), testBlockVolume(id), "op"); !errors.Is(err, ErrInvalid) {
			t.Fatal(id, err)
		}
		if err := m.Detach(context.Background(), "volume", id); !errors.Is(err, ErrInvalid) {
			t.Fatal(id, err)
		}
	}
	lock := filepath.Join(m.block.dir, ".quota.lock")
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/null", lock); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(context.Background(), testBlockVolume("unsafe-lock"), "op"); err == nil {
		t.Fatal("symlink quota lock accepted")
	}
}
