//go:build linux

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/guestproto"
	"github.com/albertize/grillo/internal/observe"
)

func logClient(t *testing.T, result guestproto.LogsResult) *guestproto.Client {
	t.Helper()
	host, guest := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	cfg := guestproto.HandshakeConfig{Key: make([]byte, 32), Capabilities: []guestproto.Capability{guestproto.CapLogs}}
	go func() {
		server, err := guestproto.NewServer(ctx, guest, cfg, guestproto.HandlerFunc(func(_ context.Context, req guestproto.Message, _ *guestproto.Stream) (any, *guestproto.Error) {
			var request guestproto.LogsRequest
			if err := guestproto.UnmarshalPayload(req.Payload, &request); err != nil {
				return nil, guestproto.Errorf(guestproto.CodeBadRequest, "bad cursor")
			}
			if request.After >= result.Next {
				return guestproto.LogsResult{Next: request.After}, nil
			}
			return result, nil
		}))
		if err == nil {
			defer server.Close()
			_ = server.Serve(ctx)
		}
	}()
	client, err := guestproto.NewClient(ctx, host, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestGuestLogIngestionCursorGapAndFailure(t *testing.T) {
	for _, scenario := range []string{"normal", "unknown", "closed"} {
		t.Run(scenario, func(t *testing.T) {
			spool, err := observe.OpenLogSpool(filepath.Join(t.TempDir(), "logs"), 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer spool.Close()
			result := guestproto.LogsResult{Gap: true, Next: 10, Chunks: []guestproto.LogChunk{{Sequence: 10, Container: "app", Stream: "stderr", Data: []byte("live-log")}}}
			if scenario == "unknown" {
				result.Chunks[0].Container = "undeclared"
			}
			if scenario == "closed" {
				_ = spool.Close()
			}
			rt := &sandboxRuntime{id: "demo-api-0", guest: logClient(t, result), containers: map[string]bool{"app": true}, logState: &guestLogState{}}
			e := &Executor{cfg: Config{Logs: spool}}
			err = e.collectLogs(context.Background(), rt)
			if scenario != "normal" {
				if err == nil || rt.logState.cursor != 0 {
					t.Fatal("bad batch acknowledged")
				}
				return
			}
			if err != nil || rt.logState.cursor != 10 {
				t.Fatalf("cursor=%d err=%v", rt.logState.cursor, err)
			}
			if err := e.collectLogs(context.Background(), rt); err != nil {
				t.Fatal(err)
			}
			records, err := spool.List(0, 0, "demo-api-0", "")
			if err != nil || len(records) != 2 {
				t.Fatalf("records=%+v err=%v", records, err)
			}
			filtered, err := spool.List(0, 0, "demo-api-0", "app")
			if err != nil || len(filtered) != 2 || !filtered[0].Gap {
				t.Fatal("container filter concealed loss marker")
			}
			if records[0].Container != "" || !records[0].Gap || records[1].Container != "app" || records[1].Stream != "stderr" || records[1].Line != "live-log" {
				t.Fatal("lost gap/output attribution")
			}
		})
	}
}
