// SPDX-License-Identifier: Apache-2.0

package guestproto

import (
	"encoding/json"
	"fmt"
	"io"
)

// Codec encodes and decodes frames over a reader and a writer. It holds no
// transport state and is safe to use over any stream, including an in-memory
// pipe in tests. Callers own concurrency: do not read and write the same Codec
// from multiple goroutines without external synchronization.
type Codec struct {
	r io.Reader
	w io.Writer
}

// NewCodec returns a Codec that reads and writes on rw.
func NewCodec(rw io.ReadWriter) *Codec { return &Codec{r: rw, w: rw} }

// NewCodecReaderWriter returns a Codec over separate reader and writer.
func NewCodecReaderWriter(r io.Reader, w io.Writer) *Codec { return &Codec{r: r, w: w} }

// ReadFrame reads one frame.
func (c *Codec) ReadFrame() (Frame, error) { return readFrame(c.r) }

// WriteFrame writes one frame, handling short writes.
func (c *Codec) WriteFrame(f Frame) error { return writeFrame(c.w, f) }

// WriteMessage encodes m as a JSON control frame.
func (c *Codec) WriteMessage(m Message) error {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("guestproto: encode %s: %w", m.Type, err)
	}
	if len(data) > MaxControlBytes {
		return fmt.Errorf("%w: control message %d bytes", ErrFrameTooLarge, len(data))
	}
	return c.WriteFrame(Frame{Kind: FrameControl, Data: data})
}

// ReadMessage reads a control frame and decodes it. Non-control frames are an
// error; callers that expect stream data must use ReadFrame.
func (c *Codec) ReadMessage() (Message, error) {
	fr, err := c.ReadFrame()
	if err != nil {
		return Message{}, err
	}
	if fr.Kind != FrameControl {
		return Message{}, fmt.Errorf("%w: got frame kind %d, want control", ErrCorrupt, fr.Kind)
	}
	var m Message
	if err := json.Unmarshal(fr.Data, &m); err != nil {
		return Message{}, fmt.Errorf("%w: decode control frame: %v", ErrCorrupt, err)
	}
	if m.Type == "" {
		return Message{}, fmt.Errorf("%w: control message without a type", ErrCorrupt)
	}
	return m, nil
}
