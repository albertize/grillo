// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
)

// ArtifactSpec names a guest artifact to record.
type ArtifactSpec struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Path    string `json:"path"`
}

// Artifact records a built guest component with its verified digest.
type Artifact struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}

// Manifest is the guest artifact manifest written alongside an image.
type Manifest struct {
	Artifacts []Artifact `json:"artifacts"`
}

// HashFile returns the SHA-256 digest and size of a file.
func HashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

// BuildManifest hashes each artifact and returns a manifest sorted by name, so
// the same inputs always produce the same bytes.
func BuildManifest(specs []ArtifactSpec) (Manifest, error) {
	manifest := Manifest{Artifacts: make([]Artifact, 0, len(specs))}
	for _, spec := range specs {
		if spec.Name == "" {
			return Manifest{}, fmt.Errorf("guest: artifact has no name (%s)", spec.Path)
		}
		sum, size, err := HashFile(spec.Path)
		if err != nil {
			return Manifest{}, fmt.Errorf("guest: hash %s: %w", spec.Name, err)
		}
		manifest.Artifacts = append(manifest.Artifacts, Artifact{
			Name:    spec.Name,
			Version: spec.Version,
			Path:    spec.Path,
			SHA256:  sum,
			Size:    size,
		})
	}
	sort.SliceStable(manifest.Artifacts, func(i, j int) bool {
		return manifest.Artifacts[i].Name < manifest.Artifacts[j].Name
	})
	return manifest, nil
}

// Write writes the manifest as indented JSON.
func (m Manifest) Write(path string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// ReadManifest loads a manifest from disk.
func ReadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// Verify re-hashes every artifact and reports the first mismatch.
func (m Manifest) Verify() error {
	for _, a := range m.Artifacts {
		sum, size, err := HashFile(a.Path)
		if err != nil {
			return fmt.Errorf("guest: verify %s: %w", a.Name, err)
		}
		if sum != a.SHA256 || size != a.Size {
			return fmt.Errorf("guest: artifact %s changed (sha256 %s, want %s)", a.Name, sum, a.SHA256)
		}
	}
	return nil
}
