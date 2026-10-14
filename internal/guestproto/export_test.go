// SPDX-License-Identifier: Apache-2.0

package guestproto

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
	"time"
)

func exportReceipt(data []byte, entries int64) ExportResult {
	sum := sha256.Sum256(data)
	return ExportResult{Bytes: int64(len(data)), Entries: entries, SHA256: hex.EncodeToString(sum[:])}
}

func TestClientExportChecksCapabilityLimitsAndReceipt(t *testing.T) {
	data := make([]byte, 1024)
	request := ExportRequest{Share: "build", MaxBytes: 1024, MaxEntries: 1}
	for _, scenario := range []string{"success", "bytes", "hash", "entries", "frame", "oversize", "missing", "writer"} {
		t.Run(scenario, func(t *testing.T) {
			handler := HandlerFunc(func(ctx context.Context, message Message, stream *Stream) (any, *Error) {
				var req ExportRequest
				if err := UnmarshalPayload(message.Payload, &req); err != nil {
					return nil, Errorf(CodeBadRequest, "decode")
				}
				switch scenario {
				case "frame":
					_, _ = stream.WriteStderr(data)
				case "oversize":
					_, _ = stream.Write(append(bytes.Clone(data), 1))
				case "missing":
					return nil, Errorf(CodeInternal, "failed after sending data")
				default:
					_, _ = stream.Write(data)
				}
				result := exportReceipt(data, 1)
				switch scenario {
				case "bytes":
					result.Bytes--
				case "hash":
					result.SHA256 = "wrong"
				case "entries":
					result.Entries = 2
				}
				return result, nil
			})
			client := newPairCapabilities(t, handler, []Capability{CapBuildExport})
			var out bytes.Buffer
			var writer io.Writer = &out
			if scenario == "writer" {
				writer = failedExportWriter{}
			}
			result, err := client.Export(context.Background(), request, writer)
			if scenario == "success" {
				if err != nil || result.Bytes != 1024 || out.Len() != 1024 {
					t.Fatal("export", result, err)
				}
			} else {
				if err == nil || !client.closed.Load() {
					t.Fatal("failed export left reusable connection", err)
				}
				if scenario == "oversize" && out.Len() != 0 {
					t.Fatal("oversized frame written to destination")
				}
			}
		})
	}
	client := newPair(t, testHandler())
	if _, err := client.Export(context.Background(), request, io.Discard); err == nil {
		t.Fatal("missing capability accepted")
	}
	if client.closed.Load() {
		t.Fatal("capability rejection closed idle client")
	}
	for _, req := range []ExportRequest{{}, {Session: 1, Share: "build", MaxBytes: MaxExportBytes + 1, MaxEntries: 1}, {Session: 1, Share: "build", MaxBytes: 1024, MaxEntries: MaxExportEntries + 1}} {
		if req.Validate() == nil {
			t.Fatal("invalid export policy accepted")
		}
	}
}

type failedExportWriter struct{}

func (failedExportWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestClientExportWholeOperationDeadline(t *testing.T) {
	client := newPairCapabilities(t, HandlerFunc(func(ctx context.Context, _ Message, stream *Stream) (any, *Error) {
		stop := stream.CloseOnCancel(ctx)
		defer stop()
		<-ctx.Done()
		return nil, Errorf(CodeCanceled, "canceled")
	}), []Capability{CapBuildExport})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := client.Export(ctx, ExportRequest{Share: "build", MaxBytes: 1024, MaxEntries: 1}, io.Discard); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("operation deadline", err)
	}
	if !client.closed.Load() {
		t.Fatal("timed-out export connection retained")
	}
}

func TestExportCancellationInterruptsNonReadingPeer(t *testing.T) {
	done := make(chan error, 1)
	client := newPairCapabilities(t, HandlerFunc(func(ctx context.Context, _ Message, stream *Stream) (any, *Error) {
		stop := stream.CloseOnCancel(ctx)
		defer stop()
		_, err := stream.Write(make([]byte, MaxDataBytes))
		done <- err
		return nil, Errorf(CodeCanceled, "canceled")
	}), []Capability{CapBuildExport})
	deadline := time.Now().Add(30 * time.Millisecond)
	raw, _ := MarshalPayload(ExportRequest{Session: 1, Share: "build", MaxBytes: MaxExportBytes, MaxEntries: 1})
	if err := client.writeMessage(Message{ID: "export-stalled", Type: TypeExport, Deadline: &deadline, Payload: raw}); err != nil {
		t.Fatal(err)
	}
	// Do not read any response frame: the server write must be interrupted.
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("blocked writer succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("non-reading peer escaped operation deadline")
	}
}

func TestBuildRunRejectsUnexpectedStreamOrCompletion(t *testing.T) {
	for _, scenario := range []string{"stream", "session", "completion"} {
		t.Run(scenario, func(t *testing.T) {
			client := newPair(t, HandlerFunc(func(_ context.Context, req Message, stream *Stream) (any, *Error) {
				switch scenario {
				case "stream":
					_, _ = stream.WriteStream(StreamStdin, []byte("x"))
				case "session":
					_ = stream.server.writeFrame(Frame{Kind: FrameData, Session: stream.Session() + 1, Stream: StreamStdout, Data: []byte("x")})
				case "completion":
					raw, _ := MarshalPayload(RunResult{})
					_ = stream.server.writeMessage(Message{ID: req.ID, Type: TypeStatusResult, Payload: raw})
				}
				return RunResult{}, nil
			}))
			if _, err := client.Run(context.Background(), RunRequest{Share: "build", Args: []string{"true"}}, io.Discard, io.Discard); err == nil || !client.closed.Load() {
				t.Fatal("inconsistent RUN accepted", err)
			}
		})
	}
}
