// SPDX-License-Identifier: Apache-2.0

package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/albertize/grillo/internal/dockerfile"
)

// copyOptions controls a context copy.
type copyOptions struct {
	ctx          context.Context
	budget       *workspaceBudget
	destRoot     string
	mode         os.FileMode
	sourceOwners map[string]fileOwner
	destOwners   map[string]fileOwner
	// base is the root used to interpret ignore-relative paths.
	base string
	// ignore, when non-nil, excludes paths (Docker copies directory contents and
	// skips ignored entries).
	ignore *dockerfile.Ignore
	// onEntry is called for every created filesystem entry.
	onEntry func(path string, info os.FileInfo) error
}

// copyTree confines source reads and destination writes to their declared roots.
// Directory traversal is streamed in small batches; file payload is reserved
// before creation and copied at the size observed on the opened descriptor.
func copyTree(src, dst string, opts copyOptions) error {
	if opts.ctx == nil {
		opts.ctx = context.Background()
	}
	if opts.base == "" {
		info, err := os.Lstat(src)
		if err != nil {
			return err
		}
		opts.base = filepath.Dir(src)
		if info.IsDir() {
			opts.base = src
		}
	}
	if opts.destRoot == "" {
		opts.destRoot = filepath.Dir(dst)
		info, err := os.Lstat(src)
		if err != nil {
			return err
		}
		if info.IsDir() {
			opts.destRoot = dst
		}
		if err := os.MkdirAll(opts.destRoot, 0755); err != nil {
			return err
		}
	}
	source, err := os.OpenRoot(opts.base)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenRoot(opts.destRoot)
	if err != nil {
		return err
	}
	defer target.Close()
	srcRel, err := filepath.Rel(opts.base, src)
	if err != nil || !filepath.IsLocal(srcRel) {
		return fmt.Errorf("build: COPY source outside its root")
	}
	dstRel, err := filepath.Rel(opts.destRoot, dst)
	if err != nil || !filepath.IsLocal(dstRel) {
		return fmt.Errorf("build: COPY destination outside its root")
	}
	var copyEntry func(string, string) error
	copyEntry = func(from, to string) error {
		if err := opts.ctx.Err(); err != nil {
			return err
		}
		if strings.Count(filepath.ToSlash(from), "/") >= maxWorkspaceDepth {
			return ErrWorkspaceLimit
		}
		if opts.ignore != nil && opts.ignore.Excluded(filepath.ToSlash(from)) {
			return nil
		}
		info, err := source.Lstat(from)
		if err != nil {
			return err
		}
		var size int64
		if info.Mode().IsRegular() {
			size = info.Size()
		}
		if err := opts.budget.reserve(to, size); err != nil {
			return err
		}
		switch {
		case info.IsDir():
			if err := target.MkdirAll(to, info.Mode().Perm()); err != nil {
				return err
			}
			dir, err := source.Open(from)
			if err != nil {
				return err
			}
			defer dir.Close()
			for {
				if err := opts.ctx.Err(); err != nil {
					return err
				}
				entries, readErr := dir.ReadDir(128)
				for _, entry := range entries {
					if err := copyEntry(filepath.Join(from, entry.Name()), filepath.Join(to, entry.Name())); err != nil {
						return err
					}
				}
				if readErr == io.EOF {
					break
				}
				if readErr != nil {
					return readErr
				}
			}
			out, err := target.OpenFile(to, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
			if err != nil {
				return err
			}
			modeErr := out.Chmod(info.Mode().Perm() | info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky))
			closeErr := out.Close()
			if modeErr != nil {
				return modeErr
			}
			if closeErr != nil {
				return closeErr
			}
		case info.Mode()&os.ModeSymlink != 0:
			link, err := source.Readlink(from)
			if err != nil {
				return err
			}
			if err := target.MkdirAll(filepath.Dir(to), 0755); err != nil {
				return err
			}
			if err := target.Remove(to); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if err := target.Symlink(link, to); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			in, err := source.OpenFile(from, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
			if err != nil {
				return err
			}
			defer in.Close()
			opened, err := in.Stat()
			if err != nil {
				return err
			}
			if !opened.Mode().IsRegular() || !os.SameFile(info, opened) || opened.Size() != size {
				return fmt.Errorf("build: COPY source changed while opening")
			}
			if err := target.MkdirAll(filepath.Dir(to), 0755); err != nil {
				return err
			}
			if err := target.Remove(to); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			out, err := target.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(out, contextReader{opts.ctx, in}, size)
			if copyErr == nil {
				mode := info.Mode().Perm() | info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky)
				if opts.mode != 0 {
					mode = opts.mode
				}
				copyErr = out.Chmod(mode)
			}
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return fmt.Errorf("build: cannot copy special file")
		}
		if opts.destOwners != nil {
			if owner, ok := opts.sourceOwners[filepath.ToSlash(from)]; ok {
				opts.destOwners[filepath.ToSlash(to)] = owner
			} else {
				delete(opts.destOwners, filepath.ToSlash(to))
			}
		}
		if opts.onEntry != nil {
			return opts.onEntry(filepath.Join(opts.destRoot, to), info)
		}
		return nil
	}
	return copyEntry(srcRel, dstRel)
}

// copyFlags are the COPY/ADD instruction flags the native builder understands.
type copyFlags struct {
	from  string
	chown string
	chmod string
}

// copyMatches implements COPY's filepath-style glob without collecting an
// unbounded directory listing or following paths outside the context.
func copyMatches(ctx context.Context, rootDir, pattern string, limit int64) ([]string, error) {
	pattern = filepath.FromSlash(strings.TrimLeft(pattern, "/"))
	if strings.Count(filepath.ToSlash(pattern), "/") >= maxWorkspaceDepth {
		return nil, ErrWorkspaceLimit
	}
	if !filepath.IsLocal(pattern) {
		return nil, errors.New("build: COPY source outside its root")
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	var matches []string
	var scanned int64
	var walk func(string, []string) error
	walk = func(dir string, parts []string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(parts) == 0 {
			if int64(len(matches)) >= limit {
				return ErrWorkspaceLimit
			}
			matches = append(matches, filepath.Join(rootDir, dir))
			return nil
		}
		part := parts[0]
		if !strings.ContainsAny(part, "*?[") {
			next := filepath.Join(dir, part)
			if _, err := root.Lstat(next); errors.Is(err, os.ErrNotExist) {
				return nil
			} else if err != nil {
				return err
			}
			return walk(next, parts[1:])
		}
		if _, err := filepath.Match(part, ""); err != nil {
			return err
		}
		f, err := root.Open(dir)
		if err != nil {
			return err
		}
		defer f.Close()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			entries, readErr := f.ReadDir(128)
			for _, entry := range entries {
				scanned++
				if scanned > limit {
					return ErrWorkspaceLimit
				}
				match, err := filepath.Match(part, entry.Name())
				if err != nil {
					return err
				}
				if match {
					if len(parts) > 1 && !entry.IsDir() {
						continue
					}
					if err := walk(filepath.Join(dir, entry.Name()), parts[1:]); err != nil {
						return err
					}
				}
			}
			if readErr == io.EOF {
				return nil
			}
			if readErr != nil {
				return readErr
			}
		}
	}
	if err := walk(".", strings.Split(filepath.Clean(pattern), string(filepath.Separator))); err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}
