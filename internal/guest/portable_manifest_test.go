// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func portableFixture(t *testing.T) (string, Manifest) {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "guest assets")
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	var specs []ArtifactSpec
	for _, name := range []string{"kernel", "initramfs"} {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte("fixture-"+name), 0444); err != nil {
			t.Fatal(err)
		}
		specs = append(specs, ArtifactSpec{Name: name, Version: "fixture", Path: path})
	}
	m, err := BuildPortableManifest(directory, specs)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Write(filepath.Join(directory, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	return directory, m
}

func TestPortableManifestMovedReadOnlyAndCWDIndependent(t *testing.T) {
	directory, first := portableFixture(t)
	original, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(original, []byte(directory)) {
		t.Fatal("build-time path serialized")
	}
	specs := []ArtifactSpec{{Name: "initramfs", Version: "fixture", Path: filepath.Join(directory, "initramfs")}, {Name: "kernel", Version: "fixture", Path: filepath.Join(directory, "kernel")}}
	second, err := BuildPortableManifest(directory, specs)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if !bytes.Equal(a, b) {
		t.Fatal("input ordering changes inventory")
	}
	moved := filepath.Join(filepath.Dir(directory), "moved assets")
	if err := os.Rename(directory, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(moved, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(moved, 0755) })
	t.Chdir(t.TempDir())
	loaded, err := ReadBootManifest(filepath.Join(moved, "manifest.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.Verify(); err != nil {
		t.Fatal(err)
	}
	for _, a := range loaded.Artifacts {
		if a.HostPath() != filepath.Join(moved, a.Path) {
			t.Fatal("wrong directory anchor")
		}
	}
	after, _ := os.ReadFile(filepath.Join(moved, "manifest.json"))
	if !bytes.Equal(original, after) {
		t.Fatal("verification rewrote inventory")
	}
	var unanchored Manifest
	if err := json.Unmarshal(original, &unanchored); err != nil {
		t.Fatal(err)
	}
	if err := unanchored.Verify(); err == nil {
		t.Fatal("unanchored portable inventory used CWD")
	}
}

func TestPortableManifestInvalidMetadataAndPaths(t *testing.T) {
	directory, base := portableFixture(t)
	tests := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{"future-schema", func(m *Manifest) { m.SchemaVersion = 2 }},
		{"negative-schema", func(m *Manifest) { m.SchemaVersion = -1 }},
		{"missing-schema", func(m *Manifest) { m.SchemaVersion = 0 }},
		{"abi", func(m *Manifest) { m.GuestABI = "99.1" }},
		{"missing-abi", func(m *Manifest) { m.GuestABI = "" }},
		{"platform", func(m *Manifest) { m.Platform = "linux/arm64" }},
		{"missing-platform", func(m *Manifest) { m.Platform = "" }},
		{"missing-kernel", func(m *Manifest) { m.Artifacts = m.Artifacts[:1] }},
		{"extra-key", func(m *Manifest) { m.Artifacts[0].Name = "key" }},
		{"duplicate-path", func(m *Manifest) { m.Artifacts[0].Path = m.Artifacts[1].Path }},
		{"zero-size", func(m *Manifest) { m.Artifacts[0].Size = 0 }},
		{"oversize", func(m *Manifest) { m.Artifacts[0].Size = (2 << 30) + 1 }},
		{"absolute", func(m *Manifest) { m.Artifacts[0].Path = "/tmp/kernel" }},
		{"traversal", func(m *Manifest) { m.Artifacts[0].Path = "../kernel" }},
		{"nested-traversal", func(m *Manifest) { m.Artifacts[0].Path = "dir/../kernel" }},
		{"dot", func(m *Manifest) { m.Artifacts[0].Path = "./kernel" }},
		{"separator", func(m *Manifest) { m.Artifacts[0].Path = "dir//kernel" }},
		{"backslash", func(m *Manifest) { m.Artifacts[0].Path = `dir\kernel` }},
		{"drive", func(m *Manifest) { m.Artifacts[0].Path = "C:/kernel" }},
		{"nul", func(m *Manifest) { m.Artifacts[0].Path = "kernel\x00" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := base
			m.Artifacts = append([]Artifact(nil), base.Artifacts...)
			test.mutate(&m)
			data, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "invalid.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			for _, allow := range []bool{false, true} {
				if _, err := ReadBootManifest(path, allow); err == nil {
					t.Fatal("invalid inventory accepted", allow)
				}
			}
		})
	}
}

