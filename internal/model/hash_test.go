// SPDX-License-Identifier: Apache-2.0

package model

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"grillo.local/grillo/internal/source"
)

var update = flag.Bool("update", false, "update golden files")

func loadFixture(t *testing.T) Application {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "application.json"))
	if err != nil {
		t.Fatal(err)
	}
	app, diags, err := LoadManifest(data)
	if err != nil {
		t.Fatalf("LoadManifest: %v (%v)", err, diags)
	}
	return app
}

func TestCanonicalJSONGolden(t *testing.T) {
	app := loadFixture(t)
	got, err := CanonicalJSON(app)
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "application.canonical.json")
	if *update {
		if err := os.WriteFile(golden, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update): %v", err)
	}
	if string(got)+"\n" != string(want) {
		t.Errorf("canonical JSON differs from %s; rerun with -update if intended", golden)
	}
}

func TestHashDeterministic(t *testing.T) {
	app := loadFixture(t)
	first, err := Hash(app)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Hash(app)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("hash not deterministic: %s != %s", first, second)
	}
	if len(first) != len("sha256:")+64 {
		t.Fatalf("unexpected hash format %q", first)
	}
}

func TestHashIgnoresVolatileFields(t *testing.T) {
	base := loadFixture(t)
	want, err := Hash(base)
	if err != nil {
		t.Fatal(err)
	}

	changed := base
	changed.Source = &source.Source{Kind: source.KindNative, Path: "/elsewhere/app.json"}
	changed.Identity.ID = "runtime-assigned-id"
	changed.Routes[0].Endpoint = "127.0.0.1:49152"
	got, err := Hash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("volatile fields changed the hash:\n want %s\n  got %s", want, got)
	}
}

func TestHashSensitiveToRealChanges(t *testing.T) {
	base := loadFixture(t)
	want, err := Hash(base)
	if err != nil {
		t.Fatal(err)
	}

	secret := base
	secret.Secrets[0].Version = "v2"
	got, err := Hash(secret)
	if err != nil {
		t.Fatal(err)
	}
	if got == want {
		t.Error("secret version change did not change the hash")
	}

	digest := base
	digest.Workloads[0].Template.Containers[0].Image.Digest = "sha256:changed"
	got, err = Hash(digest)
	if err != nil {
		t.Fatal(err)
	}
	if got == want {
		t.Error("image digest change did not change the hash")
	}
}
