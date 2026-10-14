//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/albertize/grillo/internal/guestproto"
)

var errExportLimit = errors.New("guest: build export limit exceeded")

func (a *Agent) export(ctx context.Context, message guestproto.Message, stream *guestproto.Stream) (any, *guestproto.Error) {
	var request guestproto.ExportRequest
	if err := guestproto.UnmarshalPayload(message.Payload, &request); err != nil {
		return nil, guestproto.Errorf(guestproto.CodeBadRequest, "invalid build export request")
	}
	if err := request.Validate(); err != nil || stream == nil || stream.Session() != request.Session {
		return nil, guestproto.Errorf(guestproto.CodeBadRequest, "invalid build export session/share/limits")
	}
	a.mu.Lock()
	if !a.started || !a.sandbox.Build || a.buildBusy {
		a.mu.Unlock()
		return nil, guestproto.Errorf(guestproto.CodeBusy, "build sandbox is unavailable or busy")
	}
	target, ok := a.buildShareTarget(request.Share)
	if ok {
		a.buildBusy = true
	}
	a.mu.Unlock()
	if !ok {
		return nil, guestproto.Errorf(guestproto.CodeNotFound, "unknown build share")
	}
	defer func() { a.mu.Lock(); a.buildBusy = false; a.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	// Close on cancellation to interrupt socket writes, including a peer that
	// stops reading. Never leave a partially exported stream reusable.
	stop := stream.CloseOnCancel(ctx)
	defer stop()
	result, err := exportBuildRoot(ctx, target, request.MaxBytes, request.MaxEntries, stream)
	if err != nil {
		code := guestproto.CodeInternal
		if errors.Is(err, errExportLimit) {
			code = guestproto.CodeUnsupported
		}
		if errors.Is(err, context.Canceled) {
			code = guestproto.CodeCanceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			code = guestproto.CodeDeadlineExceeded
		}
		return nil, guestproto.Errorf(code, "build export failed: %v", err)
	}
	return result, nil
}

type exportWriter struct {
	ctx                context.Context
	writer             io.Writer
	remaining, written int64
}

func (w *exportWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > w.remaining {
		return 0, errExportLimit
	}
	n, err := w.writer.Write(p)
	w.remaining -= int64(n)
	w.written += int64(n)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}

type exportReader struct {
	ctx  context.Context
	file *os.File
}

func (r exportReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.file.Read(p)
}

// exportBuildRoot is agent-owned code. No command, shell, or executable from
// the workload produces the archive. Metadata and content remain untrusted to
// the receiver even after channel authentication.
func exportBuildRoot(ctx context.Context, rootDir string, maxBytes, maxEntries int64, output io.Writer) (guestproto.ExportResult, error) {
	if maxBytes < 1024 || maxBytes > guestproto.MaxExportBytes || maxEntries <= 0 || maxEntries > guestproto.MaxExportEntries {
		return guestproto.ExportResult{}, errExportLimit
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return guestproto.ExportResult{}, err
	}
	defer root.Close()
	rootInfo, err := root.Stat(".")
	if err != nil {
		return guestproto.ExportResult{}, err
	}
	rootHeader, err := tar.FileInfoHeader(rootInfo, "")
	if err != nil {
		return guestproto.ExportResult{}, err
	}
	hash := sha256.New()
	bounded := &exportWriter{ctx: ctx, writer: io.MultiWriter(output, hash), remaining: maxBytes}
	tw := tar.NewWriter(bounded)
	var count int64
	var walk func(string, int) error
	walk = func(dir string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth >= 128 {
			return errExportLimit
		}
		f, err := root.OpenFile(dir, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		defer f.Close()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			batch, readErr := f.ReadDir(128)
			for _, child := range batch {
				if count >= maxEntries {
					return errExportLimit
				}
				name := filepath.Join(dir, child.Name())
				if strings.HasPrefix(child.Name(), ".wh.") {
					return errors.New("guest: reserved OCI whiteout name in build snapshot")
				}
				info, err := root.Lstat(name)
				if err != nil {
					return err
				}
				if !info.IsDir() && !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
					return errors.New("guest: special file in build snapshot")
				}
				link := ""
				if info.Mode()&os.ModeSymlink != 0 {
					link, err = root.Readlink(name)
					if err != nil {
						return err
					}
				}
				header, err := tar.FileInfoHeader(info, link)
				if err != nil {
					return err
				}
				header.Name = filepath.ToSlash(name)
				// Drop host/guest account names; preserve numeric ownership only.
				header.Uname, header.Gname = "", ""
				if info.IsDir() {
					header.Name += "/"
				}
				if header.Size > bounded.remaining {
					return errExportLimit
				}
				if err := tw.WriteHeader(header); err != nil {
					return err
				}
				count++
				if info.Mode().IsRegular() {
					file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
					if err != nil {
						return err
					}
					opened, statErr := file.Stat()
					if statErr != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) || opened.Size() != info.Size() {
						file.Close()
						return errors.New("guest: build snapshot changed while opening")
					}
					_, copyErr := io.CopyN(tw, exportReader{ctx, file}, info.Size())
					closeErr := file.Close()
					if copyErr != nil {
						return copyErr
					}
					if closeErr != nil {
						return closeErr
					}
				}
				if info.IsDir() {
					if err := walk(name, depth+1); err != nil {
						return err
					}
				}
			}
			if readErr == io.EOF {
				return nil
			}
			if readErr != nil {
				return readErr
			}
		}
	}
	if err := walk(".", 0); err != nil {
		return guestproto.ExportResult{}, fmt.Errorf("export snapshot: %w", err)
	}
	if err := tw.Close(); err != nil {
		return guestproto.ExportResult{}, err
	}
	return guestproto.ExportResult{Bytes: bounded.written, Entries: count, SHA256: hex.EncodeToString(hash.Sum(nil)), Root: &guestproto.BuildRootMetadata{UID: uint32(rootHeader.Uid), GID: uint32(rootHeader.Gid), Mode: uint32(rootHeader.Mode)}}, nil
}
