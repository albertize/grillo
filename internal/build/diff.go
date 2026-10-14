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
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// fileEntry is a snapshot of one filesystem entry.
type fileEntry struct {
	isDir   bool
	isLink  bool
	mode    int64
	size    int64
	link    string
	hash    string
	modTime time.Time
}

func (e fileEntry) equal(other fileEntry) bool {
	return e.isDir == other.isDir && e.isLink == other.isLink &&
		e.mode == other.mode && e.size == other.size &&
		e.link == other.link && e.hash == other.hash
}

// snapshotTree records every regular file, directory, and symlink under root.
// Special files are rejected because they cannot be represented safely in an
// OCI layer.
func snapshotTreeContext(ctx context.Context, root string, maxEntries int64) (map[string]fileEntry, error) {
	entries := map[string]fileEntry{}
	confined, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer confined.Close()
	var walk func(string) error
	walk = func(dir string) error {
		if strings.Count(filepath.ToSlash(dir), "/") >= maxWorkspaceDepth {
			return ErrWorkspaceLimit
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		f, err := confined.Open(dir)
		if err != nil {
			return err
		}
		defer f.Close()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			batch, readErr := f.ReadDir(128)
			for _, d := range batch {
				if int64(len(entries)) >= maxEntries {
					return ErrWorkspaceLimit
				}
				rel := filepath.Join(dir, d.Name())
				info, err := confined.Lstat(rel)
				if err != nil {
					return err
				}
				entry := fileEntry{mode: int64(info.Mode().Perm()) | specialBits(info.Mode()), modTime: info.ModTime()}
				switch {
				case info.Mode()&os.ModeSymlink != 0:
					entry.isLink = true
					entry.link, err = confined.Readlink(rel)
					if err != nil {
						return err
					}
				case info.IsDir():
					entry.isDir = true
				case info.Mode().IsRegular():
					entry.size = info.Size()
					file, err := confined.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
					if err != nil {
						return err
					}
					opened, statErr := file.Stat()
					if statErr != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) || opened.Size() != info.Size() {
						file.Close()
						return errors.New("build: snapshot file changed while opening")
					}
					hasher := sha256.New()
					_, copyErr := io.CopyN(hasher, contextReader{ctx, file}, info.Size())
					file.Close()
					if copyErr != nil {
						return copyErr
					}
					entry.hash = "sha256:" + hex.EncodeToString(hasher.Sum(nil))
				default:
					return fmt.Errorf("build: %s is a special file and cannot be captured in an image", rel)
				}
				entries[filepath.ToSlash(rel)] = entry
				if entry.isDir {
					if err := walk(rel); err != nil {
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
	err = walk(".")
	return entries, err
}

func specialBits(mode os.FileMode) int64 {
	var bits int64
	if mode&os.ModeSetuid != 0 {
		bits |= 0o4000
	}
	if mode&os.ModeSetgid != 0 {
		bits |= 0o2000
	}
	if mode&os.ModeSticky != 0 {
		bits |= 0o1000
	}
	return bits
}

// fileOwner is an explicit ownership override for a path.
type fileOwner struct {
	uid int
	gid int
}

// writeDiffLayer writes a tar layer containing only the changes between base and
// the current tree rooted at root: added and modified entries, and whiteouts
// for deletions. It returns the number of tar entries written.
func writeDiffLayerContext(ctx context.Context, base map[string]fileEntry, root string, owners map[string]fileOwner, w io.Writer) (int, error) {
	current, err := snapshotTreeContext(ctx, root, DefaultMaxHostEntries)
	if err != nil {
		return 0, err
	}
	include := map[string]fileEntry{}
	for rel, entry := range current {
		old, ok := base[rel]
		if !ok || !old.equal(entry) {
			include[rel] = entry
		}
	}
	// Include the ancestor directories of every change so modes are faithful.
	for rel := range include {
		for parent := parentOf(rel); parent != ""; parent = parentOf(parent) {
			if entry, ok := current[parent]; ok {
				include[parent] = entry
			}
		}
	}
	deleted := collapseDeletions(base, current)

	changed := make([]string, 0, len(include))
	for rel := range include {
		changed = append(changed, rel)
	}
	sort.Slice(changed, func(i, j int) bool {
		di, dj := strings.Count(changed[i], "/"), strings.Count(changed[j], "/")
		if di != dj {
			return di < dj
		}
		return changed[i] < changed[j]
	})

	confined, err := os.OpenRoot(root)
	if err != nil {
		return 0, err
	}
	defer confined.Close()
	tw := tar.NewWriter(w)
	written := 0
	writeEntry := func(header *tar.Header) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if owners != nil {
			if owner, ok := owners[header.Name]; ok {
				header.Uid, header.Gid = owner.uid, owner.gid
			}
		}
		written++
		return tw.WriteHeader(header)
	}
	// Directories first.
	for _, rel := range changed {
		entry := include[rel]
		if !entry.isDir {
			continue
		}
		if err := writeEntry(&tar.Header{
			Name:     rel + "/",
			Mode:     entry.mode,
			Typeflag: tar.TypeDir,
			ModTime:  entry.modTime,
		}); err != nil {
			return written, err
		}
	}
	// Whiteouts before other entries.
	for _, rel := range deleted {
		dir, baseName := splitParent(rel)
		name := joinPath(dir, ".wh."+baseName)
		if err := writeEntry(&tar.Header{
			Name:     name,
			Mode:     0o600,
			Size:     0,
			Typeflag: tar.TypeReg,
			ModTime:  time.Unix(0, 0),
		}); err != nil {
			return written, err
		}
	}
	// Files and symlinks.
	for _, rel := range changed {
		entry := include[rel]
		switch {
		case entry.isLink:
			if err := writeEntry(&tar.Header{
				Name:     rel,
				Mode:     entry.mode,
				Typeflag: tar.TypeSymlink,
				Linkname: entry.link,
				ModTime:  entry.modTime,
			}); err != nil {
				return written, err
			}
		case entry.isDir:
			// already written
		default:
			header := &tar.Header{
				Name:     rel,
				Mode:     entry.mode,
				Size:     entry.size,
				Typeflag: tar.TypeReg,
				ModTime:  entry.modTime,
			}
			if err := writeEntry(header); err != nil {
				return written, err
			}
			file, err := confined.OpenFile(filepath.FromSlash(rel), os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
			if err != nil {
				return written, err
			}
			info, err := file.Stat()
			if err != nil || !info.Mode().IsRegular() || info.Size() != entry.size {
				file.Close()
				return written, errors.New("build: layer file changed while opening")
			}
			if _, err := io.CopyN(tw, contextReader{ctx, file}, entry.size); err != nil {
				file.Close()
				return written, err
			}
			if err := file.Close(); err != nil {
				return written, err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return written, err
	}
	return written, nil
}

// collapseDeletions returns the smallest set of deleted paths: a deleted
// directory hides its deleted children.
func collapseDeletions(base, current map[string]fileEntry) []string {
	var deleted []string
	for rel := range base {
		if _, ok := current[rel]; ok {
			continue
		}
		covered := false
		for parent := parentOf(rel); parent != ""; parent = parentOf(parent) {
			if _, ok := current[parent]; !ok {
				covered = true
				break
			}
		}
		if !covered {
			deleted = append(deleted, rel)
		}
	}
	sort.Strings(deleted)
	return deleted
}

func parentOf(rel string) string {
	index := strings.LastIndex(rel, "/")
	if index < 0 {
		return ""
	}
	return rel[:index]
}

func splitParent(rel string) (string, string) {
	index := strings.LastIndex(rel, "/")
	if index < 0 {
		return "", rel
	}
	return rel[:index], rel[index+1:]
}

func joinPath(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}
