// SPDX-License-Identifier: Apache-2.0

// Package detect identifies the input format of an application source. It is
// pure and shared by frontends and the CLI; ambiguous input requires an explicit
// format instead of silent guessing.
package detect

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"

	"go.yaml.in/yaml/v3"

	"grillo.local/grillo/internal/source"
)

// Format returns the detected kind. An empty input is treated as a native empty
// application so `plan`/`up` diagnose it consistently.
func Format(data []byte, path string) (source.Kind, error) {
	if filepath.Base(path) == "Chart.yaml" {
		return source.KindHelm, nil
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return source.KindNative, nil
	}
	var document yaml.Node
	if err := yaml.Unmarshal(trimmed, &document); err != nil {
		return "", fmt.Errorf("detect: parse: %w", err)
	}
	if len(document.Content) == 0 {
		return source.KindNative, nil
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return "", errors.New("detect: input is not a mapping; use --format")
	}
	hasServices := hasKey(root, "services")
	apiVersion, hasAPIVersion := keyValue(root, "apiVersion")
	kind, hasKind := keyValue(root, "kind")
	switch {
	case hasServices && hasAPIVersion:
		return "", errors.New("detect: ambiguous input defines both services and apiVersion; pass --format")
	case hasServices:
		return source.KindCompose, nil
	case hasAPIVersion && kind == "Application":
		return source.KindNative, nil
	case hasKind:
		_ = apiVersion
		return source.KindKubernetes, nil
	default:
		return "", errors.New("detect: cannot determine the input format; pass --format")
	}
}

func hasKey(node *yaml.Node, key string) bool {
	_, ok := keyValue(node, key)
	return ok
}

func keyValue(node *yaml.Node, key string) (string, bool) {
	if node == nil || node.Kind != yaml.MappingNode {
		return "", false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1].Value, true
		}
	}
	return "", false
}
