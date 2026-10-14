//go:build linux

// SPDX-License-Identifier: Apache-2.0

package build

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/albertize/grillo/internal/guestproto"
)

func snapshotArchive(t *testing.T, headers ...*tar.Header) ([]byte, guestproto.ExportResult) {
	t.Helper()
	var output bytes.Buffer
	tw := tar.NewWriter(&output)
	for _, header := range headers {
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg && header.Size > 0 {
			if _, err := tw.Write(bytes.Repeat([]byte{'x'}, int(header.Size))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return snapshotReceipt(output.Bytes(), int64(len(headers)))
}

func snapshotReceipt(data []byte, entries int64) ([]byte, guestproto.ExportResult) {
	sum := sha256.Sum256(data)
	return data, guestproto.ExportResult{Bytes: int64(len(data)), Entries: entries, SHA256: hex.EncodeToString(sum[:])}
}

func openSnapshot(t *testing.T, data []byte) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "snapshot-")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestUnpackBuildExportPreservesDataAndNumericOwners(t *testing.T) {
	data, receipt := snapshotArchive(t,
		&tar.Header{Name: "dir/", Typeflag: tar.TypeDir, Mode: 0755, Uid: 1000, Gid: 1001},
		&tar.Header{Name: "dir/file", Typeflag: tar.TypeReg, Mode: 0640, Size: 4, Uid: 2000, Gid: 2001},
		&tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Mode: 0777, Linkname: "/container/path", Uid: 3000, Gid: 3001},
	)
	budget, _ := newWorkspaceBudget(context.Background(), 4, 10)
	scratch := t.TempDir()
	root, owners, err := unpackBuildExport(context.Background(), openSnapshot(t, data), scratch, receipt, budget)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, root, "dir/file"); got != "xxxx" {
		t.Fatal("payload changed")
	}
	if owners["dir/file"] != (fileOwner{2000, 2001}) || owners["link"] != (fileOwner{3000, 3001}) {
		t.Fatal("numeric ownership lost", owners)
	}
	if target, err := os.Readlink(filepath.Join(root, "link")); err != nil || target != "/container/path" {
		t.Fatal("symlink changed", err)
	}
	if info, err := os.Stat(root); err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("non-private export root", err)
	}
}

func TestUnpackBuildExportRejectsUnsafeOrIncompleteArchives(t *testing.T) {
	regular := func(name string) *tar.Header {
		return &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0600, Size: 4}
	}
	for _, scenario := range []string{"path", "duplicate", "whiteout", "hardlink", "fifo", "parent-link", "hash", "entries", "truncated", "trailing", "quota", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			headers := []*tar.Header{regular("file")}
			switch scenario {
			case "path":
				headers[0].Name = "../file"
			case "duplicate":
				headers = append(headers, regular("file"))
			case "whiteout":
				headers[0].Name = ".wh.file"
			case "hardlink":
				headers = []*tar.Header{{Name: "file", Typeflag: tar.TypeLink, Linkname: "other"}}
			case "fifo":
				headers = []*tar.Header{{Name: "file", Typeflag: tar.TypeFifo}}
			case "parent-link":
				headers = []*tar.Header{{Name: "parent", Typeflag: tar.TypeSymlink, Linkname: "/tmp"}, regular("parent/file")}
			}
			data, receipt := snapshotArchive(t, headers...)
			switch scenario {
			case "hash":
				receipt.SHA256 = "wrong"
			case "entries":
				receipt.Entries++
			case "truncated":
				data, receipt = snapshotReceipt(data[:len(data)-1024], receipt.Entries)
			case "trailing":
				data, receipt = snapshotReceipt(append(data, bytes.Repeat([]byte{'z'}, 512)...), receipt.Entries)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "canceled" {
				cancel()
			}
			bytesLimit := int64(10)
			if scenario == "quota" {
				bytesLimit = 3
			}
			budget, _ := newWorkspaceBudget(ctx, bytesLimit, 10)
			scratch := t.TempDir()
			root, _, err := unpackBuildExport(ctx, openSnapshot(t, data), scratch, receipt, budget)
			if err == nil || root != "" {
				t.Fatal("invalid export published", root, err)
			}
			if scenario == "quota" && !errors.Is(err, ErrWorkspaceLimit) {
				t.Fatal("quota error lost", err)
			}
			if entries, err := os.ReadDir(scratch); err != nil || len(entries) != 0 {
				t.Fatal("failed export left staging data", err)
			}
		})
	}
}

type fixtureSnapshotExporter struct {
	data    []byte
	receipt guestproto.ExportResult
	err     error
}

func (f fixtureSnapshotExporter) Export(_ context.Context, _ guestproto.ExportRequest, writer io.Writer) (guestproto.ExportResult, error) {
	if _, err := writer.Write(f.data); err != nil {
		return guestproto.ExportResult{}, err
	}
	return f.receipt, f.err
}

func TestBuildSnapshotReceiverBoundsSpoolAndExtractionTogether(t *testing.T) {
	data, receipt := snapshotArchive(t, &tar.Header{Name: "file", Typeflag: tar.TypeReg, Mode: 0600, Size: 4})
	for _, scenario := range []string{"exact", "spool-quota", "extract-quota", "failed-completion"} {
		t.Run(scenario, func(t *testing.T) {
			limit := receipt.Bytes + 4
			client := fixtureSnapshotExporter{data: data, receipt: receipt}
			switch scenario {
			case "spool-quota":
				limit = receipt.Bytes - 1
			case "extract-quota":
				limit = receipt.Bytes + 3
			case "failed-completion":
				client.err = io.ErrUnexpectedEOF
			}
			budget, _ := newWorkspaceBudget(context.Background(), limit, 10)
			scratch := t.TempDir()
			root, _, err := exportBuildSnapshot(context.Background(), client, "build", scratch, budget)
			if scenario == "exact" {
				if err != nil || readFile(t, root, "file") != "xxxx" {
					t.Fatal("valid snapshot", err)
				}
				entries, err := os.ReadDir(scratch)
				if err != nil || len(entries) != 1 || !entries[0].IsDir() {
					t.Fatal("archive retained after success", err)
				}
			} else {
				if err == nil || root != "" {
					t.Fatal("failed snapshot adopted", err)
				}
				if entries, err := os.ReadDir(scratch); err != nil || len(entries) != 0 {
					t.Fatal("failed snapshot left data", err)
				}
			}
		})
	}
}

func FuzzUnpackBuildExport(f *testing.F) {
	var seed bytes.Buffer
	tw := tar.NewWriter(&seed)
	_ = tw.WriteHeader(&tar.Header{Name: "file", Typeflag: tar.TypeReg, Mode: 0600, Size: 1})
	_, _ = tw.Write([]byte{'x'})
	_ = tw.Close()
	f.Add(seed.Bytes(), uint8(1))
	f.Add(make([]byte, 1024), uint8(0))
	f.Fuzz(func(t *testing.T, data []byte, entries uint8) {
		if len(data) > 65536 {
			t.Skip()
		}
		_, receipt := snapshotReceipt(data, int64(entries))
		budget, _ := newWorkspaceBudget(context.Background(), 1<<20, 1000)
		scratch := t.TempDir()
		root, _, err := unpackBuildExport(context.Background(), openSnapshot(t, data), scratch, receipt, budget)
		if err != nil {
			if root != "" {
				t.Fatal("failed archive returned a root")
			}
			if contents, readErr := os.ReadDir(scratch); readErr != nil || len(contents) != 0 {
				t.Fatal("failed archive left staging content", readErr)
			}
		}
	})
}
