// SPDX-License-Identifier: Apache-2.0

// Package guestproto implements the host/guest control protocol described in
// api/guest-protocol.md and spec section 6.4. It separates the framing codec from
// the transport: the codec reads and writes bounded frames over any io.Reader and
// io.Writer, and a Conn (net.Conn, an AF_VSOCK socket, or an in-memory pipe)
// carries them. Nothing here boots a VM or runs a guest; that is T07/T08.
package guestproto

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	// MaxControlBytes bounds a JSON control message payload (spec 6.4: 1 MiB).
	MaxControlBytes = 1 << 20
	// MaxDataBytes bounds a binary stream frame payload (spec 6.4: 64 KiB).
	MaxDataBytes = 64 << 10

	// headerBytes is a uint32 body length plus a uint8 frame kind.
	headerBytes = 5
)

// Framing errors. These are stable so callers can map them to diagnostics.
var (
	// ErrFrameTooLarge means a declared frame length exceeds its kind's bound.
	ErrFrameTooLarge = errors.New("guestproto: frame exceeds limit")
	// ErrUnknownFrame means the frame kind is not recognized.
	ErrUnknownFrame = errors.New("guestproto: unknown frame kind")
	// ErrCorrupt means a frame body is structurally invalid.
	ErrCorrupt = errors.New("guestproto: malformed frame")
)

// FrameKind identifies a frame family.
type FrameKind uint8

const (
	// FrameControl carries a length-prefixed JSON control message.
	FrameControl FrameKind = 1
	// FrameData carries a binary stream chunk.
	FrameData FrameKind = 2
	// FramePing is a heartbeat request and needs no body.
	FramePing FrameKind = 3
	// FramePong is a heartbeat reply and needs no body.
	FramePong FrameKind = 4
)

// StreamKind identifies the stream a data frame belongs to.
type StreamKind uint8

const (
	// StreamStdin carries client input to a session.
	StreamStdin StreamKind = 1
	// StreamStdout carries session standard output.
	StreamStdout StreamKind = 2
	// StreamStderr carries session standard error.
	StreamStderr StreamKind = 3
	// StreamResize carries a terminal window size (rows uint16, cols uint16).
	StreamResize StreamKind = 4
	// StreamExit carries the final exit code (int32 big-endian, empty means 0).
	StreamExit StreamKind = 5
)

func bodyLimit(kind FrameKind) (int, bool) {
	switch kind {
	case FrameControl:
		return MaxControlBytes, true
	case FrameData:
		// session uint32 + stream uint8 + payload
		return 5 + MaxDataBytes, true
	case FramePing, FramePong:
		return 0, true
	default:
		return 0, false
	}
}

// Frame is one decoded wire frame.
type Frame struct {
	Kind    FrameKind
	Session uint32
	Stream  StreamKind
	Data    []byte
}

// writeFrame encodes f to w, handling short writes.
func writeFrame(w io.Writer, f Frame) error {
	limit, ok := bodyLimit(f.Kind)
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownFrame, f.Kind)
	}
	var body []byte
	switch f.Kind {
	case FrameControl:
		body = f.Data
	case FrameData:
		body = make([]byte, 5+len(f.Data))
		binary.BigEndian.PutUint32(body[0:4], f.Session)
		body[4] = byte(f.Stream)
		copy(body[5:], f.Data)
	}
	if len(body) > limit {
		return fmt.Errorf("%w: %d > %d", ErrFrameTooLarge, len(body), limit)
	}
	var hdr [headerBytes]byte
	binary.BigEndian.PutUint32(hdr[0:4], uint32(len(body))+1)
	hdr[4] = byte(f.Kind)
	if err := writeFull(w, hdr[:]); err != nil {
		return err
	}
	return writeFull(w, body)
}

// readFrame decodes one frame from r. The declared length is validated against
// the kind's bound before any allocation.
func readFrame(r io.Reader) (Frame, error) {
	var hdr [headerBytes]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Frame{}, err
	}
	total := binary.BigEndian.Uint32(hdr[0:4])
	kind := FrameKind(hdr[4])
	limit, ok := bodyLimit(kind)
	if !ok {
		return Frame{}, fmt.Errorf("%w: %d", ErrUnknownFrame, kind)
	}
	if total < 1 || total > uint32(limit)+1 {
		return Frame{}, fmt.Errorf("%w: %d bytes for kind %d", ErrFrameTooLarge, total, kind)
	}
	body := make([]byte, int(total)-1)
	if _, err := io.ReadFull(r, body); err != nil {
		return Frame{}, err
	}
	switch kind {
	case FrameControl:
		return Frame{Kind: kind, Data: body}, nil
	case FrameData:
		if len(body) < 5 {
			return Frame{}, fmt.Errorf("%w: data frame body %d bytes", ErrCorrupt, len(body))
		}
		return Frame{
			Kind:    kind,
			Session: binary.BigEndian.Uint32(body[0:4]),
			Stream:  StreamKind(body[4]),
			Data:    body[5:],
		}, nil
	default:
		return Frame{Kind: kind}, nil
	}
}

func writeFull(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if n > 0 {
			p = p[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
