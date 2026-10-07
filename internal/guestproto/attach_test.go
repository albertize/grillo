// SPDX-License-Identifier: Apache-2.0

package guestproto

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestAttachedInputResizeAndEOF(t *testing.T) {
	client := newPair(t, HandlerFunc(func(ctx context.Context, req Message, stream *Stream) (any, *Error) {
		var data []byte
		resized := false
		for {
			select {
			case <-ctx.Done():
				return nil, Errorf(CodeCanceled, "cancelled")
			case fr := <-stream.Input():
				if fr.Stream == StreamResize {
					rows, cols := DecodeSize(fr.Data)
					resized = rows == 31 && cols == 97
				}
				if fr.Stream == StreamStdin {
					if len(fr.Data) == 0 {
						if !resized {
							return nil, Errorf(CodeBadRequest, "resize absent")
						}
						_, _ = stream.WriteStdout(data)
						return ExecResult{ExitCode: 7}, nil
					}
					data = append(data, fr.Data...)
				}
			}
		}
	}))
	input := make(chan Frame, 3)
	input <- SizeFrame(31, 97)
	input <- Frame{Stream: StreamStdin, Data: []byte("literal-input")}
	input <- Frame{Stream: StreamStdin}
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := client.ExecAttached(ctx, ExecRequest{Container: "app", Args: []string{"cat"}, Stdin: true}, input, &out, nil)
	if err != nil || result.ExitCode != 7 || out.String() != "literal-input" {
		t.Fatalf("result=%+v error=%v output=%q", result, err, out.String())
	}
}
func TestAttachedMalformedResizeFails(t *testing.T) {
	client := newPair(t, HandlerFunc(func(ctx context.Context, _ Message, _ *Stream) (any, *Error) {
		<-ctx.Done()
		return nil, Errorf(CodeCanceled, "cancelled")
	}))
	input := make(chan Frame, 1)
	input <- Frame{Stream: StreamResize, Data: []byte{1}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := client.ExecAttached(ctx, ExecRequest{Container: "app", Args: []string{"sleep"}, Stdin: true}, input, nil, nil); err == nil {
		t.Fatal("malformed resize accepted")
	}
}
