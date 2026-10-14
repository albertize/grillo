// SPDX-License-Identifier: Apache-2.0

package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

const (
	maxWorkspaceDepth              = 128
	DefaultMaxHostWriteBytes int64 = 4 << 30
	DefaultMaxHostEntries    int64 = 100000
)

var ErrWorkspaceLimit = errors.New("build: host workspace write budget exhausted")

// workspaceBudget counts cumulative host writes, including overwritten files,
// imported bases, all stages and compressed layer staging. It does not bound
// guest writes through virtiofs, physical allocation or unrelated host data.
type workspaceBudget struct {
	ctx            context.Context
	bytes, entries int64
}

func newWorkspaceBudget(ctx context.Context, bytes, entries int64) (*workspaceBudget, error) {
	if bytes < 0 || entries < 0 {
		return nil, fmt.Errorf("build: negative host workspace limit")
	}
	if bytes == 0 {
		bytes = DefaultMaxHostWriteBytes
	}
	if entries == 0 {
		entries = DefaultMaxHostEntries
	}
	return &workspaceBudget{ctx: ctx, bytes: bytes, entries: entries}, nil
}

// reserve charges before effects. Counting path components conservatively also
// bounds implicitly created parents; failed writes and overwrites get no refund.
func (b *workspaceBudget) reserve(name string, bytes int64) error {
	if b == nil {
		return nil
	}
	if err := b.ctx.Err(); err != nil {
		return err
	}
	entries := int64(strings.Count(filepath.ToSlash(filepath.Clean(name)), "/") + 1)
	if entries > maxWorkspaceDepth {
		return ErrWorkspaceLimit
	}
	if bytes < 0 || bytes > b.bytes || entries > b.entries {
		return ErrWorkspaceLimit
	}
	b.bytes -= bytes
	b.entries -= entries
	return nil
}

type workspaceWriter struct {
	budget    *workspaceBudget
	writer    io.Writer
	remaining int64
}

func (w *workspaceWriter) Write(p []byte) (int, error) {
	if err := w.budget.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > w.remaining || int64(len(p)) > w.budget.bytes {
		return 0, ErrWorkspaceLimit
	}
	w.remaining -= int64(len(p))
	w.budget.bytes -= int64(len(p))
	return w.writer.Write(p)
}

// contextReader deliberately has no WriterTo fast path.
type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
