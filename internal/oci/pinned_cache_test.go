// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"context"
	"encoding/json"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seededPinned(t *testing.T) (*CAS, Reference, testImage) {
	t.Helper()
	image := buildTestImage(t)
	cas, err := OpenCAS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range [][]byte{image.index, image.manifest, image.config, image.layer} {
		if err := cas.PutBytes("sha256:"+sha256Hex(b), b); err != nil {
			t.Fatal(err)
		}
	}
	ref, err := ParseReference("example.invalid/test@sha256:" + sha256Hex(image.index))
	if err != nil {
		t.Fatal(err)
	}
	return cas, ref, image
}

func TestPinnedCacheAndTransferWithoutRegistry(t *testing.T) {
	source, ref, image := seededPinned(t)
	destination, err := OpenCAS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got, err := CopyPinned(context.Background(), source, destination, ref, Platform{OS: "linux", Architecture: "amd64"})
	if err != nil || got.ManifestDigest != image.manifestDigest {
		t.Fatal(got, err)
	}
	p := &Puller{CAS: destination}
	got, err = p.Pull(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := p.Unpack(got, root, UnpackOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "bin/app")); err != nil {
		t.Fatal(err)
	}
	mutable := ref
	mutable.Digest = ""
	mutable.Tag = "latest"
	if _, err := p.Pull(context.Background(), mutable); !errors.Is(err, ErrNotFound) {
		t.Fatal("mutable tag used pinned cache", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Pull(canceled, ref); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestPinnedCacheRejectsCorruptionMissingAndSpecial(t *testing.T) {
	for _, kind := range []string{"index", "config", "layer", "missing", "symlink", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			cas, ref, image := seededPinned(t)
			digest := image.layerDigest
			if kind == "index" {
				digest = ref.Digest
			}
			if kind == "config" {
				digest = image.configDigest
			}
			path, _ := cas.Path(digest)
			if kind == "missing" {
				os.Remove(path)
			} else if kind == "symlink" || kind == "fifo" {
				os.Remove(path)
				if kind == "symlink" {
					if err := os.Symlink("/dev/null", path); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := unix.Mkfifo(path, 0600); err != nil {
						t.Fatal(err)
					}
				}
			} else if err := os.WriteFile(path, []byte(strings.Repeat("x", 10)), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := (&Puller{CAS: cas}).Pull(context.Background(), ref); err == nil {
				t.Fatal("bad pinned cache accepted")
			}
		})
	}
}

func TestPinnedMetadataBounds(t *testing.T) {
	cas, _, image := seededPinned(t)
	var manifest Manifest
	if err := json.Unmarshal(image.manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Config.Size = DefaultMaxManifestBytes + 1
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + sha256Hex(data)
	if err := cas.PutBytes(digest, data); err != nil {
		t.Fatal(err)
	}
	ref, err := ParseReference("example.invalid/test@" + digest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&Puller{CAS: cas}).Pull(context.Background(), ref); err == nil || !strings.Contains(err.Error(), "metadata") {
		t.Fatal("oversized config accepted", err)
	}
	if _, err := LoadPulled(cas, digest); err == nil || !strings.Contains(err.Error(), "metadata") {
		t.Fatal("local metadata path bypassed limit", err)
	}
}

func TestPullPersistsIndexThenWorksOffline(t *testing.T) {
	image := buildTestImage(t)
	registry := newTestRegistry(t, image, false)
	p := newPuller(t, registry)
	ref, err := ParseReference(registry.host(t) + "/library/test:latest")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Pull(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	ref.Tag = ""
	ref.Digest = "sha256:" + sha256Hex(image.index)
	p.Registry = nil
	if _, err := p.Pull(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	wrong := ref
	wrong.Digest = "sha256:" + strings.Repeat("a", 64)
	p.Registry = registry.client(t)
	if _, err := p.Pull(context.Background(), wrong); !errors.Is(err, ErrDigestMismatch) {
		t.Fatal("wrong advertised pin accepted", err)
	}
}
