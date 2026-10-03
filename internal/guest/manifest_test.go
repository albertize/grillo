// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManifestDeterministicAndVerifiable(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "agent")
	b := filepath.Join(dir, "runc")
	if err := os.WriteFile(a, []byte("agent-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("runc-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	specs := []ArtifactSpec{
		{Name: "runc", Version: "1.5.2", Path: b},
		{Name: "grillo-agent", Version: "0.1.0", Path: a},
	}
	manifest, err := BuildManifest(specs)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Artifacts) != 2 || manifest.Artifacts[0].Name != "grillo-agent" {
		t.Fatalf("manifest not sorted by name: %+v", manifest.Artifacts)
	}
	if manifest.Artifacts[0].SHA256 == "" || manifest.Artifacts[0].Size != int64(len("agent-binary")) {
		t.Fatalf("bad digest: %+v", manifest.Artifacts[0])
	}
	out := filepath.Join(dir, "manifest.json")
	if err := manifest.Write(out); err != nil {
		t.Fatal(err)
	}
	loaded, err := ReadManifest(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.Verify(); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := os.WriteFile(a, []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := loaded.Verify(); err == nil {
		t.Fatal("expected tamper detection")
	}
}

func TestBuildManifestRejectsUnnamed(t *testing.T) {
	if _, err := BuildManifest([]ArtifactSpec{{Path: "/nonexistent"}}); err == nil {
		t.Fatal("expected error for unnamed artifact")
	}
}
