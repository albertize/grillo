// SPDX-License-Identifier: Apache-2.0

// Package source describes where application input came from and records source
// locations. It is intentionally dependency-free so both frontends and the IR
// model can use it without importing each other.
package source

import "fmt"

// Kind identifies the input format an application was loaded from.
type Kind string

const (
	KindCompose    Kind = "compose"
	KindHelm       Kind = "helm"
	KindKubernetes Kind = "kubernetes"
	KindNative     Kind = "native"
)

// Source is the origin of an application. Path is a location, so it is excluded
// from IR hashes.
type Source struct {
	Kind Kind   `json:"kind"`
	Path string `json:"path,omitempty"`
}

func (s Source) String() string {
	if s.Path == "" {
		return string(s.Kind)
	}
	return fmt.Sprintf("%s(%s)", s.Kind, s.Path)
}

// Position is a 1-based location in a text input.
type Position struct {
	File   string `json:"file,omitempty"`
	Line   int    `json:"line"`
	Column int    `json:"column,omitempty"`
}

// Span is a half-open source range.
type Span struct {
	Start Position `json:"start"`
	End   Position `json:"end,omitempty"`
}

// Map associates stable keys with source spans. Frontends build a Map while
// parsing; diagnostics and inspection read it without embedding locations in
// the IR itself.
type Map struct {
	spans map[string]Span
}

// NewMap returns an empty source map.
func NewMap() *Map { return &Map{spans: map[string]Span{}} }

// Set records a span for a key such as "workload/api/containers/web".
func (m *Map) Set(key string, span Span) {
	if m == nil {
		return
	}
	m.spans[key] = span
}

// Get returns the span for a key, if any.
func (m *Map) Get(key string) (Span, bool) {
	if m == nil {
		return Span{}, false
	}
	span, ok := m.spans[key]
	return span, ok
}

// Len reports how many spans the map holds.
func (m *Map) Len() int {
	if m == nil {
		return 0
	}
	return len(m.spans)
}
