// SPDX-License-Identifier: Apache-2.0

package guestproto

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	MaxExportBytes   int64 = 8 << 30
	MaxExportEntries int64 = 100000
)

// ExportRequest selects only an agent-mounted build share, never an arbitrary
// guest path. Limits are mandatory and cover the complete uncompressed tar.
type ExportRequest struct {
	Session    uint32 `json:"session"`
	Share      string `json:"share"`
	MaxBytes   int64  `json:"maxBytes"`
	MaxEntries int64  `json:"maxEntries"`
}

func (r ExportRequest) Validate() error {
	if r.Session == 0 || r.Share == "" || len(r.Share) > 128 || r.MaxBytes < 1024 || r.MaxBytes > MaxExportBytes || r.MaxEntries <= 0 || r.MaxEntries > MaxExportEntries {
		return errors.New("guestproto: invalid build export session/share/limits")
	}
	return nil
}

// ExportResult authenticates completion through the established control
// channel. SHA256 is a checksum of all tar bytes, including its trailer.
type ExportResult struct {
	Bytes   int64              `json:"bytes"`
	Entries int64              `json:"entries"`
	SHA256  string             `json:"sha256"`
	Root    *BuildRootMetadata `json:"root,omitempty"`
}

type BuildRootMetadata struct {
	UID  uint32 `json:"uid"`
	GID  uint32 `json:"gid"`
	Mode uint32 `json:"mode"`
}

// Export streams a bounded snapshot to a local writer. On transfer failure it
// closes this dedicated connection to cancel the guest and discard stale frames.
// The caller must separately validate/extract the archive before publication.
func (c *Client) Export(ctx context.Context, req ExportRequest, output io.Writer) (ExportResult, error) {
	if !c.Info().Supports(CapBuildExport) {
		return ExportResult{}, Errorf(CodeUnsupported, "guest lacks build-export; rebuild the agent")
	}
	if req.Session == 0 {
		req.Session = newSessionID()
	}
	if err := req.Validate(); err != nil {
		return ExportResult{}, err
	}
	if output == nil {
		return ExportResult{}, errors.New("guestproto: export requires an output writer")
	}
	hash := sha256.New()
	var received int64
	onFrame := func(frame Frame) error {
		if frame.Session != req.Session || frame.Stream != StreamStdout || len(frame.Data) == 0 {
			return fmt.Errorf("%w: unexpected export frame", ErrCorrupt)
		}
		if int64(len(frame.Data)) > req.MaxBytes-received {
			return fmt.Errorf("%w: export byte limit", ErrCorrupt)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := output.Write(frame.Data)
		if err != nil {
			return err
		}
		if n != len(frame.Data) {
			return io.ErrShortWrite
		}
		received += int64(n)
		_, _ = hash.Write(frame.Data)
		return nil
	}
	m, err := c.call(ctx, TypeExport, req, onFrame)
	if err != nil {
		_ = c.Close()
		// A guest may close on the shared deadline just before the host's
		// context timer fires. Preserve the operation's deadline error.
		if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
			err = context.DeadlineExceeded
		}
		return ExportResult{}, err
	}
	var result ExportResult
	if m.Type != TypeExportResult {
		_ = c.Close()
		return ExportResult{}, fmt.Errorf("%w: invalid export response", ErrCorrupt)
	}
	if err := UnmarshalPayload(m.Payload, &result); err != nil {
		_ = c.Close()
		return ExportResult{}, err
	}
	if result.Root != nil && result.Root.Mode > 07777 {
		_ = c.Close()
		return ExportResult{}, fmt.Errorf("%w: invalid export root metadata", ErrCorrupt)
	}
	if received < 1024 || result.Bytes != received || result.Entries < 0 || result.Entries > req.MaxEntries || result.SHA256 != hex.EncodeToString(hash.Sum(nil)) {
		_ = c.Close()
		return ExportResult{}, fmt.Errorf("%w: invalid export completion receipt", ErrCorrupt)
	}
	return result, nil
}
