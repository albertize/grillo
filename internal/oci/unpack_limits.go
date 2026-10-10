// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"context"
	"errors"
	"fmt"
	"io"
)

const (
	DefaultMaxUnpackBytes        int64 = 4 << 30
	DefaultMaxUnpackFiles        int64 = 100000
	DefaultMaxUnpackArchiveBytes int64 = 8 << 30
)

// ErrUnpackLimit distinguishes resource-policy rejection from corrupt content.
var ErrUnpackLimit = errors.New("oci: extraction quota exceeded")

type unpackBudget struct {
	opts                  UnpackOptions
	bytes, files, archive int64
}

func newUnpackBudget(opts UnpackOptions) (*unpackBudget, error) {
	if opts.MaxBytes < 0 || opts.MaxFiles < 0 || opts.MaxArchiveBytes < 0 {
		return nil, fmt.Errorf("oci: negative extraction limit")
	}
	if opts.MaxBytes == 0 {
		opts.MaxBytes = DefaultMaxUnpackBytes
	}
	if opts.MaxFiles == 0 {
		opts.MaxFiles = DefaultMaxUnpackFiles
	}
	if opts.MaxArchiveBytes == 0 {
		// Bound metadata/skipped bodies too, including small fuzz/test policies.
		// This is a policy allowance, not a prediction of tar overhead.
		extra := int64(1<<20) + min(opts.MaxFiles, DefaultMaxUnpackArchiveBytes/4096)*2048
		opts.MaxArchiveBytes = DefaultMaxUnpackArchiveBytes
		if opts.MaxBytes < DefaultMaxUnpackArchiveBytes-extra {
			opts.MaxArchiveBytes = opts.MaxBytes + extra
		}
	}
	if opts.Context == nil {
		opts.Context = context.Background()
	}
	return &unpackBudget{opts: opts}, nil
}

// quotaReader has no promoted ReaderFrom/WriterTo fast path. Probe at the exact
// boundary to distinguish legitimate EOF from additional decompressed bytes.
// Context checks cannot interrupt an arbitrary reader blocked inside Read.
type quotaReader struct {
	reader    io.Reader
	ctx       context.Context
	remaining int64
	consumed  int64
}

func (r *quotaReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, nil
	}
	if r.remaining == 0 {
		var probe [1]byte
		n, err := r.reader.Read(probe[:])
		if n > 0 {
			return 0, fmt.Errorf("%w: decompressed archive bytes", ErrUnpackLimit)
		}
		return 0, err
	}
	if int64(len(data)) > r.remaining {
		data = data[:int(r.remaining)]
	}
	n, err := r.reader.Read(data)
	r.remaining -= int64(n)
	r.consumed += int64(n)
	return n, err
}
