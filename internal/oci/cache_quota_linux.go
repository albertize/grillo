//go:build linux

// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var ErrCacheQuota = errors.New("oci: cache quota exceeded; explicitly prune unused owned images or increase the configured quota")

// CacheQuota bounds logical file bytes and filesystem entries, not allocated
// blocks, other cache families or volumes. Zero selects finite defaults.
type CacheQuota struct{ MaxBytes, MaxEntries int64 }

const DefaultCASCacheBytes int64 = 16 << 30
const DefaultCacheEntries int64 = 200000

// CacheGuard serializes cooperating writers across instances/processes. It never
// evicts data. The owning UID can bypass this policy by modifying files directly.
type CacheGuard struct {
	root           string
	file           *os.File
	Quota          CacheQuota
	Bytes, Entries int64
}

func AcquireCacheGuard(ctx context.Context, root string, quota CacheQuota) (*CacheGuard, error) {
	return acquireCacheGuard(ctx, root, quota, true)
}
func acquireCacheGuard(ctx context.Context, root string, quota CacheQuota, check bool) (*CacheGuard, error) {
	if quota.MaxBytes < 0 || quota.MaxEntries < 0 {
		return nil, fmt.Errorf("oci: negative cache quota")
	}
	if quota.MaxBytes == 0 {
		quota.MaxBytes = DefaultCASCacheBytes
	}
	if quota.MaxEntries == 0 {
		quota.MaxEntries = DefaultCacheEntries
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	for path := absolute; ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("oci: unsafe cache directory")
		}
		if path == filepath.Dir(path) {
			break
		}
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) {
		return nil, fmt.Errorf("oci: cache directory must belong to the current user")
	}
	fd, err := unix.Open(filepath.Join(absolute, ".grillo-quota.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "cache-quota-lock")
	success := false
	defer func() {
		if !success {
			file.Close()
		}
	}()
	var attrs unix.Stat_t
	if err := unix.Fstat(fd, &attrs); err != nil {
		return nil, err
	}
	if attrs.Mode&unix.S_IFMT != unix.S_IFREG || attrs.Uid != uint32(os.Getuid()) || attrs.Nlink != 1 || attrs.Mode&0077 != 0 || info.Mode().Perm()&0022 != 0 {
		return nil, fmt.Errorf("oci: unsafe cache lock/directory permissions")
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if err != unix.EAGAIN && err != unix.EWOULDBLOCK {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	guard := &CacheGuard{root: absolute, file: file, Quota: quota}
	if check {
		if err := guard.Check(ctx); err != nil {
			return nil, err
		}
	}
	success = true
	return guard, nil
}
func (g *CacheGuard) Close() error { return g.file.Close() }
func (g *CacheGuard) Check(ctx context.Context) error {
	var size, count int64
	// Stream directory entries instead of WalkDir/ReadDir's whole-directory
	// sorting allocation. Bound open directories/recursion as well as entries.
	var visit func(string, int) error
	visit = func(path string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 128 {
			return fmt.Errorf("oci: cache directory depth exceeds 128")
		}
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		// ReadDir entries use File.Name when Info performs its no-follow stat.
		directory := os.NewFile(uintptr(fd), path)
		defer directory.Close()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			entries, readErr := directory.ReadDir(128)
			for _, entry := range entries {
				if err := ctx.Err(); err != nil {
					return err
				}
				if depth == 0 && entry.Name() == ".grillo-quota.lock" {
					continue
				}
				count++
				if count > g.Quota.MaxEntries {
					return ErrCacheQuota
				}
				child := filepath.Join(path, entry.Name())
				if entry.IsDir() {
					if err := visit(child, depth+1); err != nil {
						return err
					}
					continue
				}
				if !entry.Type().IsRegular() && entry.Type()&os.ModeSymlink == 0 {
					return fmt.Errorf("oci: special file in cache")
				}
				info, err := entry.Info()
				if err != nil {
					return err
				}
				if info.Size() < 0 || info.Size() > g.Quota.MaxBytes-size {
					return ErrCacheQuota
				}
				size += info.Size()
			}
			if readErr == io.EOF {
				return nil
			}
			if readErr != nil {
				return readErr
			}
		}
	}
	err := visit(g.root, 0)
	if err != nil {
		return err
	}
	g.Bytes = size
	g.Entries = count
	return nil
}
