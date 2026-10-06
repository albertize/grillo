//go:build linux

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/storage"
)

func TestEphemeralVolumeSandboxOwnership(t *testing.T) {
	ctx := context.Background()
	manager, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &Executor{cfg: Config{Volumes: manager}}
	volume := model.Volume{Name: "shared", Kind: model.VolumeEphemeral}
	first, _, err := e.attachVolume(ctx, "app", model.Application{}, volume, "pod-0")
	if err != nil {
		t.Fatal(err)
	}
	again, _, err := e.attachVolume(ctx, "app", model.Application{}, volume, "pod-0")
	if err != nil || again != first {
		t.Fatalf("same Pod did not share volume: %v", err)
	}
	second, _, err := e.attachVolume(ctx, "app", model.Application{}, volume, "pod-1")
	if err != nil || first == second {
		t.Fatalf("replicas did not get private emptyDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(first, "marker"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(second, "marker")); !os.IsNotExist(err) {
		t.Fatal("replica sees sibling emptyDir")
	}
	// Cleanup does not depend on a still-present desired template.
	if err := e.detachVolumes(ctx, "app", "pod-0"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatal("ephemeral data survived sandbox teardown")
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatal("cleanup removed sibling data", err)
	}
	if err := e.detachVolumes(ctx, "app", "pod-0"); err != nil {
		t.Fatal("repeated cleanup", err)
	}
	fresh, _, err := e.attachVolume(ctx, "app", model.Application{}, volume, "pod-0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(fresh, "marker")); !os.IsNotExist(err) {
		t.Fatal("recreated Pod inherited stale emptyDir")
	}
	if err := e.detachVolumes(ctx, "foreign-app", "pod-1"); err != nil {
		t.Fatal(err)
	}
	_, leases, err := manager.Inspect(ephemeralVolumeID("app", "shared", "pod-1"))
	if err != nil || len(leases) != 1 {
		t.Fatal("foreign application released lease", err)
	}
}

func TestEphemeralCleanupReportsBusyAndStoreFailure(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	manager, err := storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := &Executor{cfg: Config{Volumes: manager}}
	volume := model.Volume{Name: "shared", Kind: model.VolumeEphemeral, ReadOnly: true}
	source, _, err := e.attachVolume(ctx, "app", model.Application{}, volume, "pod-0")
	if err != nil {
		t.Fatal(err)
	}
	id := ephemeralVolumeID("app", "shared", "pod-0")
	// Even a scoped volume must not be deleted while an additional persisted
	// lease exists. Storage permits multiple readers, but not multiple writers.
	if _, err := manager.Attach(ctx, id, "extra-reader", true, 0, 0, "reader"); err != nil {
		t.Fatal(err)
	}
	if err := e.detachVolumes(ctx, "app", "pod-0"); !errors.Is(err, storage.ErrBusy) {
		t.Fatalf("cleanup swallowed busy deletion: %v", err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatal("busy volume was deleted", err)
	}
	// A failed inventory read is not evidence that resources are absent.
	inventory := filepath.Join(dir, "volumes")
	if err := os.Rename(inventory, inventory+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inventory, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.detachVolumes(ctx, "app", "pod-0"); err == nil {
		t.Fatal("cleanup ignored unreadable inventory")
	}
}

func TestPersistentVolumeCleanupRetainsData(t *testing.T) {
	ctx := context.Background()
	manager, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &Executor{cfg: Config{Volumes: manager}}
	volume := model.Volume{Name: "data", Kind: model.VolumePVC}
	first, _, err := e.attachVolume(ctx, "app", model.Application{}, volume, "pod-0")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.attachVolume(ctx, "app", model.Application{}, volume, "pod-1"); err == nil {
		t.Fatal("writable PVC attached to two VMs")
	}
	if err := os.WriteFile(filepath.Join(first, "marker"), []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.detachVolumes(ctx, "app", "pod-0"); err != nil {
		t.Fatal(err)
	}
	second, _, err := e.attachVolume(ctx, "app", model.Application{}, volume, "pod-1")
	if err != nil || first != second {
		t.Fatal("PVC identity changed", err)
	}
	if _, err := os.Stat(filepath.Join(second, "marker")); err != nil {
		t.Fatal("PVC data lost", err)
	}
}
