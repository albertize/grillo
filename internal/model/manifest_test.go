// SPDX-License-Identifier: Apache-2.0

package model

import (
	"bytes"
	"testing"
)

func TestLoadManifestRejectsUnknownField(t *testing.T) {
	data := []byte(`{"apiVersion":"grillo.dev/v1alpha1","kind":"Application","metadata":{"name":"x"},"workloadz":[]}`)
	_, diags, err := LoadManifest(data)
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
	if !hasCode(diags, CodeManifestDecode) {
		t.Fatalf("expected %s, got %v", CodeManifestDecode, diags)
	}
}

func TestLoadManifestRejectsTrailingData(t *testing.T) {
	data := []byte(`{"apiVersion":"grillo.dev/v1alpha1","kind":"Application","metadata":{"name":"x"}}{}`)
	if _, _, err := LoadManifest(data); err == nil {
		t.Fatal("expected error for trailing data")
	}
}

func TestMarshalManifestRoundTrip(t *testing.T) {
	app := loadFixture(t)
	first, err := MarshalManifest(app)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, _, err := LoadManifest(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := MarshalManifest(reloaded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Errorf("manifest round trip not stable:\n%s\n%s", first, second)
	}
}
