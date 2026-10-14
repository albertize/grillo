// SPDX-License-Identifier: Apache-2.0

package build

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/albertize/grillo/internal/guestproto"
	"github.com/albertize/grillo/internal/oci"
)

// snapshotExporter is the authenticated client surface needed by the receiver.
type snapshotExporter interface {
	Export(context.Context, guestproto.ExportRequest, io.Writer) (guestproto.ExportResult, error)
}

// exportBuildSnapshot stages a transfer under the same host-write budget as its
// extraction. The caller owns the returned private root and its later adoption.
func exportBuildSnapshot(ctx context.Context, client snapshotExporter, share, scratch string, budget *workspaceBudget) (string, map[string]fileOwner, error) {
	root, owners, _, err := exportBuildSnapshotWithMetadata(ctx, client, share, scratch, budget)
	return root, owners, err
}

func exportBuildSnapshotWithMetadata(ctx context.Context, client snapshotExporter, share, scratch string, budget *workspaceBudget) (string, map[string]fileOwner, *guestproto.BuildRootMetadata, error) {
	if budget == nil || client == nil {
		return "", nil, nil, errors.New("build: export requires a client and host budget")
	}
	limit := min(budget.bytes, guestproto.MaxExportBytes)
	if limit < 1024 {
		return "", nil, nil, ErrWorkspaceLimit
	}
	if err := budget.reserve("build-export.tar", 0); err != nil {
		return "", nil, nil, err
	}
	archive, err := os.CreateTemp(scratch, "build-export-*.tar")
	if err != nil {
		return "", nil, nil, err
	}
	defer func() { _ = archive.Close(); _ = os.Remove(archive.Name()) }()
	receipt, err := client.Export(ctx, guestproto.ExportRequest{Share: share, MaxBytes: limit, MaxEntries: min(budget.entries, guestproto.MaxExportEntries)}, &workspaceWriter{budget: budget, writer: archive, remaining: limit})
	if err != nil {
		return "", nil, nil, fmt.Errorf("build: receive guest snapshot: %w", err)
	}
	if receipt.Root != nil && receipt.Root.Mode > 07777 {
		return "", nil, nil, errors.New("build: invalid exported root mode")
	}
	root, owners, err := unpackBuildExport(ctx, archive, scratch, receipt, budget)
	return root, owners, receipt.Root, err
}

// unpackBuildExport validates an agent export in full before extraction into a
// new private directory. It never applies whiteouts to an existing workspace.
// Numeric owners are returned for OCI layer metadata, without host chown.
func unpackBuildExport(ctx context.Context, archive *os.File, scratch string, receipt guestproto.ExportResult, budget *workspaceBudget) (string, map[string]fileOwner, error) {
	if receipt.Bytes < 1024 || receipt.Bytes > guestproto.MaxExportBytes || receipt.Entries < 0 || receipt.Entries > guestproto.MaxExportEntries {
		return "", nil, errors.New("build: invalid export receipt limits")
	}
	info, err := archive.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != receipt.Bytes {
		return "", nil, errors.New("build: export archive size/type mismatch")
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return "", nil, err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, contextReader{ctx, io.NewSectionReader(archive, 0, receipt.Bytes)}); err != nil {
		return "", nil, err
	}
	if hex.EncodeToString(hash.Sum(nil)) != receipt.SHA256 {
		return "", nil, errors.New("build: export archive checksum mismatch")
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return "", nil, err
	}
	owners := map[string]fileOwner{}
	counted := &exportCountReader{reader: contextReader{ctx, io.NewSectionReader(archive, 0, receipt.Bytes)}}
	tr := tar.NewReader(counted)
	for {
		before := counted.bytes
		header, err := tr.Next()
		if err == io.EOF {
			if counted.bytes-before < 1024 {
				return "", nil, errors.New("build: missing export tar trailer")
			}
			var buffer [32768]byte
			for {
				n, tailErr := counted.Read(buffer[:])
				for _, value := range buffer[:n] {
					if value != 0 {
						return "", nil, errors.New("build: nonzero data after export tar trailer")
					}
				}
				if tailErr == io.EOF {
					break
				}
				if tailErr != nil {
					return "", nil, tailErr
				}
			}
			break
		}
		if err != nil {
			return "", nil, fmt.Errorf("build: invalid export tar: %w", err)
		}
		name := strings.TrimSuffix(header.Name, "/")
		if name == "" || name == "." || path.IsAbs(name) || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "\\") || strings.Count(name, "/") >= maxWorkspaceDepth {
			return "", nil, errors.New("build: invalid export entry path")
		}
		for _, part := range strings.Split(name, "/") {
			if strings.HasPrefix(part, ".wh.") {
				return "", nil, errors.New("build: whiteout in full build snapshot")
			}
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA && header.Typeflag != tar.TypeDir && header.Typeflag != tar.TypeSymlink {
			return "", nil, errors.New("build: unsupported export entry type")
		}
		if header.Uid < 0 || header.Gid < 0 || uint64(header.Uid) > uint64(^uint32(0)) || uint64(header.Gid) > uint64(^uint32(0)) {
			return "", nil, errors.New("build: invalid export ownership")
		}
		if _, duplicate := owners[name]; duplicate {
			return "", nil, errors.New("build: duplicate export path")
		}
		if int64(len(owners)) >= receipt.Entries {
			return "", nil, errors.New("build: export entry count exceeds receipt")
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA && header.Size != 0 {
			return "", nil, errors.New("build: non-file export entry has a body")
		}
		owners[name] = fileOwner{uid: header.Uid, gid: header.Gid}
		if _, err := io.Copy(io.Discard, tr); err != nil {
			return "", nil, err
		}
	}
	if int64(len(owners)) != receipt.Entries {
		return "", nil, errors.New("build: export entry count mismatch")
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return "", nil, err
	}
	if err := budget.reserve("export-root", 0); err != nil {
		return "", nil, err
	}
	root, err := os.MkdirTemp(scratch, "export-root-")
	if err != nil {
		return "", nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(root)
		}
	}()
	opts := oci.UnpackOptions{
		Context: ctx, MaxFiles: guestproto.MaxExportEntries, MaxArchiveBytes: receipt.Bytes,
		BeforeEntry:  budget.reserve,
		MapOwner:     func(_, _ int) (int, int) { return -1, -1 },
		PreserveMode: true,
	}
	if err := oci.UnpackLayer(root, contextReader{ctx, io.NewSectionReader(archive, 0, receipt.Bytes)}, false, opts); err != nil {
		return "", nil, fmt.Errorf("build: extract exported snapshot: %w", err)
	}
	keep = true
	return root, owners, nil
}

type exportCountReader struct {
	reader io.Reader
	bytes  int64
}

func (r *exportCountReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytes += int64(n)
	return n, err
}
