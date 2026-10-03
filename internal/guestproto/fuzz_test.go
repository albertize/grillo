// SPDX-License-Identifier: Apache-2.0

package guestproto

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// FuzzReadFrame checks that arbitrary bytes never panic and that any frame that
// decodes re-encodes to the same frame. Bounds are validated before allocation.
func FuzzReadFrame(f *testing.F) {
	seeds := [][]byte{
		{},
		{0, 0, 0, 1, byte(FramePing)},
		{0, 0, 0, 1, byte(FramePong)},
		{0, 0, 0, 0, byte(FrameControl)},
		{0, 0, 0xff, 0xff, 0xff, 0xff, byte(FrameControl)},
		{0, 0, 0, 6, byte(FrameData), 0, 0, 0, 1, byte(StreamExit)},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		fr, err := readFrame(bytes.NewReader(data))
		if err != nil {
			return
		}
		var buf bytes.Buffer
		if err := writeFrame(&buf, fr); err != nil {
			t.Fatalf("re-encode %+v: %v", fr, err)
		}
		got, err := readFrame(&buf)
		if err != nil {
			t.Fatalf("re-decode: %v", err)
		}
		if got.Kind != fr.Kind || got.Session != fr.Session || got.Stream != fr.Stream || !bytes.Equal(got.Data, fr.Data) {
			t.Fatalf("unstable roundtrip: %+v vs %+v", got, fr)
		}
	})
}

// FuzzReadMessage checks that arbitrary control payloads never panic.
func FuzzReadMessage(f *testing.F) {
	f.Add([]byte(`{"type":"status"}`))
	f.Add([]byte(`{"type":"hello","payload":{"version":{"major":1,"minor":0}}}`))
	f.Add([]byte(`not json`))
	f.Add([]byte{0x00, 0x01, 0x02})
	f.Fuzz(func(t *testing.T, payload []byte) {
		var buf bytes.Buffer
		var hdr [5]byte
		binary.BigEndian.PutUint32(hdr[0:4], uint32(len(payload))+1)
		hdr[4] = byte(FrameControl)
		buf.Write(hdr[:])
		buf.Write(payload)
		msg, err := NewCodec(&buf).ReadMessage()
		if err != nil {
			return
		}
		if msg.Type == "" {
			t.Fatal("decoded message without a type")
		}
	})
}
