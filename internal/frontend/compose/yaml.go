// SPDX-License-Identifier: Apache-2.0

package compose

import (
	"go.yaml.in/yaml/v3"

	"grillo.local/grillo/internal/source"
)

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

func scalarValue(node *yaml.Node, key string) (string, bool) {
	value, ok := mapGet(node, key)
	if !ok || value.Kind != yaml.ScalarNode {
		return "", false
	}
	return value.Value, true
}

func sequence(node *yaml.Node) []*yaml.Node {
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil
	}
	return node.Content
}

func spanOf(node *yaml.Node, file string) source.Span {
	if node == nil {
		return source.Span{}
	}
	return source.Span{Start: source.Position{File: file, Line: node.Line, Column: node.Column}}
}

func diagnostic(severity source.Severity, compatibility source.Compatibility, code string, node *yaml.Node, file, resource, field, message, consequence string) source.Diagnostic {
	return source.Diagnostic{
		Code:          code,
		Severity:      severity,
		Compatibility: compatibility,
		Span:          spanPtr(node, file),
		Resource:      resource,
		Field:         field,
		Message:       message,
		Consequence:   consequence,
	}
}

func spanPtr(node *yaml.Node, file string) *source.Span {
	if node == nil {
		return nil
	}
	span := spanOf(node, file)
	return &span
}

// interpolateNode expands variables in every string scalar in the tree.
func interpolateNode(node *yaml.Node, env map[string]string, diagnostics *source.List, file string) {
	if node == nil {
		return
	}
	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" {
		value, warnings, err := Interpolate(node.Value, env)
		if err != nil {
			*diagnostics = append(*diagnostics, diagnostic(source.SeverityError, source.Unsupported, "compose.interpolation", node, file, "", "", err.Error(), ""))
			return
		}
		node.Value = value
		for _, warning := range warnings {
			*diagnostics = append(*diagnostics, diagnostic(source.SeverityWarning, source.ValidateOnly, "compose.interpolation_warning", node, file, "", "", warning, ""))
		}
	}
	for _, child := range node.Content {
		interpolateNode(child, env, diagnostics, file)
	}
}
