//go:build linux

// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	m, err := Open(filepath.Join(t.TempDir(), "storage"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func managedVolume(id, app string) Volume {
	return Volume{ID: id, Name: id, Kind: KindManaged, AccessMode: AccessReadWriteOnce, Owner: Owner{Application: app}}
}

func TestCreateManagedPersists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "storage")
	m, _ := Open(dir)
	ctx := context.Background()
	volume, err := m.Create(ctx, managedVolume("data", "app"), "op-1")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(volume.Source)
	if err != nil || !info.IsDir() {
		t.Fatalf("managed dir missing: %v", err)
	}
	// Reopening reloads the record (the "database survives restart" property).
	reopened, _ := Open(dir)
	list, err := reopened.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != "data" {
		t.Fatalf("list = %+v", list)
	}
}

func TestCreateIdempotentByOperation(t *testing.T) {
	m := testManager(t)
	ctx := context.Background()
	first, err := m.Create(ctx, managedVolume("data", "app"), "op-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Create(ctx, managedVolume("data", "app"), "op-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.CreatedAt != second.CreatedAt {
		t.Fatal("idempotent create returned a different volume")
	}
	if _, err := m.Create(ctx, managedVolume("data", "app"), "op-2"); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
}

func TestCreateRejectsReadWriteMany(t *testing.T) {
	m := testManager(t)
	v := managedVolume("data", "app")
	v.AccessMode = AccessReadWriteMany
	if _, err := m.Create(context.Background(), v, "op"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}

func TestValidateBind(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	resolved, err := ValidateBind(link)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != real {
		t.Fatalf("resolved = %q, want %q", resolved, real)
	}
	bad := []string{"relative/path", "/", "/nonexistent-grillo-path"}
	if home, err := os.UserHomeDir(); err == nil {
		bad = append(bad, home)
	}
	for _, path := range bad {
		if _, err := ValidateBind(path); err == nil {
			t.Errorf("ValidateBind(%q) succeeded, want error", path)
		}
	}
}

func TestAccessModesAndReadOnly(t *testing.T) {
	m := testManager(t)
	ctx := context.Background()
	if _, err := m.Create(ctx, managedVolume("rwo", "app"), "op"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach(ctx, "rwo", "sbx-1", false, 0, 0, "op"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach(ctx, "rwo", "sbx-2", false, 0, 0, "op"); !errors.Is(err, ErrBusy) {
		t.Fatalf("second writer err = %v, want ErrBusy", err)
	}
	if _, err := m.Attach(ctx, "rwo", "sbx-2", true, 0, 0, "op"); !errors.Is(err, ErrBusy) {
		t.Fatalf("read-only second attach on RWO err = %v, want ErrBusy", err)
	}

	ro := Volume{ID: "ro", Name: "ro", Kind: KindManaged, ReadOnly: true, AccessMode: AccessReadOnlyMany, Owner: Owner{Application: "app"}}
	if _, err := m.Create(ctx, ro, "op"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach(ctx, "ro", "sbx-1", false, 0, 0, "op"); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("write to read-only volume err = %v, want ErrReadOnly", err)
	}
	if _, err := m.Attach(ctx, "ro", "sbx-1", true, 0, 0, "op"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach(ctx, "ro", "sbx-2", true, 0, 0, "op"); err != nil {
		t.Fatal(err)
	}
}

func TestAttachmentUIDMapping(t *testing.T) {
	m := testManager(t)
	ctx := context.Background()
	if _, err := m.Create(ctx, managedVolume("data", "app"), "op"); err != nil {
		t.Fatal(err)
	}
	attachment, err := m.Attach(ctx, "data", "sbx-1", false, 1000, 2000, "op")
	if err != nil {
		t.Fatal(err)
	}
	if attachment.UID != 1000 || attachment.GID != 2000 {
		t.Fatalf("attachment = %+v", attachment)
	}
	_, leases, err := m.Inspect("data")
	if err != nil || len(leases) != 1 || leases[0].UID != 1000 {
		t.Fatalf("leases = %+v err=%v", leases, err)
	}
}

func TestDetachAndReleaseSandbox(t *testing.T) {
	m := testManager(t)
	ctx := context.Background()
	if _, err := m.Create(ctx, managedVolume("a", "app"), "op"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(ctx, managedVolume("b", "app"), "op"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach(ctx, "a", "sbx-1", false, 0, 0, "op"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach(ctx, "b", "sbx-1", false, 0, 0, "op"); err != nil {
		t.Fatal(err)
	}
	if err := m.Detach(ctx, "a", "sbx-1"); err != nil {
		t.Fatal(err)
	}
	if _, leases, _ := m.Inspect("a"); len(leases) != 0 {
		t.Fatalf("lease not released: %+v", leases)
	}
	if err := m.ReleaseSandbox(ctx, "sbx-1"); err != nil {
		t.Fatal(err)
	}
	if _, leases, _ := m.Inspect("b"); len(leases) != 0 {
		t.Fatalf("release did not clear lease: %+v", leases)
	}
}

func TestReconcileDropsLeasesForGoneSandboxes(t *testing.T) {
	m := testManager(t)
	ctx := context.Background()
	if _, err := m.Create(ctx, Volume{ID: "data", Name: "data", Kind: KindManaged, ReadOnly: true, AccessMode: AccessReadOnlyMany, Owner: Owner{Application: "app"}}, "op"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach(ctx, "data", "alive", true, 0, 0, "op"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach(ctx, "data", "dead", true, 0, 0, "op"); err != nil {
		t.Fatal(err)
	}
	if err := m.Reconcile(func(id string) bool { return id == "alive" }); err != nil {
		t.Fatal(err)
	}
	_, leases, _ := m.Inspect("data")
	if len(leases) != 1 || leases[0].SandboxID != "alive" {
		t.Fatalf("reconcile left %+v", leases)
	}
}

func TestDeleteRefusesBindAndLeased(t *testing.T) {
	m := testManager(t)
	ctx := context.Background()
	host := t.TempDir()
	bind := Volume{ID: "bind", Name: "bind", Kind: KindBind, Source: host, Owner: Owner{Application: "app"}}
	if _, err := m.Create(ctx, bind, "op"); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(ctx, "bind"); !errors.Is(err, ErrNotOwned) {
		t.Fatalf("delete bind err = %v, want ErrNotOwned", err)
	}
	if _, err := os.Stat(host); err != nil {
		t.Fatal("bind source was deleted")
	}

	if _, err := m.Create(ctx, managedVolume("data", "app"), "op"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach(ctx, "data", "sbx-1", false, 0, 0, "op"); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(ctx, "data"); !errors.Is(err, ErrBusy) {
		t.Fatalf("delete leased err = %v, want ErrBusy", err)
	}
	if err := m.Detach(ctx, "data", "sbx-1"); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(ctx, "data"); err != nil {
		t.Fatalf("delete after detach: %v", err)
	}
}

func TestDeleteOwnedSkipsBindAndForeignVolumes(t *testing.T) {
	m := testManager(t)
	ctx := context.Background()
	// Owned managed, owned bind, foreign managed.
	owned := managedVolume("owned", "app")
	if _, err := m.Create(ctx, owned, "op"); err != nil {
		t.Fatal(err)
	}
	host := t.TempDir()
	bind := Volume{ID: "bind", Name: "bind", Kind: KindBind, Source: host, Owner: Owner{Application: "app"}}
	if _, err := m.Create(ctx, bind, "op"); err != nil {
		t.Fatal(err)
	}
	foreign := managedVolume("foreign", "other")
	if _, err := m.Create(ctx, foreign, "op"); err != nil {
		t.Fatal(err)
	}

	deleted, err := m.DeleteOwned(ctx, "app", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0] != "owned" {
		t.Fatalf("deleted = %v", deleted)
	}
	if _, err := os.Stat(host); err != nil {
		t.Fatal("bind volume content was deleted by down --volumes")
	}
	if _, _, err := m.Inspect("foreign"); err != nil {
		t.Fatal("foreign volume was deleted")
	}
}
