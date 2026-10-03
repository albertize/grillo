// SPDX-License-Identifier: Apache-2.0

package model

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestModelImportBoundary enforces the architectural rule that the IR model
// imports no VMM, network, process, HTTP, or frontend code: only the standard
// library and the dependency-free source package are allowed.
func TestModelImportBoundary(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			for _, spec := range file.Imports {
				path := strings.Trim(spec.Path.Value, `"`)
				if !allowedModelImport(path) {
					t.Errorf("%s imports forbidden package %q", name, path)
				}
			}
		}
	}
}

func allowedModelImport(path string) bool {
	if path == "grillo.local/grillo/internal/source" {
		return true
	}
	first := path
	if i := strings.IndexByte(path, '/'); i >= 0 {
		first = path[:i]
	}
	// Standard library packages have no dot in their first path element.
	return !strings.Contains(first, ".")
}
