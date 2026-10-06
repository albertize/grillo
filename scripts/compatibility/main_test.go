// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/albertize/grillo/internal/source"
)

func TestRegistryDocumentsMatchValidators(t *testing.T) {
	data, text, err := documents()
	if err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string][]byte{"compatibility-registry.json": data, "compatibility-registry.md": text} {
		actual, err := os.ReadFile(filepath.Join("..", "..", "docs", name))
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("%s diverged from validators; run go run ./scripts/compatibility -output docs (err=%v)", name, err)
		}
	}
	again, againText, err := documents()
	if err != nil || !bytes.Equal(data, again) || !bytes.Equal(text, againText) {
		t.Fatal("registry output is nondeterministic")
	}
}

func TestRegistryEntryMetadataAndFixtures(t *testing.T) {
	seen := map[string]bool{}
	states := map[source.Compatibility]bool{}
	for _, entry := range entries() {
		key := entry.Format + "/" + entry.Scope + "/" + entry.Field
		if seen[key] {
			t.Fatal("duplicate registry key", key)
		}
		seen[key] = true
		if entry.Version == "" || entry.Milestone == "" || entry.Default == "" || len(entry.Fixtures) == 0 {
			t.Fatal("missing metadata", key)
		}
		states[entry.State] = true
		for _, fixture := range entry.Fixtures {
			if _, err := os.Stat(filepath.Join("..", "..", fixture)); err != nil {
				t.Fatal("missing regression suite", fixture, err)
			}
		}
	}
	for _, state := range []source.Compatibility{source.Supported, source.Degraded, source.ValidateOnly, source.Unsupported} {
		if !states[state] {
			t.Fatal("missing support state", state)
		}
	}
}