func TestPortableManifestSymlinksSpecialFilesAndTampering(t *testing.T) {
	for _, attack := range []string{"file-link", "parent-link", "directory-link", "fifo", "tamper", "size"} {
		t.Run(attack, func(t *testing.T) {
			directory, m := portableFixture(t)
			path := filepath.Join(directory, "kernel")
			switch attack {
			case "file-link":
				target := filepath.Join(directory, "target")
				if err := os.Rename(path, target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("target", path); err != nil {
					t.Fatal(err)
				}
			case "parent-link":
				outside := t.TempDir()
				if err := os.WriteFile(filepath.Join(outside, "kernel"), []byte("fixture-kernel"), 0444); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(directory, "link")); err != nil {
					t.Fatal(err)
				}
				for i := range m.Artifacts {
					if m.Artifacts[i].Name == "kernel" {
						m.Artifacts[i].Path = "link/kernel"
					}
				}
			case "directory-link":
				link := filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(directory, link); err != nil {
					t.Fatal(err)
				}
				directory = link
			case "fifo":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			case "tamper", "size":
				if err := os.Chmod(path, 0600); err != nil {
					t.Fatal(err)
				}
				data := []byte("mutated-kernel")
				if attack == "size" {
					data = append(data, 'x')
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.Write(filepath.Join(directory, "manifest.json")); err != nil {
				t.Fatal(err)
			}
			loaded, err := ReadBootManifest(filepath.Join(directory, "manifest.json"), false)
			if err == nil {
				err = loaded.Verify()
			}
			if err == nil {
				t.Fatal("unsafe artifact accepted")
			}
		})
	}
}

func TestInstalledManifestNeverFallsBackToLegacy(t *testing.T) {
	directory, _ := portableFixture(t)
	legacy, err := BuildManifest([]ArtifactSpec{{Name: "kernel", Version: "fixture", Path: filepath.Join(directory, "kernel")}, {Name: "initramfs", Version: "fixture", Path: filepath.Join(directory, "initramfs")}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "legacy.json")
	if err := legacy.Write(path); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBootManifest(path, false); err == nil {
		t.Fatal("installed legacy accepted")
	}
	if m, err := ReadBootManifest(path, true); err != nil {
		t.Fatal(err)
	} else if err := m.Verify(); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(directory, "fifo.json")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBootManifest(fifo, true); err == nil {
		t.Fatal("FIFO manifest accepted")
	}
}

func TestPortableBuilderRejectsOutsideMissingAndFixtureArtifacts(t *testing.T) {
	directory, _ := portableFixture(t)
	specs := []ArtifactSpec{{Name: "kernel", Version: "fixture", Path: filepath.Join(directory, "kernel")}, {Name: "initramfs", Version: "fixture", Path: filepath.Join(directory, "initramfs")}}
	for _, path := range []string{filepath.Join(filepath.Dir(directory), "outside"), filepath.Join(directory, "missing")} {
		changed := append([]ArtifactSpec(nil), specs...)
		changed[0].Path = path
		if _, err := BuildPortableManifest(directory, changed); err == nil {
			t.Fatal("unsafe input accepted", path)
		}
	}
	specs[0].Name = "key"
	if _, err := BuildPortableManifest(directory, specs); err == nil {
		t.Fatal("credential artifact accepted")
	}
	if _, err := BuildPortableManifest(directory, nil); err == nil {
		t.Fatal("empty inventory accepted")
	}
	// The inventory is not a secret scanner: an opaque image still needs review.
}

func TestPortableManifestRejectsDuplicateArtifactJSONFields(t *testing.T) {
	directory, m := portableFixture(t)
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), `"name":"kernel"`, `"name":"kernel","name":"kernel"`, 1))
	path := filepath.Join(directory, "duplicate.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBootManifest(path, false); err == nil {
		t.Fatal("duplicate artifact JSON field accepted")
	}
}
