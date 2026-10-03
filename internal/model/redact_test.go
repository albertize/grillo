// SPDX-License-Identifier: Apache-2.0

package model

import (
	"bytes"
	"strings"
	"testing"
)

func TestPublicRedactsSensitiveEntries(t *testing.T) {
	app := loadFixture(t)
	app.Configs[0].Entries = append(app.Configs[0].Entries, ConfigEntry{Key: "TOKEN", Text: "super-secret-value", Sensitive: true})

	pub, err := Public(app)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, entry := range pub.Configs[0].Entries {
		if entry.Key == "TOKEN" {
			found = true
			if entry.Text != Redacted || entry.Binary != nil {
				t.Errorf("sensitive entry not redacted: %+v", entry)
			}
		}
	}
	if !found {
		t.Fatal("sensitive entry missing from public view")
	}
	if app.Configs[0].Entries[len(app.Configs[0].Entries)-1].Text != "super-secret-value" {
		t.Error("Public mutated the original application")
	}
	data, err := CanonicalJSON(pub)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("super-secret-value")) {
		t.Error("public canonical JSON leaked a sensitive value")
	}
}

func TestPublicNeverContainsSecretValues(t *testing.T) {
	app := loadFixture(t)
	pub, err := Public(app)
	if err != nil {
		t.Fatal(err)
	}
	data, err := CanonicalJSON(pub)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture references a secret by name/version only; no value exists in
	// the IR, so the public view must name it without carrying any value.
	if !bytes.Contains(data, []byte("db-secret")) {
		t.Error("public view should retain the secret reference name")
	}
}

func TestRedactText(t *testing.T) {
	text := "error: token abc123 rejected; retry abc123"
	got := RedactText(text, []string{"abc123", ""})
	if strings.Contains(got, "abc123") {
		t.Errorf("RedactText leaked a secret: %q", got)
	}
	if strings.Count(got, Redacted) != 2 {
		t.Errorf("expected two redactions, got %q", got)
	}
}
