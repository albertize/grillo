// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"strings"
	"testing"
)

func TestParseManifestOCI(t *testing.T) {
	data := `{
		"schemaVersion": 2,
		"mediaType": "` + MediaTypeOCIManifest + `",
		"config": {"mediaType": "` + MediaTypeOCIConfig + `", "digest": "` + exampleDigest + `", "size": 100},
		"layers": [
			{"mediaType": "` + MediaTypeOCILayerGzip + `", "digest": "` + exampleDigest + `", "size": 200}
		]
	}`
	manifest, err := ParseManifest([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Config.Digest != exampleDigest || len(manifest.Layers) != 1 {
		t.Fatalf("manifest = %+v", manifest)
	}
}

func TestParseDockerSchema2Manifest(t *testing.T) {
	data := `{
		"schemaVersion": 2,
		"mediaType": "` + MediaTypeDockerManifest + `",
		"config": {"mediaType": "` + MediaTypeDockerConfig + `", "digest": "` + exampleDigest + `", "size": 10},
		"layers": [{"mediaType": "` + MediaTypeDockerLayerGzip + `", "digest": "` + exampleDigest + `", "size": 20}]
	}`
	manifest, err := ParseManifest([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Config.MediaType != MediaTypeDockerConfig || manifest.Layers[0].MediaType != MediaTypeDockerLayerGzip {
		t.Fatalf("manifest = %+v", manifest)
	}
	if !IsGzipLayer(manifest.Layers[0].MediaType) {
		t.Fatal("docker gzip layer not recognized")
	}
}

func TestParseManifestRejectsSchema1(t *testing.T) {
	if _, err := ParseManifest([]byte(`{"schemaVersion":1,"fsLayers":[]}`)); err == nil {
		t.Fatal("expected schema1 rejection")
	}
	if _, err := ParseManifest([]byte(`{"schemaVersion":2,"mediaType":"` + MediaTypeDockerSchema1 + `"}`)); err == nil {
		t.Fatal("expected schema1 media type rejection")
	}
}

func TestParseManifestRejectsZstd(t *testing.T) {
	data := `{"schemaVersion":2,"config":{"digest":"` + exampleDigest + `","size":1},"layers":[{"mediaType":"` + MediaTypeOCILayerZstd + `","digest":"` + exampleDigest + `","size":1}]}`
	_, err := ParseManifest([]byte(data))
	if err == nil || !strings.Contains(err.Error(), "zstd") {
		t.Fatalf("err = %v, want zstd rejection", err)
	}
}

func TestParseManifestRejectsUnsupportedLayer(t *testing.T) {
	data := `{"schemaVersion":2,"config":{"digest":"` + exampleDigest + `","size":1},"layers":[{"mediaType":"application/x-foo","digest":"` + exampleDigest + `","size":1}]}`
	if _, err := ParseManifest([]byte(data)); err == nil {
		t.Fatal("expected unsupported layer rejection")
	}
}

func TestParseManifestRejectsBadConfig(t *testing.T) {
	data := `{"schemaVersion":2,"config":{"digest":"sha256:short","size":1}}`
	if _, err := ParseManifest([]byte(data)); err == nil {
		t.Fatal("expected invalid config descriptor")
	}
}

func TestParseIndexAndSelectPlatform(t *testing.T) {
	data := `{
		"schemaVersion": 2,
		"mediaType": "` + MediaTypeOCIIndex + `",
		"manifests": [
			{"mediaType":"` + MediaTypeOCIManifest + `","digest":"` + exampleDigest + `","size":1,"platform":{"os":"linux","architecture":"arm64"}},
			{"mediaType":"` + MediaTypeOCIManifest + `","digest":"` + exampleDigest + `","size":1,"platform":{"os":"linux","architecture":"amd64"}}
		]
	}`
	index, err := ParseIndex([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := SelectPlatform(index, Platform{OS: "linux", Architecture: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.Platform.Architecture != "amd64" {
		t.Fatalf("selected = %+v", descriptor.Platform)
	}
	if _, err := SelectPlatform(index, Platform{OS: "linux", Architecture: "riscv64"}); err == nil {
		t.Fatal("expected no-platform error")
	}
}

func TestLayerMediaTypeHelpers(t *testing.T) {
	if !IsLayerSupported(MediaTypeDockerLayerGzip) || IsLayerSupported(MediaTypeOCILayerZstd) {
		t.Fatal("layer support helpers are wrong")
	}
	if !IsGzipLayer(MediaTypeOCILayerGzip) || IsGzipLayer(MediaTypeOCILayer) {
		t.Fatal("gzip helper is wrong")
	}
	if !IsIndex(MediaTypeDockerList) || IsIndex(MediaTypeOCIManifest) {
		t.Fatal("index helper is wrong")
	}
}
