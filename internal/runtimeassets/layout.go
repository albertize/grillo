// SPDX-License-Identifier: Apache-2.0

// Package runtimeassets resolves installed files independently of the working
// directory. Explicit developer overrides are never replaced by fallbacks.
package runtimeassets

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// GuestABI is the installed guest layout identifier, not a release support claim.
const GuestABI = "1.1"

// Layout describes the single prefix-relative application payload.
type Layout struct{ Prefix string }

func Discover() (Layout, error) {
	exe, err := os.Executable()
	if err != nil {
		return Layout{}, fmt.Errorf("resolve Grillo executable: %w", err)
	}
	return FromExecutable(exe, os.Getenv("GRILLO_ASSET_PREFIX"))
}

// FromExecutable resolves launcher symlinks before deriving the package prefix.
// Supported executable locations are bin/grillo and libexec/grillo/grillod.
func FromExecutable(exe, prefix string) (Layout, error) {
	if prefix != "" {
		if !filepath.IsAbs(prefix) {
			return Layout{}, fmt.Errorf("GRILLO_ASSET_PREFIX must be absolute")
		}
		return Layout{Prefix: filepath.Clean(prefix)}, nil
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return Layout{}, fmt.Errorf("resolve installed executable: %w", err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return Layout{}, err
	}
	dir := filepath.Dir(resolved)
	switch {
	case filepath.Base(dir) == "bin":
		return Layout{Prefix: filepath.Dir(dir)}, nil
	case filepath.Base(dir) == "grillo" && filepath.Base(filepath.Dir(dir)) == "libexec":
		return Layout{Prefix: filepath.Dir(filepath.Dir(dir))}, nil
	default:
		return Layout{}, fmt.Errorf("unknown Grillo install layout; set absolute GRILLO_ASSET_PREFIX")
	}
}

// Path allows explicit flag/environment overrides. Relative overrides are made
// absolute at the caller's invocation, never passed to a child's working directory.
func (l Layout) Path(component, override string) (string, error) {
	paths := map[string]string{
		"daemon": "libexec/grillo/grillod", "netns": "libexec/grillo/grillo-netns",
		"helm": "libexec/grillo/helm", "kernel": "lib/grillo/guest/" + GuestABI + "/bzImage",
		"initramfs": "lib/grillo/guest/" + GuestABI + "/initramfs.cpio.gz",
		"manifest":  "lib/grillo/guest/" + GuestABI + "/manifest.json",
		"guide":     "share/doc/grillo/runtime-layout.md",
	}
	relative, ok := paths[component]
	if !ok {
		return "", fmt.Errorf("unknown runtime component %q", component)
	}
	if override != "" {
		return filepath.Abs(override)
	}
	if l.Prefix == "" {
		return "", fmt.Errorf("missing install prefix for %s", component)
	}
	return filepath.Join(l.Prefix, filepath.FromSlash(relative)), nil
}

// Resolve uses an explicit override even outside a recognized installation.
func Resolve(component, override string) (string, error) {
	if override != "" {
		return (Layout{}).Path(component, override)
	}
	layout, err := Discover()
	if err != nil {
		return "", err
	}
	return layout.Path(component, "")
}

// VirtioFSD selects distro-managed filesystem helpers consistently for daemon
// and doctor. An explicit override is never replaced by another executable.
func VirtioFSD(override string) (string, error) {
	if override != "" {
		path, err := filepath.Abs(override)
		if err != nil {
			return "", err
		}
		return path, Executable(path)
	}
	for _, path := range []string{"/usr/libexec/virtiofsd", "/usr/bin/virtiofsd"} {
		if err := Executable(path); err == nil {
			return path, nil
		}
	}
	path, err := exec.LookPath("virtiofsd")
	if err != nil {
		return "", fmt.Errorf("virtiofsd unavailable; provision a compatible distro helper or set GRILLO_VIRTIOFSD_BINARY")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return path, Executable(path)
}

// Executable rejects absent/non-executable helpers rather than searching PATH.
func Executable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("required helper %s unavailable: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("required helper %s is not executable", path)
	}
	return nil
}
