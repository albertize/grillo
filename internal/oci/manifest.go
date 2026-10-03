// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"encoding/json"
	"fmt"
)

// Media types supported by the distribution and image handling code.
const (
	MediaTypeOCIManifest    = "application/vnd.oci.image.manifest.v1+json"
	MediaTypeOCIIndex       = "application/vnd.oci.image.index.v1+json"
	MediaTypeDockerManifest = "application/vnd.docker.distribution.manifest.v2+json"
	MediaTypeDockerList     = "application/vnd.docker.distribution.manifest.list.v2+json"
	MediaTypeDockerSchema1  = "application/vnd.docker.distribution.manifest.v1+json"

	MediaTypeOCIConfig    = "application/vnd.oci.image.config.v1+json"
	MediaTypeDockerConfig = "application/vnd.docker.container.image.v1+json"

	MediaTypeOCILayerGzip    = "application/vnd.oci.image.layer.v1.tar+gzip"
	MediaTypeOCILayer        = "application/vnd.oci.image.layer.v1.tar"
	MediaTypeDockerLayerGzip = "application/vnd.docker.image.rootfs.diff.tar.gzip"
	MediaTypeDockerLayer     = "application/vnd.docker.image.rootfs.diff.tar"
	MediaTypeOCILayerZstd    = "application/vnd.oci.image.layer.v1.tar+zstd"
)

// Platform identifies an image target.
type Platform struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Variant      string `json:"variant,omitempty"`
}

// Descriptor references content by digest.
type Descriptor struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Platform    *Platform         `json:"platform,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// Manifest is an OCI or Docker schema2 image manifest.
type Manifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType,omitempty"`
	ArtifactType  string            `json:"artifactType,omitempty"`
	Config        Descriptor        `json:"config"`
	Layers        []Descriptor      `json:"layers"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

// Index is a manifest list / OCI index.
type Index struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType,omitempty"`
	Manifests     []Descriptor      `json:"manifests"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

type manifestHeader struct {
	SchemaVersion int    `json:"schemaVersion"`
	MediaType     string `json:"mediaType"`
}

// IsIndex reports whether a media type is a manifest list / index.
func IsIndex(mediaType string) bool {
	return mediaType == MediaTypeOCIIndex || mediaType == MediaTypeDockerList
}

// IsLayerSupported reports whether a layer media type can be unpacked. zstd is
// recognized so it can be rejected with a clear error instead of a parse failure.
func IsLayerSupported(mediaType string) bool {
	switch mediaType {
	case MediaTypeOCILayerGzip, MediaTypeOCILayer, MediaTypeDockerLayerGzip, MediaTypeDockerLayer:
		return true
	default:
		return false
	}
}

// IsGzipLayer reports whether a layer is gzip-compressed.
func IsGzipLayer(mediaType string) bool {
	return mediaType == MediaTypeOCILayerGzip || mediaType == MediaTypeDockerLayerGzip
}

// ParseManifest parses a schema2/OCI image manifest, rejecting schema1.
func ParseManifest(data []byte) (Manifest, error) {
	var header manifestHeader
	if err := json.Unmarshal(data, &header); err != nil {
		return Manifest{}, fmt.Errorf("oci: decode manifest header: %w", err)
	}
	if header.SchemaVersion == 1 || header.MediaType == MediaTypeDockerSchema1 {
		return Manifest{}, fmt.Errorf("oci: schema1 manifests are not supported")
	}
	if header.SchemaVersion != 2 {
		return Manifest{}, fmt.Errorf("oci: unsupported manifest schemaVersion %d", header.SchemaVersion)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("oci: decode manifest: %w", err)
	}
	if !digestRe.MatchString(manifest.Config.Digest) || manifest.Config.Size < 0 {
		return Manifest{}, fmt.Errorf("oci: manifest has an invalid config descriptor")
	}
	for i, layer := range manifest.Layers {
		if !digestRe.MatchString(layer.Digest) {
			return Manifest{}, fmt.Errorf("oci: layer %d has an invalid digest", i)
		}
		if layer.Size < 0 {
			return Manifest{}, fmt.Errorf("oci: layer %d has a negative size", i)
		}
		if layer.MediaType == MediaTypeOCILayerZstd {
			return Manifest{}, fmt.Errorf("oci: zstd layers are not supported")
		}
		if !IsLayerSupported(layer.MediaType) {
			return Manifest{}, fmt.Errorf("oci: unsupported layer media type %q", layer.MediaType)
		}
	}
	return manifest, nil
}

// ParseIndex parses a manifest list / OCI index.
func ParseIndex(data []byte) (Index, error) {
	var index Index
	if err := json.Unmarshal(data, &index); err != nil {
		return Index{}, fmt.Errorf("oci: decode index: %w", err)
	}
	if index.SchemaVersion == 1 {
		return Index{}, fmt.Errorf("oci: schema1 manifests are not supported")
	}
	for i, descriptor := range index.Manifests {
		if !digestRe.MatchString(descriptor.Digest) {
			return Index{}, fmt.Errorf("oci: index entry %d has an invalid digest", i)
		}
	}
	return index, nil
}

// SelectPlatform returns the index entry for the requested platform.
func SelectPlatform(index Index, platform Platform) (Descriptor, error) {
	for _, descriptor := range index.Manifests {
		if descriptor.Platform == nil {
			continue
		}
		if descriptor.Platform.OS != platform.OS || descriptor.Platform.Architecture != platform.Architecture {
			continue
		}
		if platform.Variant != "" && descriptor.Platform.Variant != platform.Variant {
			continue
		}
		return descriptor, nil
	}
	return Descriptor{}, fmt.Errorf("oci: no manifest for platform %s/%s", platform.OS, platform.Architecture)
}
