//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/albertize/grillo/internal/guestproto"
	"github.com/albertize/grillo/internal/oci"
)

func populateBuildSeed(ctx context.Context, spec guestproto.SandboxSpec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	seed := spec.BuildSeed
	if seed == nil {
		return errors.New("guest: block build seed is missing")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var sourcePath, targetPath string
	for _, share := range spec.Shares {
		if share.Tag == seed.SourceTag {
			sourcePath = share.Target
		}
		if share.Tag == seed.TargetTag {
			targetPath = share.Target
		}
	}
	source, err := os.OpenRoot(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	archive, err := source.OpenFile("root.tar", os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer archive.Close()
	info, err := archive.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != seed.Bytes {
		return errors.New("guest: block seed archive size/type mismatch")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, seedReader{ctx, io.NewSectionReader(archive, 0, seed.Bytes)}); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != seed.SHA256 {
		return errors.New("guest: block seed checksum mismatch")
	}
	target, err := os.OpenRoot(targetPath)
	if err != nil {
		return err
	}
	defer target.Close()
	entries, err := target.Open(".")
	if err != nil {
		return err
	}
	defer entries.Close()
	contents, err := entries.ReadDir(2)
	if err != nil && err != io.EOF {
		return err
	}
	if len(contents) > 1 || len(contents) == 1 && contents[0].Name() != "lost+found" {
		return errors.New("guest: block build root is not fresh")
	}
	if len(contents) == 1 {
		// Remove only the empty formatter-created directory. Never recursively
		// discard preexisting contents to manufacture a fresh build workspace.
		if err := target.Remove("lost+found"); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	opts := oci.UnpackOptions{Context: ctx, MaxFiles: seed.Entries, MaxArchiveBytes: seed.Bytes, PreserveMode: true,
		BeforeHeader: func(h *tar.Header) error {
			name := strings.TrimSuffix(h.Name, "/")
			if path.IsAbs(name) || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "\\") || strings.Count(name, "/") >= 128 || seen[name] {
				return errors.New("guest: invalid block seed path")
			}
			if name == "." && (len(seen) != 0 || h.Typeflag != tar.TypeDir) {
				return errors.New("guest: invalid block seed root metadata")
			}
			for _, part := range strings.Split(name, "/") {
				if strings.HasPrefix(part, ".wh.") {
					return errors.New("guest: whiteout in block seed snapshot")
				}
			}
			if h.Typeflag != tar.TypeDir && h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA && h.Typeflag != tar.TypeSymlink {
				return errors.New("guest: unsupported block seed entry")
			}
			if h.Uid < 0 || h.Gid < 0 || uint64(h.Uid) > uint64(^uint32(0)) || uint64(h.Gid) > uint64(^uint32(0)) || h.Mode < 0 || h.Mode > 07777 {
				return errors.New("guest: invalid block seed metadata")
			}
			seen[name] = true
			return nil
		},
	}
	if err := oci.UnpackLayer(targetPath, seedReader{ctx, io.NewSectionReader(archive, 0, seed.Bytes)}, false, opts); err != nil {
		return err
	}
	if int64(len(seen)) != seed.Entries || !seen["."] {
		return errors.New("guest: block seed entry count/root mismatch")
	}
	return nil
}

type seedReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r seedReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
