//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/albertize/grillo/internal/guestproto"
)

func TestContainerLogsRetentionAndCursor(t *testing.T) {
	logs := &ContainerLogs{}
	for i := 0; i < 300; i++ {
		_, _ = logs.Writer("app", "stdout").Write([]byte{byte(i)})
	}
	result := logs.Read(0)
	if !result.Gap || len(result.Chunks) != guestproto.MaxLogBatchChunks || result.Chunks[0].Sequence != 45 {
		t.Fatalf("unexpected retained batch: %+v", result)
	}
	result.Chunks[0].Data[0] = 0
	if logs.Read(0).Chunks[0].Data[0] != 44 {
		t.Fatal("caller mutated ring")
	}
	cursor := uint64(0)
	count := 0
	for {
		batch := logs.Read(cursor)
		if len(batch.Chunks) == 0 {
			break
		}
		cursor = batch.Next
		count += len(batch.Chunks)
	}
	if count != 256 || cursor != 300 {
		t.Fatalf("retained %d cursor %d", count, cursor)
	}
	if logs.Read(cursor).Gap {
		t.Fatal("current cursor marked lost")
	}
}

func TestContainerLogsBoundedChunksAndSeparateStreams(t *testing.T) {
	logs := &ContainerLogs{}
	payload := bytes.Repeat([]byte{0xff}, 10<<10)
	if n, err := logs.Writer("app", "stdout").Write(payload); err != nil || n != len(payload) {
		t.Fatalf("write: %d %v", n, err)
	}
	_, _ = logs.Writer("sidecar", "stderr").Write([]byte("partial"))
	result := logs.Read(0)
	if len(result.Chunks) != 4 || result.Gap {
		t.Fatalf("batch: %+v", result)
	}
	var got []byte
	for _, chunk := range result.Chunks[:3] {
		if len(chunk.Data) > guestproto.MaxLogChunkBytes {
			t.Fatal("oversized chunk")
		}
		got = append(got, chunk.Data...)
	}
	if !bytes.Equal(got, payload) || result.Chunks[3].Stream != "stderr" || result.Chunks[3].Container != "sidecar" {
		t.Fatal("lost bytes/stream identity")
	}
}

func TestAgentInitFailureOutputOnlyInLogs(t *testing.T) {
	rt := newFakeRuntime()
	rt.streamOut = []byte("synthetic-private-init-output")
	rt.runExit["setup"] = 1
	agent := NewAgent(rt, t.TempDir())
	spec := testSandbox()
	raw, _ := guestproto.MarshalPayload(guestproto.StartRequest{Sandbox: &spec})
	_, perr := agent.Handle(context.Background(), guestproto.Message{Type: guestproto.TypeStart, Payload: raw}, nil)
	if perr == nil || strings.Contains(perr.Error(), "synthetic-private-init-output") {
		t.Fatal("init failure output exposed as a public error")
	}
	result := agent.logs.Read(0)
	if len(result.Chunks) != 1 || string(result.Chunks[0].Data) != "synthetic-private-init-output" {
		t.Fatal("failed init output missing from workload logs")
	}
}

func TestAgentLogsMalformedRequestAndInitOutput(t *testing.T) {
	rt := newFakeRuntime()
	rt.streamOut = []byte("init-only-log")
	agent := NewAgent(rt, t.TempDir())
	spec := testSandbox()
	raw, _ := guestproto.MarshalPayload(guestproto.StartRequest{Sandbox: &spec})
	if _, err := agent.Handle(context.Background(), guestproto.Message{Type: guestproto.TypeStart, Payload: raw}, nil); err != nil {
		t.Fatal(err)
	}
	value, err := agent.Handle(context.Background(), guestproto.Message{Type: guestproto.TypeLogs}, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(guestproto.LogsResult)
	if len(result.Chunks) != 1 || string(result.Chunks[0].Data) != "init-only-log" {
		t.Fatalf("init logs: %+v", result)
	}
	if _, err := agent.Handle(context.Background(), guestproto.Message{Type: guestproto.TypeLogs, Payload: []byte("{")}, nil); err == nil || err.Code != guestproto.CodeBadRequest {
		t.Fatal("malformed request accepted")
	}
}
