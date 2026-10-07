// SPDX-License-Identifier: Apache-2.0

package guestproto

import (
	"context"
	"testing"
)

func TestLogsClientValidation(t *testing.T) {
	valid := LogsResult{Next: 1, Chunks: []LogChunk{{Sequence: 1, Container: "app", Stream: "stdout", Data: []byte("hello")}}}
	cases := []struct {
		name   string
		result LogsResult
		bad    bool
	}{
		{"valid", valid, false},
		{"empty", LogsResult{}, false},
		{"cursor", LogsResult{Next: 3}, true},
		{"sequence", LogsResult{Next: 0, Chunks: []LogChunk{{Container: "app", Stream: "stdout"}}}, true},
		{"stream", LogsResult{Next: 1, Chunks: []LogChunk{{Sequence: 1, Container: "app", Stream: "stdin"}}}, true},
		{"oversized", LogsResult{Next: 1, Chunks: []LogChunk{{Sequence: 1, Container: "app", Stream: "stdout", Data: make([]byte, MaxLogChunkBytes+1)}}}, true},
		{"batch", LogsResult{Chunks: make([]LogChunk, MaxLogBatchChunks+1)}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newPair(t, HandlerFunc(func(_ context.Context, req Message, _ *Stream) (any, *Error) {
				if req.Type != TypeLogs {
					t.Errorf("type: %s", req.Type)
				}
				return tc.result, nil
			}))
			result, err := client.Logs(context.Background(), 0)
			if (err != nil) != tc.bad {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}
