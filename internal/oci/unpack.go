// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

// Whiteout markers used by OCI/Docker layers.
const (
	whiteoutPrefix = ".wh."
	whiteoutOpaque = ".wh..wh..opq"
)

// UnpackOptions bounds and adjusts extraction.
type UnpackOptions struct {
	// MapOwner optionally rewrites image UIDs/GIDs (for rootless extraction).
	// Returning -1 keeps the original value, which is then recorded only if the
	// chown is permitted.
	MapOwner func(uid, gid int) (int, int)
	// MaxBytes bounds cumulative regular-file payload written, even overwrites.
	// Zero selects DefaultMaxUnpackBytes; it never disables the quota.
	MaxBytes int64
	// MaxFiles bounds entries including whiteouts (zero selects the default).
	MaxFiles int64
	// MaxArchiveBytes bounds decompressed tar bytes including metadata, padding
	// and skipped bodies. Zero selects a finite derived default, capped at 8 GiB.
	MaxArchiveBytes int64
	// Context checks cancellation between reads/entries, not within blocked I/O.
	Context context.Context
}

// DiffID returns the SHA-256 of the uncompressed layer content.
func DiffID(r io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, r); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

// UnpackLayer applies one layer to root. It handles gzip, whiteouts, and opaque
// directories, and refuses entries that could escape root through absolute
// paths, "..", symlinks, hardlinks, devices, or FIFOs.
func UnpackLayer(root string, layer io.Reader, gzipCompressed bool, opts UnpackOptions) error {
	budget, err := newUnpackBudget(opts)
	if err != nil {
		return err
	}
	return unpackLayer(root, layer, gzipCompressed, budget)
}

func unpackLayer(root string, layer io.Reader, gzipCompressed bool, budget *unpackBudget) error {
	if err := budget.opts.Context.Err(); err != nil {
		return err
	}
	reader := layer
	if gzipCompressed {
		gz, err := gzip.NewReader(layer)
		if err != nil {
			return fmt.Errorf("oci: gzip layer: %w", err)
		}
		defer gz.Close()
		reader = gz
	}
	bounded := &quotaReader{reader: reader, ctx: budget.opts.Context, remaining: budget.opts.MaxArchiveBytes - budget.archive}
	defer func() { budget.archive += bounded.consumed }()
	tr := tar.NewReader(bounded)
	ex := &extractor{root: root, opts: budget.opts, budget: budget}
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			// Consume padding/trailing compressed data under the same quota, validating
			// gzip checksum rather than stopping as soon as tar reports its trailer.
			_, err := io.Copy(io.Discard, bounded)
			return err
		}
		if err != nil {
			return fmt.Errorf("oci: read layer tar: %w", err)
		}
		if err := ex.entry(header, tr); err != nil {
			return err
		}
	}
}

type extractor struct {
	root   string
	opts   UnpackOptions
	budget *unpackBudget
}

func (e *extractor) entry(header *tar.Header, body io.Reader) error {
	if err := e.opts.Context.Err(); err != nil {
		return err
	}
	if err := e.count(); err != nil {
		return err
	}
	name := header.Name
	if name == "" {
		return nil
	}
	// Whiteout handling happens before path validation of the entry itself.
	base := path.Base(name)
	dir := filepath.Dir(name)
	switch {
	case base == whiteoutOpaque:
		target, err := e.join(dir)
		if err != nil {
			return err
		}
		return removeAllWithin(e.root, target)
	case strings.HasPrefix(base, whiteoutPrefix):
		plain := strings.TrimPrefix(base, whiteoutPrefix)
		target, err := e.join(filepath.Join(dir, plain))
		if err != nil {
			return err
		}
		return removeAllWithin(e.root, target)
	}

	target, err := e.join(name)
	if err != nil {
		return err
	}
	switch header.Typeflag {
	case tar.TypeDir:
		if err := os.MkdirAll(target, os.FileMode(header.Mode)&os.ModePerm); err != nil {
			return err
		}
		return e.applyMeta(target, header)
	case tar.TypeReg, tar.TypeRegA:
		return e.regular(target, header, body)
	case tar.TypeSymlink:
		if err := e.checkLinkTarget(header.Linkname); err != nil {
			return err
		}
		_ = os.Remove(target)
		if err := os.Symlink(header.Linkname, target); err != nil {
			return err
		}
		return nil
	case tar.TypeLink:
		linkTarget, err := e.join(header.Linkname)
		if err != nil {
			return err
		}
		_ = os.Remove(target)
		if err := os.Link(linkTarget, target); err != nil {
			return err
		}
		return nil
	case tar.TypeXGlobalHeader, tar.TypeXHeader:
		return nil
	case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
		return fmt.Errorf("oci: refusing device/FIFO entry %q", name)
	default:
		return fmt.Errorf("oci: unsupported tar entry type %d for %q", header.Typeflag, name)
	}
}

