// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	"go.yaml.in/yaml/v3"

	"github.com/albertize/grillo/internal/source"
)

// mapGet returns the value node for a key in a mapping node.
func mapGet(node *yaml.Node, key string) (*yaml.Node, bool) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1], true
		}
	}
	return nil, false
}

// scalarValue returns a scalar mapping value.
func scalarValue(node *yaml.Node, key string) (string, bool) {
	value, ok := mapGet(node, key)
	if !ok || value.Kind != yaml.ScalarNode {
		return "", false
	}
	return value.Value, true
}

// sequence returns a node's sequence items, or nil.
func sequence(node *yaml.Node) []*yaml.Node {
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil
	}
	return node.Content
}

// spanOf records the location of a node.
func spanOf(node *yaml.Node, file string) *source.Span {
	line, column := 1, 1
	if node != nil {
		line, column = node.Line, node.Column
	}
	position := source.Position{File: file, Line: line, Column: column}
	return &source.Span{Start: position, End: position}
}

// spanValue is spanOf as a value for source maps.
func spanValue(node *yaml.Node, file string) source.Span { return *spanOf(node, file) }

// diagnostic builds a structured diagnostic.
func diagnostic(severity source.Severity, compatibility source.Compatibility, code string, node *yaml.Node, file, resource, field, message, consequence string) source.Diagnostic {
	return source.Diagnostic{
		Code:          code,
		Severity:      severity,
		Compatibility: compatibility,
		Span:          spanOf(node, file),
		Resource:      resource,
		Field:         field,
		Message:       message,
		Consequence:   consequence,
	}
}
