//go:build linux

// SPDX-License-Identifier: Apache-2.0

package distribution

import (
	"debug/buildinfo"
	"fmt"
	"path"
	"strings"
)

// BinaryProvenance is extracted from the copied bytes, not the builder's module
// graph. It identifies declared inputs; it is not publisher authentication or
// corresponding-source/license clearance. Arbitrary linker/settings text is omitted.
type BinaryProvenance struct {
	Path      string             `json:"path"`
	GoVersion string             `json:"go_version"`
	Main      ModuleProvenance   `json:"main"`
	Modules   []ModuleProvenance `json:"modules"`
}
type ModuleProvenance struct {
	Path        string            `json:"path"`
	Version     string            `json:"version"`
	Sum         string            `json:"sum,omitempty"`
	Replacement *ModuleProvenance `json:"replacement,omitempty"`
}

func binaryProvenance(file, relative string) (BinaryProvenance, error) {
	info, err := buildinfo.ReadFile(file)
	if err != nil {
		return BinaryProvenance{}, fmt.Errorf("stage: missing Go build metadata for %s", relative)
	}
	if len(info.Deps) > 2048 || len(info.GoVersion) > 128 {
		return BinaryProvenance{}, fmt.Errorf("stage: oversized Go build metadata")
	}
	main, err := moduleProvenance(info.Main.Path, info.Main.Version, info.Main.Sum, false)
	if err != nil {
		return BinaryProvenance{}, err
	}
	result := BinaryProvenance{Path: relative, GoVersion: info.GoVersion, Main: main, Modules: []ModuleProvenance{}}
	for _, dep := range info.Deps {
		entry, err := moduleProvenance(dep.Path, dep.Version, dep.Sum, false)
		if err != nil {
			return BinaryProvenance{}, err
		}
		if dep.Replace != nil {
			replacement, err := moduleProvenance(dep.Replace.Path, dep.Replace.Version, dep.Replace.Sum, true)
			if err != nil {
				return BinaryProvenance{}, err
			}
			entry.Replacement = &replacement
		}
		result.Modules = append(result.Modules, entry)
	}
	return result, nil
}

func moduleProvenance(p, v, s string, replacement bool) (ModuleProvenance, error) {
	if len(p) > 1024 || len(v) > 256 || len(s) > 256 {
		return ModuleProvenance{}, fmt.Errorf("stage: oversized module identity")
	}
	// Local replacement paths can expose private builder directories. Preserve
	// the fact of replacement, never the local pathname.
	if (replacement && v == "") || path.IsAbs(p) || strings.HasPrefix(p, ".") || strings.ContainsAny(p, "\\:") {
		p = "<local-replacement>"
	}
	return ModuleProvenance{Path: p, Version: v, Sum: s}, nil
}
