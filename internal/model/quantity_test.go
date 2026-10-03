// SPDX-License-Identifier: Apache-2.0

package model

import "testing"

func TestParseMillicores(t *testing.T) {
	valid := map[string]Millicores{
		"1":     1000,
		"0.5":   500,
		"1.25":  1250,
		"100m":  100,
		"1000m": 1000,
		"0":     0,
	}
	for in, want := range valid {
		got, err := ParseMillicores(in)
		if err != nil {
			t.Errorf("ParseMillicores(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseMillicores(%q) = %d, want %d", in, got, want)
		}
	}
	invalid := []string{"", "-1", "abc", "1.2345", "1.5m", "100mi", "9223372036854775808", "99999999999999999999m"}
	for _, in := range invalid {
		if got, err := ParseMillicores(in); err == nil {
			t.Errorf("ParseMillicores(%q) = %d, want error", in, got)
		}
	}
}

func TestParseBytes(t *testing.T) {
	valid := map[string]Bytes{
		"128Mi": 134217728,
		"1Gi":   1073741824,
		"500M":  500000000,
		"1.5Gi": 1610612736,
		"12345": 12345,
		"1k":    1000,
		"2E":    2000000000000000000,
	}
	for in, want := range valid {
		got, err := ParseBytes(in)
		if err != nil {
			t.Errorf("ParseBytes(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseBytes(%q) = %d, want %d", in, got, want)
		}
	}
	invalid := []string{"", "-1", "10MiB", "1.2345Gi", "9223372036854775808", "9999999999999Ei", "1.5m", "3g"}
	for _, in := range invalid {
		if got, err := ParseBytes(in); err == nil {
			t.Errorf("ParseBytes(%q) = %d, want error", in, got)
		}
	}
}

func TestQuantityString(t *testing.T) {
	if got := Millicores(1500).String(); got != "1500m" {
		t.Errorf("Millicores.String() = %q", got)
	}
	if got := Bytes(1024).String(); got != "1024B" {
		t.Errorf("Bytes.String() = %q", got)
	}
}
