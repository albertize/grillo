// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func FuzzUnpackLayer(f *testing.F) {
	var seed bytes.Buffer
	tw := tar.NewWriter(&seed)
	if err := tw.WriteHeader(&tar.Header{Name: "hello", Mode: 0o600, Size: 2}); err != nil {
		f.Fatal(err)
	}
	if _, err := tw.Write([]byte("hi")); err != nil {
		f.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		f.Fatal(err)
	}
	f.Add(seed.Bytes(), false)
	f.Add([]byte{}, true)
	f.Fuzz(func(t *testing.T, data []byte, compressed bool) {
		if len(data) > 64<<10 {
			t.Skip()
		}
		parent := t.TempDir()
		root := filepath.Join(parent, "root")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		sentinel := filepath.Join(parent, "sentinel")
		if err := os.WriteFile(sentinel, []byte("unchanged"), 0o600); err != nil {
			t.Fatal(err)
		}
		_ = UnpackLayer(root, bytes.NewReader(data), compressed, UnpackOptions{MaxBytes: 64 << 10, MaxFiles: 64})
		got, err := os.ReadFile(sentinel)
		if err != nil || string(got) != "unchanged" {
			t.Fatal("extraction changed outside sentinel")
		}
		entries, err := os.ReadDir(parent)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 {
			t.Fatal("extraction created an outside entry")
		}
	})
}
