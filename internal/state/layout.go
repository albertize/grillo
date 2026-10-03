// SPDX-License-Identifier: Apache-2.0

// Package state implements the per-user runtime layout, single-writer lock,
// atomic snapshots, pending operations, and a bounded event journal. It is
// generic metadata storage: it does not decide reconciliation policy.
package state

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// Config supplies the environment the layout is derived from. Tests construct
// one directly; DefaultConfig reads the real process environment.
type Config struct {
	Getenv  func(string) string
	UID     int
	Home    string
	TempDir string
}

// DefaultConfig reads the current process environment.
func DefaultConfig() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Getenv:  os.Getenv,
		UID:     os.Getuid(),
		Home:    home,
		TempDir: os.TempDir(),
	}
}

// Layout is the resolved per-user directory set. Every directory is private to
// the user (0700) and must not be a symlink.
type Layout struct {
	Runtime string // lock, socket, process metadata
	State   string // desired state, observations, operations, events
	Data    string // volumes, guest artifacts, secret store
	Cache   string // OCI blobs and derived rootfs
}

// NewLayout resolves the layout from Config using XDG variables with documented
// fallbacks. It does not create directories.
func NewLayout(cfg Config) (Layout, error) {
	if cfg.UID < 0 {
		return Layout{}, errors.New("state: invalid uid")
	}
	runtime := cfg.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		candidate := fmt.Sprintf("/run/user/%d", cfg.UID)
		if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
			runtime = candidate
		} else {
			runtime = filepath.Join(cfg.TempDir, fmt.Sprintf("grillo-%d", cfg.UID))
		}
	}
	stateHome := cfg.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		stateHome = filepath.Join(cfg.Home, ".local", "state")
	}
	dataHome := cfg.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(cfg.Home, ".local", "share")
	}
	cacheHome := cfg.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		cacheHome = filepath.Join(cfg.Home, ".cache")
	}
	return Layout{
		Runtime: filepath.Join(runtime, "grillo"),
		State:   filepath.Join(stateHome, "grillo"),
		Data:    filepath.Join(dataHome, "grillo"),
		Cache:   filepath.Join(cacheHome, "grillo"),
	}, nil
}

// Prepare creates the layout directories with 0700 and verifies that each is a
// real directory owned by the current user, rejecting symlinks.
func (l Layout) Prepare() error {
	for _, dir := range []string{l.Runtime, l.State, l.Data, l.Cache} {
		if err := ensurePrivateDir(dir, os.Getuid()); err != nil {
			return err
		}
	}
	return nil
}

func ensurePrivateDir(dir string, uid int) error {
	fi, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
			return fmt.Errorf("state: create %s: %w", dir, mkErr)
		}
		fi, err = os.Lstat(dir)
	}
	if err != nil {
		return fmt.Errorf("state: stat %s: %w", dir, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("state: refusing symlinked directory %s", dir)
	}
	if !fi.IsDir() {
		return fmt.Errorf("state: %s is not a directory", dir)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != uid {
		return fmt.Errorf("state: %s is owned by uid %d, want %d", dir, st.Uid, uid)
	}
	return nil
}
