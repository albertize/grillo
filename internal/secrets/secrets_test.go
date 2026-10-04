// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/state"
)

func TestPutGetVersionBehavior(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secrets")
	store, err := Open(dir, state.Ops{})
	if err != nil {
		t.Fatal(err)
	}
	ref1, err := store.Put("db", map[string][]byte{"password": []byte("s3cr3t")})
	if err != nil {
		t.Fatal(err)
	}
	if ref1.Name != "db" || ref1.ID == "" || ref1.Version == "" {
		t.Fatalf("unexpected ref %+v", ref1)
	}

	ref2, err := store.Put("db", map[string][]byte{"password": []byte("s3cr3t")})
	if err != nil {
		t.Fatal(err)
	}
	if ref2.ID != ref1.ID || ref2.Version != ref1.Version {
		t.Fatalf("version changed for identical value: %+v %+v", ref1, ref2)
	}

	ref3, err := store.Put("db", map[string][]byte{"password": []byte("new-secret")})
	if err != nil {
		t.Fatal(err)
	}
	if ref3.ID == ref1.ID || ref3.Version == ref1.Version {
		t.Fatalf("changed value reused identity: %+v", ref3)
	}

	got, err := store.Get(ref1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got["password"], []byte("s3cr3t")) {
		t.Fatalf("Get returned %q", got["password"])
	}

	if _, err := store.Get(model.SecretRef{Name: "db", ID: ref1.ID, Version: "bogus"}); !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("error = %v, want ErrVersionMismatch", err)
	}
	if _, err := store.Get(model.SecretRef{Name: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}

	fi, err := os.Stat(filepath.Join(dir, ref1.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("secret file mode = %v, want 0600", fi.Mode().Perm())
	}
	dfi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dfi.Mode().Perm() != 0o700 {
		t.Errorf("secret dir mode = %v, want 0700", dfi.Mode().Perm())
	}
}

func TestRefsDoNotLeakValues(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secrets")
	store, err := Open(dir, state.Ops{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put("db", map[string][]byte{"password": []byte("super-secret-value")}); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(store.Refs())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("super-secret-value")) {
		t.Fatalf("public refs leaked a value: %s", data)
	}
}

func TestCollectRemovesOnlyUnreferencedVersions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secrets")
	store, err := Open(dir, state.Ops{})
	if err != nil {
		t.Fatal(err)
	}
	old, err := store.Put("db", map[string][]byte{"password": []byte("old")})
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.Put("db", map[string][]byte{"password": []byte("current")})
	if err != nil {
		t.Fatal(err)
	}
	removed, err := store.Collect(func(ref model.SecretRef) bool { return ref.ID == current.ID })
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, err := store.Get(old); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old version still readable: %v", err)
	}
	if _, err := store.Get(current); err != nil {
		t.Fatalf("current version lost: %v", err)
	}
	if refs := store.Refs(); len(refs) != 1 || refs[0].ID != current.ID {
		t.Fatalf("unexpected refs after collect: %+v", refs)
	}
}

func TestSecretValuesDoNotLeakIntoStateOrPublicIR(t *testing.T) {
	base := t.TempDir()
	store, err := Open(filepath.Join(base, "secrets"), state.Ops{})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Put("db", map[string][]byte{"password": []byte("super-secret-value")})
	if err != nil {
		t.Fatal(err)
	}
	app := model.Application{
		APIVersion: model.APIVersion,
		Kind:       model.KindApplication,
		Identity:   model.Identity{Name: "x", Namespace: model.DefaultNamespace},
		Secrets:    []model.SecretRef{ref},
	}
	pub, err := model.Public(app)
	if err != nil {
		t.Fatal(err)
	}

	layout, err := state.NewLayout(state.Config{
		Getenv: func(k string) string {
			return map[string]string{
				"XDG_RUNTIME_DIR": filepath.Join(base, "run"),
				"XDG_STATE_HOME":  filepath.Join(base, "state"),
				"XDG_DATA_HOME":   filepath.Join(base, "data"),
				"XDG_CACHE_HOME":  filepath.Join(base, "cache"),
			}[k]
		},
		UID: os.Getuid(), Home: base, TempDir: base,
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := state.Open(layout, state.Ops{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Commit(state.Snapshot{Application: &pub}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(st.SnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("super-secret-value")) {
		t.Fatalf("state snapshot leaked a secret value: %s", data)
	}
	if !bytes.Contains(data, []byte(ref.Version)) {
		t.Fatalf("state snapshot should retain the secret version: %s", data)
	}
}
