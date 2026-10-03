// SPDX-License-Identifier: Apache-2.0

package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grillo.local/grillo/internal/model"
)

func app(name string) *model.Application {
	return &model.Application{
		APIVersion: model.APIVersion,
		Kind:       model.KindApplication,
		Identity:   model.Identity{Name: name, Namespace: model.DefaultNamespace},
	}
}

func TestCommitLoadRoundTrip(t *testing.T) {
	layout := testLayout(t)
	store, err := Open(layout, Ops{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	snap := Snapshot{
		Application: app("store"),
		Operations:  []Operation{{ID: "op-1", Kind: "up", Status: OperationPending}},
	}
	if err := store.Commit(snap); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(store.SnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("state file mode = %v, want 0600", fi.Mode().Perm())
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != CurrentVersion || got.Application == nil || got.Application.Identity.Name != "store" {
		t.Fatalf("unexpected snapshot: %+v", got)
	}
	if len(got.Operations) != 1 || got.Operations[0].ID != "op-1" {
		t.Fatalf("operations not preserved: %+v", got.Operations)
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	store, err := Open(testLayout(t), Ops{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != CurrentVersion || got.Application != nil {
		t.Fatalf("unexpected empty snapshot: %+v", got)
	}
}

func TestSecondWriterRejected(t *testing.T) {
	layout := testLayout(t)
	first, err := Open(layout, Ops{})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := Open(layout, Ops{}); !errors.Is(err, ErrWriterBusy) {
		t.Fatalf("second Open error = %v, want ErrWriterBusy", err)
	}
}

func TestFutureVersionRejected(t *testing.T) {
	layout := testLayout(t)
	store, err := Open(layout, Ops{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := AtomicWriteFile(layout.State, "state.json", []byte(`{"version":99}`), 0o600, DefaultOps()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrFutureVersion) {
		t.Fatalf("Load error = %v, want ErrFutureVersion", err)
	}
}

func TestLegacyVersionMigratedWithBackup(t *testing.T) {
	layout := testLayout(t)
	store, err := Open(layout, Ops{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	legacy := []byte(`{"application":{"apiVersion":"grillo.dev/v1alpha1","kind":"Application","metadata":{"name":"legacy"}}}`)
	if err := AtomicWriteFile(layout.State, "state.json", legacy, 0o600, DefaultOps()); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != CurrentVersion {
		t.Fatalf("migrated version = %d, want %d", got.Version, CurrentVersion)
	}
	if _, err := os.Stat(filepath.Join(layout.State, "state.v0.bak")); err != nil {
		t.Fatalf("expected backup before migration: %v", err)
	}
}

func TestCorruptStateReported(t *testing.T) {
	layout := testLayout(t)
	store, err := Open(layout, Ops{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := AtomicWriteFile(layout.State, "state.json", []byte("{not json"), 0o600, DefaultOps()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Load error = %v, want ErrCorrupt", err)
	}
}

func TestStrayTempFileIgnored(t *testing.T) {
	layout := testLayout(t)
	store, err := Open(layout, Ops{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Commit(Snapshot{Application: app("one")}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(layout.State, ".state.json.tmp-xyz"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Application == nil || got.Application.Identity.Name != "one" {
		t.Fatalf("stray temp affected load: %+v", got)
	}
}

// TestCommitFailureLeavesPreviousState injects a failure at each atomic-write
// step and verifies the previous state survives and no temp file leaks.
func TestCommitFailureLeavesPreviousState(t *testing.T) {
	steps := map[string]func(*Ops){
		"createTemp": func(o *Ops) {
			o.CreateTemp = func(string, string) (*os.File, error) { return nil, errors.New("boom") }
		},
		"write": func(o *Ops) {
			o.Write = func(*os.File, []byte) (int, error) { return 0, errors.New("boom") }
		},
		"chmod": func(o *Ops) { o.Chmod = func(*os.File, os.FileMode) error { return errors.New("boom") } },
		"sync":  func(o *Ops) { o.Sync = func(*os.File) error { return errors.New("boom") } },
		"close": func(o *Ops) { o.Close = func(f *os.File) error { _ = f.Close(); return errors.New("boom") } },
		"rename": func(o *Ops) {
			o.Rename = func(string, string) error { return errors.New("boom") }
		},
	}
	for name, inject := range steps {
		t.Run(name, func(t *testing.T) {
			layout := testLayout(t)
			seed, err := Open(layout, Ops{})
			if err != nil {
				t.Fatal(err)
			}
			if err := seed.Commit(Snapshot{Application: app("one")}); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(seed.SnapshotPath())
			if err != nil {
				t.Fatal(err)
			}
			seed.Close()

			ops := DefaultOps()
			inject(&ops)
			store, err := Open(layout, ops)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if err := store.Commit(Snapshot{Application: app("two")}); err == nil {
				t.Fatal("expected injected commit failure")
			}
			after, err := os.ReadFile(store.SnapshotPath())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Errorf("previous state changed after failure:\n%s\n%s", before, after)
			}
			var snap Snapshot
			if err := json.Unmarshal(after, &snap); err != nil || snap.Application.Identity.Name != "one" {
				t.Errorf("previous state not readable: %v %+v", err, snap)
			}
			entries, err := os.ReadDir(layout.State)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".state.json.tmp-") {
					t.Errorf("temp file leaked: %s", e.Name())
				}
			}
		})
	}
}

// TestCommitSyncDirFailureKeepsNewState documents that a directory-fsync failure
// happens after the rename, so the new state is visible; only durability on
// crash is at risk. No temp file may leak.
func TestCommitSyncDirFailureKeepsNewState(t *testing.T) {
	layout := testLayout(t)
	seed, err := Open(layout, DefaultOps())
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Commit(Snapshot{Application: app("one")}); err != nil {
		t.Fatal(err)
	}
	seed.Close()

	ops := DefaultOps()
	ops.SyncDir = func(string) error { return errors.New("boom") }
	store, err := Open(layout, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Commit(Snapshot{Application: app("two")}); err == nil {
		t.Fatal("expected injected syncDir failure")
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Application == nil || got.Application.Identity.Name != "two" {
		t.Fatalf("rename should have published new state: %+v", got)
	}
	entries, err := os.ReadDir(layout.State)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".state.json.tmp-") {
			t.Errorf("temp file leaked: %s", e.Name())
		}
	}
}
