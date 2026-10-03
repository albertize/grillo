// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type layoutFixture struct {
	dir            string
	manifestDigest string
	configDigest   string
	layerDigest    string
	layerPlain     []byte
}

func writeLayoutFixture(t *testing.T, corruptLayer bool) layoutFixture {
	t.Helper()
	dir := t.TempDir()
	blobs := filepath.Join(dir, "blobs", "sha256")
	if err := os.MkdirAll(blobs, 0o755); err != nil {
		t.Fatal(err)
	}

	// Layer: a tiny tar archive, gzip-compressed.
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	fileBody := []byte("grillo-layer")
	if err := tw.WriteHeader(&tar.Header{Name: "hello.txt", Mode: 0o644, Size: int64(len(fileBody))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(fileBody); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	layerPlain := tarBuf.Bytes()
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(layerPlain); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	layerData := gz.Bytes()

	diffID := "sha256:" + sha256HexTest(layerPlain)
	config := map[string]any{
		"architecture": "amd64",
		"os":           "linux",
		"rootfs":       map[string]any{"type": "layers", "diff_ids": []string{diffID}},
	}
	configData, _ := json.Marshal(config)
	configDigest := "sha256:" + sha256HexTest(configData)

	layerDigest := "sha256:" + sha256HexTest(layerData)
	manifest := map[string]any{
		"schemaVersion": 2,
		"mediaType":     MediaTypeOCIManifest,
		"config":        map[string]any{"mediaType": MediaTypeOCIConfig, "digest": configDigest, "size": len(configData)},
		"layers":        []map[string]any{{"mediaType": MediaTypeOCILayerGzip, "digest": layerDigest, "size": len(layerData)}},
	}
	manifestData, _ := json.Marshal(manifest)
	manifestDigest := "sha256:" + sha256HexTest(manifestData)

	writeBlob := func(digest string, data []byte) {
		name := digest[len("sha256:"):]
		if corruptLayer && digest == layerDigest {
			data = append([]byte("corrupt"), data...)
		}
		if err := os.WriteFile(filepath.Join(blobs, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeBlob(configDigest, configData)
	writeBlob(layerDigest, layerData)
	writeBlob(manifestDigest, manifestData)

	index := map[string]any{
		"schemaVersion": 2,
		"mediaType":     MediaTypeOCIIndex,
		"manifests": []map[string]any{{
			"mediaType":   MediaTypeOCIManifest,
			"digest":      manifestDigest,
			"size":        len(manifestData),
			"annotations": map[string]string{"org.opencontainers.image.ref.name": "localhost/grillo-test:local"},
		}},
	}
	indexData, _ := json.Marshal(index)
	if err := os.WriteFile(filepath.Join(dir, "index.json"), indexData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "oci-layout"), []byte(`{"imageLayoutVersion":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return layoutFixture{dir: dir, manifestDigest: manifestDigest, configDigest: configDigest, layerDigest: layerDigest, layerPlain: layerPlain}
}

func TestImportLayout(t *testing.T) {
	fixture := writeLayoutFixture(t, false)
	cas, err := OpenCAS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pulled, err := ImportLayout(fixture.dir, "grillo-test:local", cas, Platform{OS: "linux", Architecture: "amd64"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if pulled.ManifestDigest != fixture.manifestDigest {
		t.Fatalf("manifest = %s want %s", pulled.ManifestDigest, fixture.manifestDigest)
	}
	if len(pulled.Manifest.Layers) != 1 || pulled.Manifest.Layers[0].Digest != fixture.layerDigest {
		t.Fatalf("layers = %+v", pulled.Manifest.Layers)
	}
	for _, digest := range []string{fixture.manifestDigest, fixture.configDigest, fixture.layerDigest} {
		if !cas.Has(digest) {
			t.Fatalf("blob %s missing after import", digest)
		}
	}
	// The importer honours the configured digest and unpack verifies diff_ids.
	puller := &Puller{CAS: cas}
	root := t.TempDir()
	if err := puller.Unpack(pulled, root, UnpackOptions{}); err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "hello.txt")); err != nil || string(data) != "grillo-layer" {
		t.Fatalf("unpacked file = %q err=%v", data, err)
	}
}

func TestImportLayoutRejectsCorruption(t *testing.T) {
	fixture := writeLayoutFixture(t, true)
	cas, err := OpenCAS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ImportLayout(fixture.dir, "", cas, Platform{}, 0); err == nil {
		t.Fatal("corrupt blob was accepted")
	}
}

func TestImportLayoutRejectsMissingLayout(t *testing.T) {
	cas, err := OpenCAS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ImportLayout(t.TempDir(), "", cas, Platform{}, 0); err == nil {
		t.Fatal("directory without oci-layout was accepted")
	}
}

func sha256HexTest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
