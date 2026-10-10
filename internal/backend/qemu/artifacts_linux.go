//go:build linux

// SPDX-License-Identifier: Apache-2.0

package qemu

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/albertize/grillo/internal/guest"
	"github.com/albertize/grillo/internal/sandbox"
)

func copyArtifact(ctx context.Context, a guest.Artifact, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	source, err := a.Open()
	if err != nil {
		return fmt.Errorf("qemu: open %s artifact", a.Name)
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != a.Size {
		return fmt.Errorf("qemu: invalid %s artifact", a.Name)
	}
	file, err := os.CreateTemp(filepath.Dir(target), ".verified-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	hash := sha256.New()
	remaining := a.Size
	buffer := make([]byte, 64<<10)
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := source.Read(buffer[:min(int64(len(buffer)), remaining)])
		if n > 0 {
			if _, e := io.MultiWriter(file, hash).Write(buffer[:n]); e != nil {
				return e
			}
			remaining -= int64(n)
		}
		if err != nil && remaining > 0 {
			return fmt.Errorf("qemu: read %s artifact", a.Name)
		}
	}
	var extra [1]byte
	if n, _ := source.Read(extra[:]); n != 0 || hex.EncodeToString(hash.Sum(nil)) != a.SHA256 {
		return fmt.Errorf("qemu: %s artifact digest mismatch", a.Name)
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(file.Name(), target)
}

// appendBootKey adds a deterministic newc archive in a second gzip member.
// Linux unpacks concatenated initramfs archives in order; only the key changes.
// The per-operation snapshot is private. No key appears in argv or public IR.
func appendBootKey(path string, key []byte) error {
	if len(key) != 32 {
		return fmt.Errorf("qemu: fresh boot key must contain 32 bytes")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	writer := gzip.NewWriter(f)
	entry := func(name string, mode int, data []byte) error {
		nameSize := len(name) + 1
		header := fmt.Sprintf("070701%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x", 1, mode, 0, 0, 1, 0, len(data), 0, 0, 0, 0, nameSize, 0)
		if _, err := io.WriteString(writer, header+name+"\x00"); err != nil {
			return err
		}
		if _, err := writer.Write(make([]byte, (4-(110+nameSize)%4)%4)); err != nil {
			return err
		}
		if _, err := writer.Write(data); err != nil {
			return err
		}
		_, err := writer.Write(make([]byte, (4-len(data)%4)%4))
		return err
	}
	if err := entry("etc/grillo/key", 0o100600, []byte(base64.StdEncoding.EncodeToString(key)+"\n")); err != nil {
		return err
	}
	if err := entry("TRAILER!!!", 0, nil); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return f.Sync()
}

func (b *Backend) prepareBoot(ctx context.Context, spec sandbox.Spec, dir string) (sandbox.Spec, error) {
	if b.artifacts != nil {
		for _, name := range []string{"kernel", "initramfs"} {
			path := spec.Kernel
			if path == "" {
				path = b.cfg.Kernel
			}
			if name == "initramfs" {
				path = spec.Initramfs
				if path == "" {
					path = b.cfg.Initramfs
				}
			}
			expected := b.artifacts[name]
			actual, err := filepath.Abs(path)
			if err != nil {
				return spec, err
			}
			wanted, err := filepath.Abs(expected.HostPath())
			if err != nil || actual != wanted {
				return spec, fmt.Errorf("qemu: %s path does not match manifest", name)
			}
			target := filepath.Join(dir, "verified-"+name)
			if err := copyArtifact(ctx, expected, target); err != nil {
				return spec, err
			}
			if name == "kernel" {
				spec.Kernel = target
			} else {
				spec.Initramfs = target
			}
		}
	}
	if b.cfg.BootKeyOverlay {
		if b.artifacts == nil {
			return spec, fmt.Errorf("qemu: boot key overlay requires verified artifacts")
		}
		if err := appendBootKey(spec.Initramfs, spec.GuestKey); err != nil {
			return spec, err
		}
	}
	return spec, nil
}
