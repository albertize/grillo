// SPDX-License-Identifier: Apache-2.0

package source

import (
	"fmt"
	"sort"
	"strings"
)

// Severity is independent of compatibility state.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Compatibility is the honest support state of a feature.
type Compatibility string

const (
	Supported    Compatibility = "SUPPORTED"
	Degraded     Compatibility = "DEGRADED"
	ValidateOnly Compatibility = "VALIDATE_ONLY"
	Unsupported  Compatibility = "UNSUPPORTED"
)

// Diagnostic is a structured, field-level explanation. It never carries secret
// values; callers must redact messages before adding untrusted data.
type Diagnostic struct {
	Code          string        `json:"code"`
	Severity      Severity      `json:"severity"`
	Compatibility Compatibility `json:"support,omitempty"`
	Span          *Span         `json:"span,omitempty"`
	Resource      string        `json:"resource,omitempty"`
	Field         string        `json:"field,omitempty"`
	Message       string        `json:"message"`
	Consequence   string        `json:"consequence,omitempty"`
	Remediation   string        `json:"remediation,omitempty"`
}

func (d Diagnostic) Error() string {
	var b strings.Builder
	b.WriteString(string(d.Severity))
	b.WriteString(": ")
	b.WriteString(d.Code)
	if d.Resource != "" || d.Field != "" {
		b.WriteString(" [")
		b.WriteString(d.Resource)
		if d.Field != "" {
			b.WriteString(".")
			b.WriteString(d.Field)
		}
		b.WriteString("]")
	}
	b.WriteString(": ")
	b.WriteString(d.Message)
	if d.Span != nil {
		fmt.Fprintf(&b, " (%s:%d:%d)", d.Span.Start.File, d.Span.Start.Line, d.Span.Start.Column)
	}
	return b.String()
}

// List is an ordered collection of diagnostics with stable sorting.
type List []Diagnostic

// HasErrors reports whether any diagnostic has error severity.
func (l List) HasErrors() bool {
	for _, d := range l {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Errors returns only error-severity diagnostics.
func (l List) Errors() List {
	var out List
	for _, d := range l {
		if d.Severity == SeverityError {
			out = append(out, d)
		}
	}
	return out
}

// Sort orders diagnostics deterministically by span, code, resource, and field
// so reports and golden files are reproducible.
func (l List) Sort() {
	sort.SliceStable(l, func(i, j int) bool {
		a, b := l[i], l[j]
		an, bn := a.Span == nil, b.Span == nil
		if an != bn {
			return !an // located diagnostics first
		}
		if !an {
			if ka, kb := spanKey(a.Span), spanKey(b.Span); ka != kb {
				return ka < kb
			}
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Resource != b.Resource {
			return a.Resource < b.Resource
		}
		return a.Field < b.Field
	})
}

func spanKey(s *Span) string {
	if s == nil {
		return ""
	}
	return fmt.Sprintf("%s:%09d:%09d", s.Start.File, s.Start.Line, s.Start.Column)
}
