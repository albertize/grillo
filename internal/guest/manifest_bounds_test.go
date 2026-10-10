// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestRejectsEmptyUnknownTrailingAndOversized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest")
	for _, data := range []string{`{"artifacts":[]}`, `{"artifacts":[],"unknown":true}`, `{"artifacts":[]} {}`, `{"artifacts":[],"artifacts":[]}`, `{"artifacts":[],"schema_version":1,"schema_version":0}`, `{"artifacts":[],"unknown":` + strings.Repeat("[", 10) + `0` + strings.Repeat("]", 10) + `}`, strings.Repeat(" ", (1<<20)+1)} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadManifest(path); err == nil {
			t.Fatal("invalid manifest accepted")
		}
	}
}
func TestManifestRejectsDuplicateArtifactsAndInvalidDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kernel")
	if err := os.WriteFile(path, []byte("kernel"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec := ArtifactSpec{Name: "kernel", Version: "fixture", Path: path}
	if _, err := BuildManifest([]ArtifactSpec{spec, spec}); err == nil {
		t.Fatal("duplicate artifacts accepted")
	}
	m, err := BuildManifest([]ArtifactSpec{spec})
	if err != nil {
		t.Fatal(err)
	}
	m.Artifacts[0].SHA256 = strings.Repeat("g", 64)
	if err := m.Verify(); err == nil {
		t.Fatal("invalid digest accepted")
	}
}
