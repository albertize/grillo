// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCASCommitAndOpen(t *testing.T) {
	c, err := OpenCAS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("hello world")
	digest := "sha256:" + sha256Hex(data)
	if err := c.Commit(digest, bytes.NewReader(data), int64(len(data)), 0); err != nil {
		t.Fatal(err)
	}
	if !c.Has(digest) {
		t.Fatal("committed blob missing")
	}
	f, err := c.Open(digest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, _ := io.ReadAll(f)
	if !bytes.Equal(got, data) {
		t.Fatalf("got %q", got)
	}
}

func TestCASRejectsCorruptAndPartial(t *testing.T) {
	c, _ := OpenCAS(t.TempDir())
	data := []byte("hello")
	correct := "sha256:" + sha256Hex(data)
	wrong := "sha256:" + strings.Repeat("0", 64)

	if err := c.Commit(wrong, bytes.NewReader(data), -1, 0); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("err = %v, want ErrDigestMismatch", err)
	}
	if c.Has(wrong) {
		t.Fatal("corrupt blob became visible")
	}
	if err := c.Commit(correct, bytes.NewReader(data), 999, 0); !errors.Is(err, ErrSizeMismatch) {
		t.Fatalf("err = %v, want ErrSizeMismatch", err)
	}
	if c.Has(correct) {
		t.Fatal("partial blob became visible")
	}
	if err := c.Commit(correct, bytes.NewReader(data), int64(len(data)), 3); !errors.Is(err, ErrSizeMismatch) {
		t.Fatalf("max-bytes err = %v, want ErrSizeMismatch", err)
	}
}

func TestCASFetchDeduplicates(t *testing.T) {
	c, _ := OpenCAS(t.TempDir())
	data := []byte("shared blob")
	digest := "sha256:" + sha256Hex(data)
	var calls int32
	fetch := func(context.Context) (io.ReadCloser, int64, error) {
		atomic.AddInt32(&calls, 1)
		return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Fetch(context.Background(), digest, fetch, 0); err != nil {
				t.Errorf("fetch: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("fetch called %d times, want 1", got)
	}
	if !c.Has(digest) {
		t.Fatal("blob missing after concurrent fetch")
	}
}

func TestCASCollect(t *testing.T) {
	c, _ := OpenCAS(t.TempDir())
	first := []byte("first")
	second := []byte("second")
	d1 := "sha256:" + sha256Hex(first)
	d2 := "sha256:" + sha256Hex(second)
	_ = c.PutBytes(d1, first)
	_ = c.PutBytes(d2, second)
	removed, err := c.Collect(func(digest string) bool { return digest == d1 })
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 || !c.Has(d1) || c.Has(d2) {
		t.Fatalf("collect removed=%d d1=%v d2=%v", removed, c.Has(d1), c.Has(d2))
	}
}

func TestOpenCASRejectsBadDigest(t *testing.T) {
	c, _ := OpenCAS(t.TempDir())
	if _, err := c.Path("sha256:short"); err == nil {
		t.Fatal("expected invalid digest error")
	}
}
