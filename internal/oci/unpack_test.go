// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

type tarEntry struct {
	name     string
	typeflag byte
	mode     int64
	uid      int
	gid      int
	body     []byte
	link     string
}

func buildTar(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.name,
			Typeflag: e.typeflag,
			Mode:     e.mode,
			Uid:      e.uid,
			Gid:      e.gid,
			Size:     int64(len(e.body)),
			Linkname: e.link,
		}
		if e.typeflag == tar.TypeDir {
			hdr.Mode |= 0o40000
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if len(e.body) > 0 {
			if _, err := tw.Write(e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestUnpackBasic(t *testing.T) {
	root := t.TempDir()
	layer := buildTar(t,
		tarEntry{name: "etc", typeflag: tar.TypeDir, mode: 0o755},
		tarEntry{name: "etc/app.conf", typeflag: tar.TypeReg, mode: 0o640, body: []byte("key=value\n")},
	)
	if err := UnpackLayer(root, bytes.NewReader(layer), false, UnpackOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "etc/app.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "key=value\n" {
		t.Fatalf("content = %q", data)
	}
	info, _ := os.Stat(filepath.Join(root, "etc/app.conf"))
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
}

func TestUnpackGzip(t *testing.T) {
	root := t.TempDir()
	layer := gzipBytes(t, buildTar(t, tarEntry{name: "bin/tool", typeflag: tar.TypeReg, mode: 0o755, body: []byte("#!/bin/sh\n")}))
	if err := UnpackLayer(root, bytes.NewReader(layer), true, UnpackOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "bin/tool")); err != nil {
		t.Fatal(err)
	}
}

func TestUnpackWhiteout(t *testing.T) {
	root := t.TempDir()
	base := buildTar(t,
		tarEntry{name: "etc", typeflag: tar.TypeDir, mode: 0o755},
		tarEntry{name: "etc/keep", typeflag: tar.TypeReg, mode: 0o644, body: []byte("keep")},
		tarEntry{name: "etc/remove", typeflag: tar.TypeReg, mode: 0o644, body: []byte("remove")},
	)
	if err := UnpackLayer(root, bytes.NewReader(base), false, UnpackOptions{}); err != nil {
		t.Fatal(err)
	}
	overlay := buildTar(t, tarEntry{name: "etc/.wh.remove", typeflag: tar.TypeReg, mode: 0o644})
	if err := UnpackLayer(root, bytes.NewReader(overlay), false, UnpackOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "etc/remove")); !os.IsNotExist(err) {
		t.Fatalf("whiteout did not remove file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "etc/keep")); err != nil {
		t.Fatalf("whiteout removed the wrong file: %v", err)
	}
}

func TestUnpackOpaqueDirectory(t *testing.T) {
	root := t.TempDir()
	base := buildTar(t,
		tarEntry{name: "dir", typeflag: tar.TypeDir, mode: 0o755},
		tarEntry{name: "dir/a", typeflag: tar.TypeReg, mode: 0o644, body: []byte("a")},
		tarEntry{name: "dir/b", typeflag: tar.TypeReg, mode: 0o644, body: []byte("b")},
	)
	if err := UnpackLayer(root, bytes.NewReader(base), false, UnpackOptions{}); err != nil {
		t.Fatal(err)
	}
	overlay := buildTar(t,
		tarEntry{name: "dir/.wh..wh..opq", typeflag: tar.TypeReg, mode: 0o644},
		tarEntry{name: "dir/c", typeflag: tar.TypeReg, mode: 0o644, body: []byte("c")},
	)
	if err := UnpackLayer(root, bytes.NewReader(overlay), false, UnpackOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "dir/a")); !os.IsNotExist(err) {
		t.Fatal("opaque directory did not remove a")
	}
	if _, err := os.Stat(filepath.Join(root, "dir/c")); err != nil {
		t.Fatal("opaque directory removed the new entry")
	}
}

func TestUnpackAdversarial(t *testing.T) {
	cases := map[string][]tarEntry{
		"absolute path": {{name: "/etc/passwd", typeflag: tar.TypeReg, mode: 0o644, body: []byte("x")}},
		"parent escape": {{name: "../escape", typeflag: tar.TypeReg, mode: 0o644, body: []byte("x")}},
		"device node":   {{name: "dev/null", typeflag: tar.TypeChar, mode: 0o666}},
		"fifo":          {{name: "run/pipe", typeflag: tar.TypeFifo, mode: 0o600}},
		"absolute hardlink": {
			{name: "evil", typeflag: tar.TypeLink, link: "/etc/passwd"},
		},
		"parent hardlink": {
			{name: "evil", typeflag: tar.TypeLink, link: "../../etc/passwd"},
		},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := UnpackLayer(root, bytes.NewReader(buildTar(t, entries...)), false, UnpackOptions{}); err == nil {
				t.Fatal("expected adversarial entry to be rejected")
			}
		})
	}
}

func TestUnpackRefusesSymlinkTraversal(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(t.TempDir(), "outside"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	// First layer plants a symlink pointing outside the root.
	plant := buildTar(t, tarEntry{name: "link", typeflag: tar.TypeSymlink, link: "/etc"})
	if err := UnpackLayer(root, bytes.NewReader(plant), false, UnpackOptions{}); err != nil {
		t.Fatal(err)
	}
	// A later entry that writes through the symlink must be refused.
	attack := buildTar(t, tarEntry{name: "link/evil", typeflag: tar.TypeReg, mode: 0o644, body: []byte("x")})
	if err := UnpackLayer(root, bytes.NewReader(attack), false, UnpackOptions{}); err == nil {
		t.Fatal("expected symlink traversal to be refused")
	}
}

func TestUnpackClearsSetuid(t *testing.T) {
	root := t.TempDir()
	layer := buildTar(t, tarEntry{name: "bin/setuid", typeflag: tar.TypeReg, mode: 0o4755, body: []byte("x")})
	if err := UnpackLayer(root, bytes.NewReader(layer), false, UnpackOptions{}); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(root, "bin/setuid"))
	if info.Mode()&os.ModeSetuid != 0 {
		t.Fatal("setuid bit was preserved")
	}
}

func TestDiffID(t *testing.T) {
	layer := buildTar(t, tarEntry{name: "x", typeflag: tar.TypeReg, mode: 0o644, body: []byte("x")})
	got, err := DiffID(bytes.NewReader(layer))
	if err != nil {
		t.Fatal(err)
	}
	if want := "sha256:" + sha256Hex(layer); got != want {
		t.Fatalf("DiffID = %s, want %s", got, want)
	}
}
