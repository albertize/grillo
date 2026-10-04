// SPDX-License-Identifier: Apache-2.0

package image

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/albertize/grillo/internal/oci"
)

func putBlob(t *testing.T, cas *oci.CAS, data []byte) oci.Descriptor {
	t.Helper()
	sum := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if err := cas.PutBytes(digest, data); err != nil {
		t.Fatal(err)
	}
	return oci.Descriptor{Digest: digest, Size: int64(len(data))}
}

func blobsOf(img oci.PulledImage) []string {
	out := []string{img.ManifestDigest, img.Manifest.Config.Digest}
	for _, layer := range img.Manifest.Layers {
		out = append(out, layer.Digest)
	}
	return out
}

func fakeImage(t *testing.T, cas *oci.CAS, tag string) oci.PulledImage {
	t.Helper()
	config := putBlob(t, cas, []byte("config-"+tag))
	layer := putBlob(t, cas, []byte("layer-"+tag))
	manifest := putBlob(t, cas, []byte("manifest-"+tag))
	return oci.PulledImage{
		ManifestDigest: manifest.Digest,
		Manifest: oci.Manifest{
			Config: config,
			Layers: []oci.Descriptor{layer},
		},
	}
}

func TestStoreImportListPin(t *testing.T) {
	cas, err := oci.OpenCAS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(t.TempDir(), "images"), cas)
	if err != nil {
		t.Fatal(err)
	}
	first := fakeImage(t, cas, "one")
	record, err := store.Import(first, Meta{Reference: "example/one:latest", Source: SourcePull})
	if err != nil {
		t.Fatal(err)
	}
	if record.Source != SourcePull || record.LayerCount != 1 || len(record.Blobs) != 3 {
		t.Fatalf("record = %+v", record)
	}
	if ok, err := store.Verify("example/one:latest"); err != nil || !ok {
		t.Fatalf("verify = %v %v", ok, err)
	}
	if _, err := store.Import(fakeImage(t, cas, "two"), Meta{Reference: "example/two:v2", Source: SourceBuild}); err != nil {
		t.Fatal(err)
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Reference != "example/one:latest" || list[1].Reference != "example/two:v2" {
		t.Fatalf("list = %+v", list)
	}
	if err := store.Pin("example/one:latest", true); err != nil {
		t.Fatal(err)
	}
	pinned, _, _ := store.Get("example/one:latest")
	if !pinned.Pinned {
		t.Fatal("record not pinned")
	}
}

func TestStoreImportRejectsMissingBlob(t *testing.T) {
	cas, err := oci.OpenCAS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(t.TempDir(), "images"), cas)
	if err != nil {
		t.Fatal(err)
	}
	image := fakeImage(t, cas, "gap")
	// Drop a layer blob so the record would dangle.
	layer := image.Manifest.Layers[0].Digest
	path, err := cas.Path(layer)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Import(image, Meta{Reference: "example/gap:latest"}); err == nil {
		t.Fatal("import accepted a missing blob")
	}
}

func TestPrunePreservesActiveAndPinned(t *testing.T) {
	cas, err := oci.OpenCAS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(t.TempDir(), "images"), cas)
	if err != nil {
		t.Fatal(err)
	}
	active := fakeImage(t, cas, "active")
	if _, err := store.Import(active, Meta{Reference: "app/active:latest", Source: SourcePull}); err != nil {
		t.Fatal(err)
	}
	pinned := fakeImage(t, cas, "pinned")
	if _, err := store.Import(pinned, Meta{Reference: "app/pinned:latest", Source: SourcePull, Pinned: true}); err != nil {
		t.Fatal(err)
	}
	stale := fakeImage(t, cas, "stale")
	if _, err := store.Import(stale, Meta{Reference: "app/stale:latest", Source: SourceBuild}); err != nil {
		t.Fatal(err)
	}

	result, err := store.Prune(context.Background(), func(record Record) bool {
		return record.Reference == "app/active:latest"
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.RemovedRecords) != 1 || result.RemovedRecords[0].Reference != "app/stale:latest" {
		t.Fatalf("removed = %+v", result.RemovedRecords)
	}
	if result.RemovedBlobs == 0 {
		t.Fatal("no blobs were collected")
	}
	// Active and pinned content is intact.
	for _, digest := range blobsOf(active) {
		if !cas.Has(digest) {
			t.Fatalf("active blob %s was collected", digest)
		}
	}
	for _, digest := range blobsOf(pinned) {
		if !cas.Has(digest) {
			t.Fatalf("pinned blob %s was collected", digest)
		}
	}
	for _, digest := range stale.Manifest.Layers {
		if cas.Has(digest.Digest) {
			t.Fatalf("stale blob %s survived prune", digest.Digest)
		}
	}
	if _, ok, _ := store.Get("app/stale:latest"); ok {
		t.Fatal("stale record survived prune")
	}
	if ok, _ := store.Verify("app/active:latest"); !ok {
		t.Fatal("active record no longer verifies")
	}
}

func TestPinDigestProtectsStandaloneBlob(t *testing.T) {
	cas, err := oci.OpenCAS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(t.TempDir(), "images"), cas)
	if err != nil {
		t.Fatal(err)
	}
	orphan := putBlob(t, cas, []byte("orphan-blob"))
	if err := store.PinDigest(orphan.Digest, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Prune(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !cas.Has(orphan.Digest) {
		t.Fatal("pinned digest was collected")
	}
	if err := store.PinDigest(orphan.Digest, false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Prune(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if cas.Has(orphan.Digest) {
		t.Fatal("unpinned digest survived prune")
	}
}
