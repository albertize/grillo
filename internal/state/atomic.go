// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"
	"os"
	"path/filepath"
)

// Ops are the filesystem operations used by atomic writes. Tests replace them
// to inject a failure at each commit step and prove the previous file survives.
type Ops struct {
	CreateTemp func(dir, pattern string) (*os.File, error)
	Write      func(f *os.File, p []byte) (int, error)
	Chmod      func(f *os.File, mode os.FileMode) error
	Sync       func(f *os.File) error
	Close      func(f *os.File) error
	Rename     func(oldpath, newpath string) error
	Remove     func(path string) error
	SyncDir    func(dir string) error
}

// DefaultOps returns the real filesystem operations.
func DefaultOps() Ops {
	return Ops{
		CreateTemp: os.CreateTemp,
		Write:      func(f *os.File, p []byte) (int, error) { return f.Write(p) },
		Chmod:      func(f *os.File, mode os.FileMode) error { return f.Chmod(mode) },
		Sync:       func(f *os.File) error { return f.Sync() },
		Close:      func(f *os.File) error { return f.Close() },
		Rename:     os.Rename,
		Remove:     os.Remove,
		SyncDir:    syncDir,
	}
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// AtomicWriteFile writes name inside dir using a same-directory temporary file,
// fsync of the file, rename, and fsync of the directory. On any failure the
// temporary file is removed and any previous file is left untouched.
func AtomicWriteFile(dir, name string, data []byte, perm os.FileMode, ops Ops) (err error) {
	if ops.CreateTemp == nil {
		ops = DefaultOps()
	}
	f, err := ops.CreateTemp(dir, "."+name+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			_ = ops.Close(f)
			_ = ops.Remove(tmp)
		}
	}()

	if _, err = ops.Write(f, data); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}
	if err = ops.Chmod(f, perm); err != nil {
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err = ops.Sync(f); err != nil {
		return fmt.Errorf("sync temp: %w", err)
	}
	if err = ops.Close(f); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err = ops.Rename(tmp, filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("rename temp: %w", err)
	}
	if err = ops.SyncDir(dir); err != nil {
		return fmt.Errorf("sync dir: %w", err)
	}
	return nil
}
