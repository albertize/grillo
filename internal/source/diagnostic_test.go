// SPDX-License-Identifier: Apache-2.0

package source

import "testing"

func TestDiagnosticListSortAndErrors(t *testing.T) {
	list := List{
		{Code: "b", Severity: SeverityError, Span: &Span{Start: Position{File: "f", Line: 2}}},
		{Code: "a", Severity: SeverityWarning, Span: &Span{Start: Position{File: "f", Line: 1}}},
		{Code: "a", Severity: SeverityError},
	}
	if !list.HasErrors() {
		t.Fatal("expected HasErrors true")
	}
	if got := len(list.Errors()); got != 2 {
		t.Fatalf("Errors() = %d, want 2", got)
	}
	list.Sort()
	if list[0].Span == nil || list[0].Span.Start.Line != 1 || list[0].Code != "a" {
		t.Fatalf("sort order wrong: %+v", list)
	}
}

func TestDiagnosticErrorString(t *testing.T) {
	d := Diagnostic{Code: "c", Severity: SeverityError, Resource: "workload/w", Field: "field", Message: "boom", Span: &Span{Start: Position{File: "f", Line: 3, Column: 4}}}
	got := d.Error()
	for _, want := range []string{"error", "c", "workload/w.field", "boom", "f:3:4"} {
		if !contains(got, want) {
			t.Errorf("Error() = %q, missing %q", got, want)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestSourceMap(t *testing.T) {
	m := NewMap()
	m.Set("workload/api", Span{Start: Position{File: "app.json", Line: 5}})
	if span, ok := m.Get("workload/api"); !ok || span.Start.Line != 5 {
		t.Fatalf("Get returned %+v, %v", span, ok)
	}
	if _, ok := m.Get("missing"); ok {
		t.Error("unexpected span for missing key")
	}
	if m.Len() != 1 {
		t.Errorf("Len = %d, want 1", m.Len())
	}
}
