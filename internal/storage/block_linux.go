//go:build linux

// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

var ErrQuota = errors.New("storage: block-image quota exhausted; remove unused owned volumes explicitly or increase the reservation policy")

// BlockOptions opts into fixed-size ext4 images. No host mount or sudo is used.
// Geometry bounds guest writes; sparse allocation does not reserve host free
// space. Filesystem metadata reduces usable payload below image byte capacity.
type BlockOptions struct {
	Formatter        string
	DefaultBytes     int64
	MaxReservedBytes int64
	MaxImages        int
}

type blockStore struct {
	options BlockOptions
	dir     string
}

// OpenBounded enables real block-backed volumes for newly created non-bind data.
// Existing directory volumes are never silently migrated, resized or deleted.
func OpenBounded(dir string, options BlockOptions) (*Manager, error) {
	if options.DefaultBytes < 16<<20 || options.DefaultBytes%(4096) != 0 || options.MaxReservedBytes < options.DefaultBytes || options.MaxImages <= 0 {
		return nil, fmt.Errorf("storage: invalid block capacity/reservation policy")
	}
	if !filepath.IsAbs(options.Formatter) {
		return nil, fmt.Errorf("storage: absolute trusted mke2fs path required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, options.Formatter, "-V")
	command.Env = []string{"PATH=/usr/sbin:/usr/bin", "LC_ALL=C"}
	var output limitedFormatterOutput
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil || !strings.HasPrefix(output.text, "mke2fs 1.47.3 ") {
		return nil, fmt.Errorf("storage: reviewed mke2fs 1.47.3 required")
	}
	m, err := Open(dir)
	if err != nil {
		return nil, err
	}
	blockDir := filepath.Join(dir, "block-images")
	if err := os.MkdirAll(blockDir, 0700); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(blockDir)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil || resolved != absolute {
		return nil, fmt.Errorf("storage: block store must not follow symlinks")
	}
	info, err := os.Stat(absolute)
	if err != nil || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("storage: private block store required")
	}
	m.block = &blockStore{options: options, dir: absolute}
	return m, nil
}

func (m *Manager) BlockStorageEnabled() bool { return m.block != nil }

// lock serializes reservation/creation/deletion across cooperating managers and
// processes. The operation deadline includes waiting for the advisory lock.
func (b *blockStore) lock(ctx context.Context) (*os.File, error) {
	fd, err := unix.Open(filepath.Join(b.dir, ".quota.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "volume-quota-lock")
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) || stat.Mode&0077 != 0 {
		f.Close()
		return nil, fmt.Errorf("storage: unsafe quota lock")
	}
	for {
		if err := ctx.Err(); err != nil {
			f.Close()
			return nil, err
		}
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if err != unix.EWOULDBLOCK && err != unix.EAGAIN {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (b *blockStore) usage(ctx context.Context) (int64, int, error) {
	dir, err := os.Open(b.dir)
	if err != nil {
		return 0, 0, err
	}
	defer dir.Close()
	var bytes int64
	var count int
	for {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		entries, err := dir.ReadDir(128)
		for _, entry := range entries {
			if entry.Name() == ".quota.lock" {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return 0, 0, err
			}
			if !info.Mode().IsRegular() {
				return 0, 0, fmt.Errorf("storage: unexpected block-store entry; manual review required")
			}
			count++
			if count > b.options.MaxImages || info.Size() > b.options.MaxReservedBytes-bytes {
				return 0, 0, ErrQuota
			}
			bytes += info.Size()
		}
		if errors.Is(err, os.ErrNotExist) {
			return 0, 0, err
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return bytes, count, nil
			}
			return 0, 0, err
		}
	}
}

func (m *Manager) createBlock(ctx context.Context, volume Volume, op string) (Volume, error) {
	b := m.block
	lock, err := b.lock(ctx)
	if err != nil {
		return Volume{}, err
	}
	defer lock.Close()
	// Recheck durable metadata after cross-manager admission.
	if existing, err := m.load(volume.ID); err == nil {
		return m.existingBlockRequest(existing, volume, op)
	} else if !errors.Is(err, ErrNotFound) {
		return Volume{}, err
	}
	capacity := volume.CapacityBytes
	if capacity == 0 {
		capacity = b.options.DefaultBytes
	}
	if capacity < 16<<20 || capacity%4096 != 0 {
		return Volume{}, fmt.Errorf("storage: block capacity must be >=16 MiB and 4096-byte aligned")
	}
	bytes, count, err := b.usage(ctx)
	if err != nil {
		return Volume{}, err
	}
	if count >= b.options.MaxImages || capacity > b.options.MaxReservedBytes-bytes {
		return Volume{}, ErrQuota
	}
	final := filepath.Join(b.dir, volume.ID+".img")
	if _, err := os.Lstat(final); err == nil {
		return Volume{}, fmt.Errorf("storage: interrupted image without record; manual review required")
	} else if !errors.Is(err, os.ErrNotExist) {
		return Volume{}, err
	}
	file, err := os.CreateTemp(b.dir, ".image-")
	if err != nil {
		return Volume{}, err
	}
	stage := file.Name()
	defer os.Remove(stage)
	if err := file.Truncate(capacity); err != nil {
		file.Close()
		return Volume{}, err
	}
	if err := file.Close(); err != nil {
		return Volume{}, err
	}
	command := exec.CommandContext(ctx, b.options.Formatter, "-F", "-q", "-t", "ext4", "-b", "4096", "-m", "0", "-O", "^has_journal", "-E", "lazy_itable_init=0,nodiscard", stage)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin", "LC_ALL=C"}
	var output limitedFormatterOutput
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		return Volume{}, fmt.Errorf("storage: ext4 formatting failed: %w", err)
	}
	if err := verifyBlock(stage, capacity); err != nil {
		return Volume{}, err
	}
	if err := unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, final, unix.RENAME_NOREPLACE); err != nil {
		return Volume{}, err
	}
	directory, err := os.Open(b.dir)
	if err != nil {
		os.Remove(final)
		return Volume{}, err
	}
	err = directory.Sync()
	directory.Close()
	if err != nil {
		os.Remove(final)
		return Volume{}, err
	}
	volume.Source = final
	volume.Filesystem = "ext4"
	volume.CapacityBytes = capacity
	volume.Owner.Operation = op
	volume.CreatedAt = time.Now().UTC()
	if err := os.MkdirAll(m.volumeDir(volume.ID), 0700); err != nil {
		os.Remove(final)
		return Volume{}, err
	}
	if err := m.save(volume); err != nil {
		os.Remove(final)
		return Volume{}, err
	}
	return volume, nil
}

func (m *Manager) existingBlockRequest(existing, requested Volume, op string) (Volume, error) {
	if existing.Kind != requested.Kind || requested.CapacityBytes > 0 && requested.CapacityBytes != existing.CapacityBytes {
		return Volume{}, fmt.Errorf("%w: existing volume backing/capacity changes require explicit migration", ErrUnsupported)
	}
	return m.existingBlock(existing, op)
}

func (m *Manager) existingBlock(volume Volume, op string) (Volume, error) {
	if op != "" && volume.Owner.Operation != op {
		return Volume{}, ErrBusy
	}
	if volume.Kind == KindBind {
		return volume, nil
	}
	if volume.Filesystem != "ext4" || volume.Source != filepath.Join(m.block.dir, volume.ID+".img") {
		return Volume{}, fmt.Errorf("%w: legacy directory volume requires explicitly authorized migration", ErrUnsupported)
	}
	if err := verifyBlock(volume.Source, volume.CapacityBytes); err != nil {
		return Volume{}, err
	}
	return volume, nil
}

func verifyBlock(path string, capacity int64) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) || stat.Mode&0077 != 0 {
		return fmt.Errorf("storage: private single-link block image required")
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != capacity {
		return fmt.Errorf("storage: block geometry changed")
	}
	var super [64]byte
	if _, err := file.ReadAt(super[:], 1024); err != nil {
		return err
	}
	if binary.LittleEndian.Uint16(super[56:58]) != 0xef53 {
		return fmt.Errorf("storage: invalid ext4 superblock")
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return nil
}

type limitedFormatterOutput struct{ text string }

func (o *limitedFormatterOutput) Write(data []byte) (int, error) {
	if len(o.text)+len(data) > 4096 {
		return 0, fmt.Errorf("storage: formatter output bound")
	}
	o.text += string(data)
	return len(data), nil
}
