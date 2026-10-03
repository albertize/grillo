// SPDX-License-Identifier: Apache-2.0

package build

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"grillo.local/grillo/internal/dockerfile"
)

// copyOptions controls a context copy.
type copyOptions struct {
	// base is the root used to interpret ignore-relative paths.
	base string
	// ignore, when non-nil, excludes paths (Docker copies directory contents and
	// skips ignored entries).
	ignore *dockerfile.Ignore
	// onEntry is called for every created filesystem entry.
	onEntry func(path string, info os.FileInfo) error
}

// copyTree copies a source file, symlink, or directory into dst. For a
// directory source the directory's contents are copied into dst, matching
// Docker COPY semantics.
func copyTree(src, dst string, opts copyOptions) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			childSrc := filepath.Join(src, entry.Name())
			childDst := filepath.Join(dst, entry.Name())
			if opts.ignore != nil && opts.skip(childSrc) {
				continue
			}
			if err := copyEntry(childSrc, childDst, opts); err != nil {
				return err
			}
		}
		return nil
	}
	return copyEntry(src, dst, opts)
}

func (o copyOptions) skip(path string) bool {
	if o.ignore == nil {
		return false
	}
	rel, err := filepath.Rel(o.base, path)
	if err != nil {
		return false
	}
	return o.ignore.Excluded(filepath.ToSlash(rel))
}

func copyEntry(src, dst string, opts copyOptions) error {
	if opts.skip(src) {
		return nil
	}
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		if opts.onEntry != nil {
			if err := opts.onEntry(dst, info); err != nil {
				return err
			}
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			childSrc := filepath.Join(src, entry.Name())
			childDst := filepath.Join(dst, entry.Name())
			if opts.ignore != nil && opts.skip(childSrc) {
				continue
			}
			if err := copyEntry(childSrc, childDst, opts); err != nil {
				return err
			}
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		_ = os.Remove(dst)
		if err := os.Symlink(target, dst); err != nil {
			return err
		}
	case info.Mode().IsRegular():
		if err := copyFile(src, dst, info.Mode().Perm()); err != nil {
			return err
		}
	default:
		return fmt.Errorf("build: cannot copy special file %s", src)
	}
	if opts.onEntry != nil {
		if err := opts.onEntry(dst, info); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	_ = os.Remove(dst)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// copyFlags are the COPY/ADD instruction flags the native builder understands.
type copyFlags struct {
	from  string
	chown string
	chmod string
}
