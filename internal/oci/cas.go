// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// CAS errors.
var (
	ErrDigestMismatch = errors.New("oci: digest mismatch")
	ErrSizeMismatch   = errors.New("oci: size mismatch")
	ErrNotFound       = errors.New("oci: blob not found")
	ErrBlobCorrupt    = errors.New("oci: blob is corrupt")
)

// CAS is a content-addressed blob store. A blob is only visible under its final
// digest path after its size and SHA-256 have been verified, so a partial
// download never looks valid.
type CAS struct {
	dir      string
	tmpDir   string
	blobsDir string

	mu       sync.Mutex
	inflight map[string]*casInflight
}

type casInflight struct {
	done chan struct{}
	err  error
}

// OpenCAS prepares a CAS rooted at dir.
func OpenCAS(dir string) (*CAS, error) {
	c := &CAS{
		dir:      dir,
		tmpDir:   filepath.Join(dir, "tmp"),
		blobsDir: filepath.Join(dir, "blobs", "sha256"),
		inflight: make(map[string]*casInflight),
	}
	for _, sub := range []string{c.tmpDir, c.blobsDir} {
		if err := os.MkdirAll(sub, 0o755); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// Dir returns the CAS root.
func (c *CAS) Dir() string { return c.dir }

// Path returns the on-disk path of a digest.
func (c *CAS) Path(digest string) (string, error) {
	hexPart, err := digestHex(digest)
	if err != nil {
		return "", err
	}
	return filepath.Join(c.blobsDir, hexPart), nil
}

// Has reports whether a verified blob exists.
func (c *CAS) Has(digest string) bool {
	path, err := c.Path(digest)
	if err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// Open returns a reader for a stored blob.
func (c *CAS) Open(digest string) (io.ReadCloser, error) {
	path, err := c.Path(digest)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return f, err
}

// Commit writes a blob, verifying its size and digest before the atomic rename.
// maxBytes bounds the read (<=0 means no bound).
func (c *CAS) Commit(expectedDigest string, r io.Reader, expectedSize, maxBytes int64) error {
	if _, err := digestHex(expectedDigest); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.tmpDir, "blob-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	hash := sha256.New()
	var written int64
	buf := make([]byte, 64*1024)
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			if maxBytes > 0 && written+int64(n) > maxBytes {
				return fmt.Errorf("%w: exceeds %d bytes", ErrSizeMismatch, maxBytes)
			}
			written += int64(n)
			hash.Write(buf[:n])
			if _, err := tmp.Write(buf[:n]); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	got := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if got != expectedDigest {
		return fmt.Errorf("%w: got %s, want %s", ErrDigestMismatch, got, expectedDigest)
	}
	if expectedSize >= 0 && written != expectedSize {
		return fmt.Errorf("%w: got %d bytes, want %d", ErrSizeMismatch, written, expectedSize)
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	final, err := c.Path(expectedDigest)
	if err != nil {
		return err
	}
	if err := os.Rename(tmpName, final); err != nil {
		return err
	}
	return syncDir(filepath.Dir(final))
}

// PutBytes stores an in-memory blob after verification.
func (c *CAS) PutBytes(digest string, data []byte) error {
	return c.Commit(digest, bytes.NewReader(data), int64(len(data)), 0)
}

// Fetch stores a blob, downloading it at most once even under concurrency. A
// blob already present is not downloaded again.
func (c *CAS) Fetch(ctx context.Context, digest string, fetch func(context.Context) (io.ReadCloser, int64, error), maxBytes int64) error {
	if c.Has(digest) {
		return nil
	}
	c.mu.Lock()
	if existing, ok := c.inflight[digest]; ok {
		c.mu.Unlock()
		select {
		case <-existing.done:
			if existing.err == nil {
				return nil
			}
			return existing.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	flight := &casInflight{done: make(chan struct{})}
	c.inflight[digest] = flight
	c.mu.Unlock()

	err := c.fetchOnce(ctx, digest, fetch, maxBytes)

	c.mu.Lock()
	delete(c.inflight, digest)
	flight.err = err
	close(flight.done)
	c.mu.Unlock()
	return err
}

func (c *CAS) fetchOnce(ctx context.Context, digest string, fetch func(context.Context) (io.ReadCloser, int64, error), maxBytes int64) error {
	body, size, err := fetch(ctx)
	if err != nil {
		return err
	}
	defer body.Close()
	return c.Commit(digest, body, size, maxBytes)
}

// Collect removes blobs for which keep returns false and returns the count.
func (c *CAS) Collect(keep func(digest string) bool) (int, error) {
	entries, err := os.ReadDir(c.blobsDir)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		digest := "sha256:" + entry.Name()
		if keep(digest) {
			continue
		}
		if err := os.Remove(filepath.Join(c.blobsDir, entry.Name())); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

func digestHex(digest string) (string, error) {
	if !digestRe.MatchString(digest) {
		return "", fmt.Errorf("oci: invalid digest %q", digest)
	}
	return digest[len("sha256:"):], nil
}

func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
