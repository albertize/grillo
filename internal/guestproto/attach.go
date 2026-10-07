// SPDX-License-Identifier: Apache-2.0

package guestproto

import "encoding/binary"

// SizeFrame represents a terminal window update in the bounded stream codec.
func SizeFrame(rows, cols uint16) Frame {
	data := make([]byte, 4)
	binary.BigEndian.PutUint16(data, rows)
	binary.BigEndian.PutUint16(data[2:], cols)
	return Frame{Kind: FrameData, Stream: StreamResize, Data: data}
}
func DecodeSize(data []byte) (uint16, uint16) {
	if len(data) != 4 {
		return 0, 0
	}
	return binary.BigEndian.Uint16(data), binary.BigEndian.Uint16(data[2:])
}
