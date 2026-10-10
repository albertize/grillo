// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestUnpackFiniteDefaultsAndInvalidLimits(t *testing.T) {
	budget, err := newUnpackBudget(UnpackOptions{})
	if err != nil || budget.opts.MaxBytes != DefaultMaxUnpackBytes || budget.opts.MaxFiles != DefaultMaxUnpackFiles || budget.opts.MaxArchiveBytes <= 0 {
		t.Fatal("unbounded defaults", err)
	}
	for _, opts := range []UnpackOptions{{MaxBytes: -1}, {MaxFiles: -1}, {MaxArchiveBytes: -1}} {
		if err := UnpackLayer(t.TempDir(), bytes.NewReader(nil), false, opts); err == nil {
			t.Fatal("negative quota accepted")
		}
	}
}

func TestUnpackDefaultRejectsOversizedHeaderWithoutAllocatingPayload(t *testing.T) {
	var layer bytes.Buffer
	writer := tar.NewWriter(&layer)
	if err := writer.WriteHeader(&tar.Header{Name: "too-large", Typeflag: tar.TypeReg, Mode: 0600, Size: DefaultMaxUnpackBytes + 1}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := UnpackLayer(root, bytes.NewReader(layer.Bytes()), false, UnpackOptions{}); !errors.Is(err, ErrUnpackLimit) {
		t.Fatal("default quota not applied", err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatal("oversized payload file created")
	}
}

func TestUnpackWhiteoutsConsumeEntryBudgetBeforeRemoval(t *testing.T) {
	root := t.TempDir()
	layer := buildTar(t, tarEntry{name: "keep", typeflag: tar.TypeReg, mode: 0600, body: []byte("x")}, tarEntry{name: ".wh.keep", typeflag: tar.TypeReg, mode: 0600})
	if err := UnpackLayer(root, bytes.NewReader(layer), false, UnpackOptions{MaxFiles: 1}); !errors.Is(err, ErrUnpackLimit) {
		t.Fatal("whiteout bypassed quota", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "keep")); err != nil || string(data) != "x" {
		t.Fatal("over-quota whiteout deleted content", err)
	}
}

func TestUnpackArchiveQuotaAndChecksum(t *testing.T) {
	layer := buildTar(t, tarEntry{name: "file", typeflag: tar.TypeReg, mode: 0600, body: []byte("abc")})
	for _, compressed := range []bool{false, true} {
		input := layer
		if compressed {
			input = gzipBytes(t, layer)
		}
		if err := UnpackLayer(t.TempDir(), bytes.NewReader(input), compressed, UnpackOptions{MaxArchiveBytes: int64(len(layer))}); err != nil {
			t.Fatal("exact boundary rejected", err)
		}
		if err := UnpackLayer(t.TempDir(), bytes.NewReader(input), compressed, UnpackOptions{MaxArchiveBytes: int64(len(layer) - 1)}); !errors.Is(err, ErrUnpackLimit) {
			t.Fatal("archive quota not enforced", err)
		}
	}
	// Tar finishes before gzip's footer: extraction must still validate CRC.
	compressed := gzipBytes(t, layer)
	compressed[len(compressed)-8] ^= 1
	if err := UnpackLayer(t.TempDir(), bytes.NewReader(compressed), true, UnpackOptions{}); err == nil {
		t.Fatal("corrupt gzip footer ignored")
	}
	trailing := append(bytes.Clone(layer), bytes.Repeat([]byte{0}, 4096)...)
	if err := UnpackLayer(t.TempDir(), bytes.NewReader(gzipBytes(t, trailing)), true, UnpackOptions{MaxArchiveBytes: int64(len(layer))}); !errors.Is(err, ErrUnpackLimit) {
		t.Fatal("trailing expansion bypassed quota", err)
	}
}

func quotaTestImage(t *testing.T, layers ...[]byte) (*Puller, PulledImage) {
	t.Helper()
	cas, err := OpenCAS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	image := PulledImage{}
	for _, layer := range layers {
		compressed := gzipBytes(t, layer)
		digest := "sha256:" + sha256Hex(compressed)
		if err := cas.PutBytes(digest, compressed); err != nil {
			t.Fatal(err)
		}
		image.Manifest.Layers = append(image.Manifest.Layers, Descriptor{MediaType: MediaTypeOCILayerGzip, Digest: digest, Size: int64(len(compressed))})
		image.Config.RootFS.DiffIDs = append(image.Config.RootFS.DiffIDs, "sha256:"+sha256Hex(layer))
	}
	return &Puller{CAS: cas}, image
}

func TestUnpackImageQuotasAreSharedAcrossLayers(t *testing.T) {
	first := buildTar(t, tarEntry{name: "one", typeflag: tar.TypeReg, mode: 0600, body: []byte("abc")})
	second := buildTar(t, tarEntry{name: "two", typeflag: tar.TypeReg, mode: 0600, body: []byte("def")})
	puller, image := quotaTestImage(t, first, second)
	for _, opts := range []UnpackOptions{{MaxBytes: 5}, {MaxFiles: 1}, {MaxArchiveBytes: int64(len(first) + len(second) - 1)}} {
		root := t.TempDir()
		if err := puller.Unpack(image, root, opts); !errors.Is(err, ErrUnpackLimit) {
			t.Fatal("layers reset quota", err)
		}
		if _, err := os.Stat(filepath.Join(root, "two")); !os.IsNotExist(err) {
			t.Fatal("over-quota second layer wrote content")
		}
	}
	if err := puller.Unpack(image, t.TempDir(), UnpackOptions{MaxBytes: 6, MaxFiles: 2, MaxArchiveBytes: int64(len(first) + len(second))}); err != nil {
		t.Fatal("exact image quota rejected", err)
	}
	// The pre-extraction diff_id pass is bounded too, even without tar parsing.
	if err := puller.Unpack(image, t.TempDir(), UnpackOptions{MaxArchiveBytes: 1}); !errors.Is(err, ErrUnpackLimit) {
		t.Fatal("diff_id expansion unbounded", err)
	}
}

type cancelUnpackReader struct {
	reader io.Reader
	cancel context.CancelFunc
}

func (r *cancelUnpackReader) Read(data []byte) (int, error) {
	n, err := r.reader.Read(data)
	r.cancel()
	return n, err
}
func TestUnpackCancellationAndOverwriteAccounting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	layer := buildTar(t, tarEntry{name: "file", typeflag: tar.TypeReg, mode: 0600, body: []byte("abc")})
	root := t.TempDir()
	if err := UnpackLayer(root, &cancelUnpackReader{bytes.NewReader(layer), cancel}, false, UnpackOptions{Context: ctx}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatal("canceled extraction wrote files")
	}
	repeated := buildTar(t, tarEntry{name: "file", typeflag: tar.TypeReg, mode: 0600, body: []byte("abc")}, tarEntry{name: "file", typeflag: tar.TypeReg, mode: 0600, body: []byte("def")})
	if err := UnpackLayer(root, bytes.NewReader(repeated), false, UnpackOptions{MaxBytes: 5}); !errors.Is(err, ErrUnpackLimit) {
		t.Fatal("overwrite bypassed cumulative quota", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "file")); err != nil || string(data) != "abc" {
		t.Fatal("over-quota overwrite mutated content", err)
	}
}
