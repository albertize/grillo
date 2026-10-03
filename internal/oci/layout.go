// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LayoutVersion is the OCI image layout version Grillo understands.
const LayoutVersion = "1.0.0"

// ImportLayout imports an OCI image layout directory (as produced by
// `podman save --format oci-dir`) into the CAS. Every referenced blob is
// verified against its descriptor digest and size before it becomes visible.
// When reference is non-empty it selects the index entry with a matching
// org.opencontainers.image.ref.name annotation; otherwise the sole entry or the
// entry matching platform is chosen.
func ImportLayout(dir, reference string, cas *CAS, platform Platform, maxBytes int64) (PulledImage, error) {
	layoutData, err := os.ReadFile(filepath.Join(dir, "oci-layout"))
	if err != nil {
		return PulledImage{}, fmt.Errorf("oci: not an OCI image layout: %w", err)
	}
	var layout struct {
		ImageLayoutVersion string `json:"imageLayoutVersion"`
	}
	if err := json.Unmarshal(layoutData, &layout); err != nil {
		return PulledImage{}, fmt.Errorf("oci: parse oci-layout: %w", err)
	}
	if layout.ImageLayoutVersion != LayoutVersion {
		return PulledImage{}, fmt.Errorf("oci: unsupported image layout version %q", layout.ImageLayoutVersion)
	}
	indexData, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		return PulledImage{}, fmt.Errorf("oci: read index: %w", err)
	}
	var index Index
	if err := json.Unmarshal(indexData, &index); err != nil {
		return PulledImage{}, fmt.Errorf("oci: parse index: %w", err)
	}
	descriptor, err := selectLayoutManifest(index, reference, platform)
	if err != nil {
		return PulledImage{}, err
	}

	manifestData, err := readLayoutBlob(dir, descriptor.Digest)
	if err != nil {
		return PulledImage{}, err
	}
	if err := cas.PutBytes(descriptor.Digest, manifestData); err != nil {
		return PulledImage{}, err
	}
	manifest, err := ParseManifest(manifestData)
	if err != nil {
		return PulledImage{}, err
	}
	if err := commitLayoutBlob(cas, dir, manifest.Config, maxBytes); err != nil {
		return PulledImage{}, fmt.Errorf("oci: config %s: %w", manifest.Config.Digest, err)
	}
	configData, err := readLayoutBlob(dir, manifest.Config.Digest)
	if err != nil {
		return PulledImage{}, err
	}
	config, err := ParseImageConfig(configData)
	if err != nil {
		return PulledImage{}, err
	}
	for i, layer := range manifest.Layers {
		if err := commitLayoutBlob(cas, dir, layer, maxBytes); err != nil {
			return PulledImage{}, fmt.Errorf("oci: layout layer %d (%s): %w", i, layer.Digest, err)
		}
	}
	ref := Reference{}
	if reference != "" {
		if parsed, err := ParseReference(reference); err == nil {
			ref = parsed
		}
	}
	return PulledImage{
		Reference:      ref,
		Manifest:       manifest,
		ManifestDigest: descriptor.Digest,
		Config:         config,
	}, nil
}

func selectLayoutManifest(index Index, reference string, platform Platform) (Descriptor, error) {
	if len(index.Manifests) == 0 {
		return Descriptor{}, fmt.Errorf("oci: image layout index has no manifests")
	}
	refName := "org.opencontainers.image.ref.name"
	if reference != "" {
		for _, descriptor := range index.Manifests {
			name := descriptor.Annotations[refName]
			if refNameMatch(name, reference) {
				return descriptor, nil
			}
		}
	}
	if len(index.Manifests) == 1 {
		return index.Manifests[0], nil
	}
	if platform.OS != "" {
		if descriptor, err := SelectPlatform(index, platform); err == nil {
			return descriptor, nil
		}
	}
	return index.Manifests[0], nil
}

// refNameMatch compares a stored ref-name annotation with a reference,
// tolerating the localhost/ prefix Podman adds for locally built images.
func refNameMatch(name, reference string) bool {
	return name == reference ||
		strings.TrimPrefix(name, "localhost/") == reference ||
		strings.TrimPrefix(reference, "localhost/") == name
}

func readLayoutBlob(dir, digest string) ([]byte, error) {
	path, err := layoutBlobPath(dir, digest)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("oci: read blob %s: %w", digest, err)
	}
	return data, nil
}

func commitLayoutBlob(cas *CAS, dir string, descriptor Descriptor, maxBytes int64) error {
	path, err := layoutBlobPath(dir, descriptor.Digest)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("oci: read blob %s: %w", descriptor.Digest, err)
	}
	defer f.Close()
	return cas.Commit(descriptor.Digest, f, descriptor.Size, maxBytes)
}

func layoutBlobPath(dir, digest string) (string, error) {
	hexPart, err := digestHex(digest)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "blobs", "sha256", hexPart), nil
}