func (e *extractor) count() error {
	if e.budget.files >= e.opts.MaxFiles {
		return fmt.Errorf("%w: image exceeds %d entries", ErrUnpackLimit, e.opts.MaxFiles)
	}
	e.budget.files++
	return nil
}

func (e *extractor) regular(target string, header *tar.Header, body io.Reader) error {
	if header.Size < 0 {
		return fmt.Errorf("oci: negative file size for %q", header.Name)
	}
	if header.Size > e.opts.MaxBytes-e.budget.bytes {
		return fmt.Errorf("%w: image exceeds %d payload bytes", ErrUnpackLimit, e.opts.MaxBytes)
	}
	if dir := filepath.Dir(target); dir != e.root {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	// Remove any existing entry first so a symlink is never followed by the open.
	_ = os.Remove(target)
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|syscall.O_NOFOLLOW, os.FileMode(header.Mode)&os.ModePerm)
	if err != nil {
		return err
	}
	n, copyErr := io.CopyN(f, body, header.Size)
	e.budget.bytes += n
	closeErr := f.Close()
	if copyErr != nil {
		return fmt.Errorf("oci: write %q: %w", header.Name, copyErr)
	}
	if closeErr != nil {
		return closeErr
	}
	return e.applyMeta(target, header)
}

func (e *extractor) applyMeta(target string, header *tar.Header) error {
	// Clear setuid/setgid: extracted content must not gain privilege.
	perm := os.FileMode(header.Mode) & os.ModePerm
	if err := os.Chmod(target, perm); err != nil {
		return err
	}
	uid, gid := header.Uid, header.Gid
	if e.opts.MapOwner != nil {
		if mu, mg := e.opts.MapOwner(uid, gid); mu != 0 || mg != 0 {
			uid, gid = mu, mg
		}
	}
	if err := os.Lchown(target, uid, gid); err != nil && !errors.Is(err, syscall.EPERM) {
		return err
	}
	return nil
}

// join validates a layer name and returns an absolute path under root. The
// parent path components must not be symlinks.
func (e *extractor) join(name string) (string, error) {
	if path.IsAbs(name) {
		return "", fmt.Errorf("oci: absolute path %q in layer", name)
	}
	clean := path.Clean(name)
	if clean == "." {
		return e.root, nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("oci: path escapes root: %q", name)
	}
	current := e.root
	parts := strings.Split(clean, "/")
	for i, part := range parts {
		current = filepath.Join(current, part)
		if i == len(parts)-1 {
			break
		}
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("oci: symlink in parent path %q", name)
		}
	}
	return current, nil
}

func (e *extractor) checkLinkTarget(target string) error {
	if target == "" {
		return fmt.Errorf("oci: empty link target")
	}
	// Absolute and ".." symlink targets are allowed because they are resolved
	// inside the container at runtime, not during extraction. The containment
	// guarantee comes from never following symlinks while extracting.
	return nil
}

// removeAllWithin removes target only when it is strictly inside root.
func removeAllWithin(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("oci: refusing to remove %s outside %s", target, root)
	}
	if rel == "." {
		return nil
	}
	return os.RemoveAll(target)
}
