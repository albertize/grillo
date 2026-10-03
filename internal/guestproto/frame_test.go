// SPDX-License-Identifier: Apache-2.0

package guestproto

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// oneByteReader yields one byte per Read to force partial I/O.
type oneByteReader struct{ r io.Reader }

func (o *oneByteReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return o.r.Read(p[:1])
}

// shortWriter writes at most one byte per Write.
type shortWriter struct{ w io.Writer }

func (s *shortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return s.w.Write(p[:1])
}

func TestFrameRoundTrip(t *testing.T) {
	frames := []Frame{
		{Kind: FrameControl, Data: []byte(`{"type":"status"}`)},
		{Kind: FrameData, Session: 7, Stream: StreamStdout, Data: []byte("hello")},
		{Kind: FrameData, Session: 7, Stream: StreamExit, Data: encodeExit(3)},
		{Kind: FramePing},
		{Kind: FramePong},
	}
	for _, want := range frames {
		var buf bytes.Buffer
		if err := writeFrame(&buf, want); err != nil {
			t.Fatalf("%+v: %v", want, err)
		}
		got, err := readFrame(&buf)
		if err != nil {
			t.Fatalf("%+v: %v", want, err)
		}
		if got.Kind != want.Kind || got.Session != want.Session || got.Stream != want.Stream || !bytes.Equal(got.Data, want.Data) {
			t.Fatalf("roundtrip = %+v, want %+v", got, want)
		}
	}
}

func TestFramePartialIO(t *testing.T) {
	var buf bytes.Buffer
	want := Frame{Kind: FrameData, Session: 9, Stream: StreamStderr, Data: bytes.Repeat([]byte("x"), 5000)}
	if err := writeFrame(&shortWriter{w: &buf}, want); err != nil {
		t.Fatal(err)
	}
	got, err := readFrame(&oneByteReader{r: &buf})
	if err != nil {
		t.Fatal(err)
	}
	if got.Session != want.Session || got.Stream != want.Stream || !bytes.Equal(got.Data, want.Data) {
		t.Fatalf("partial roundtrip = %+v, want %+v", got, want)
	}
}

func TestFrameOverflowRejected(t *testing.T) {
	declared := []byte(`{"type":"status"}`)
	var hdr [5]byte
	binary.BigEndian.PutUint32(hdr[0:4], MaxControlBytes+100)
	hdr[4] = byte(FrameControl)
	if _, err := readFrame(bytes.NewReader(hdr[:])); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("oversized declared length: err = %v, want ErrFrameTooLarge", err)
	}

	binary.BigEndian.PutUint32(hdr[0:4], 1)
	hdr[4] = 99
	if _, err := readFrame(bytes.NewReader(hdr[:])); !errors.Is(err, ErrUnknownFrame) {
		t.Fatalf("unknown kind: err = %v, want ErrUnknownFrame", err)
	}

	var shortBody [5]byte
	binary.BigEndian.PutUint32(shortBody[0:4], 1)
	shortBody[4] = byte(FrameData)
	if _, err := readFrame(bytes.NewReader(shortBody[:])); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("short data body: err = %v, want ErrCorrupt", err)
	}

	if err := writeFrame(&bytes.Buffer{}, Frame{Kind: FrameControl, Data: declared}); err != nil {
		t.Fatalf("valid control: %v", err)
	}
	if err := writeFrame(&bytes.Buffer{}, Frame{Kind: FrameControl, Data: make([]byte, MaxControlBytes+1)}); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("oversized control write: err = %v, want ErrFrameTooLarge", err)
	}
	if err := writeFrame(&bytes.Buffer{}, Frame{Kind: FrameData, Data: make([]byte, MaxDataBytes+1)}); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("oversized data write: err = %v, want ErrFrameTooLarge", err)
	}
}

// TestFrameBackpressureDeadline shows a non-reading peer blocks the writer until
// the deadline, so memory stays bounded instead of buffering the frame.
func TestFrameBackpressureDeadline(t *testing.T) {
	_, writing := net.Pipe()
	defer writing.Close()
	if err := writing.SetWriteDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	err := writeFrame(writing, Frame{Kind: FrameData, Data: make([]byte, MaxDataBytes)})
	if err == nil {
		t.Fatal("expected a write timeout against a non-reading peer")
	}
	var ne net.Error
	if !errors.As(err, &ne) || !ne.Timeout() {
		t.Fatalf("err = %v, want a timeout net.Error", err)
	}
}
