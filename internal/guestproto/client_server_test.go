// SPDX-License-Identifier: Apache-2.0

package guestproto

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"
)

func testHandler() Handler {
	return HandlerFunc(func(ctx context.Context, req Message, stream *Stream) (any, *Error) {
		switch req.Type {
		case TypeStatus:
			return StatusResult{
				State:      "running",
				UptimeMS:   42,
				Containers: []ContainerStatus{{Name: "app", State: "running"}},
			}, nil
		case TypeProbe:
			var pr ProbeRequest
			if err := UnmarshalPayload(req.Payload, &pr); err != nil {
				return nil, Errorf(CodeBadRequest, "%v", err)
			}
			return ProbeResult{Healthy: true, Detail: string(pr.Kind)}, nil
		case TypeStart:
			return StartResult{State: "started"}, nil
		case TypeStop:
			return StopResult{Stopped: true}, nil
		case TypeExec:
			var er ExecRequest
			if err := UnmarshalPayload(req.Payload, &er); err != nil {
				return nil, Errorf(CodeBadRequest, "%v", err)
			}
			_, _ = stream.WriteStdout([]byte("hello "))
			_, _ = stream.WriteStdout([]byte("world"))
			_, _ = stream.WriteStderr([]byte("warn"))
			if er.TTY {
				_ = stream.Resize(24, 80)
			}
			_ = stream.Exit(7)
			return ExecResult{ExitCode: 7}, nil
		default:
			return nil, Errorf(CodeUnsupported, "type %s", req.Type)
		}
	})
}

func newPair(t *testing.T, handler Handler) *Client {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	cfg := HandshakeConfig{
		Sandbox:      "sbx-1",
		Key:          testKey(1),
		Agent:        "agent",
		Capabilities: []Capability{CapExec, CapProbe},
	}
	srvCh := make(chan *Server, 1)
	go func() {
		s, err := NewServer(context.Background(), serverConn, cfg, handler)
		if err != nil {
			return
		}
		srvCh <- s
		_ = s.Serve(context.Background())
	}()
	client, err := NewClient(context.Background(), clientConn, cfg)
	if err != nil {
		t.Fatal(err)
	}
	server := <-srvCh
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	return client
}

func TestClientServerRoundtrip(t *testing.T) {
	client := newPair(t, testHandler())
	ctx := context.Background()

	if err := client.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	status, err := client.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.State != "running" || status.UptimeMS != 42 || len(status.Containers) != 1 {
		t.Fatalf("status = %+v", status)
	}
	probe, err := client.Probe(ctx, ProbeRequest{Kind: ProbeHTTP, URL: "http://127.0.0.1/"})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !probe.Healthy || probe.Detail != "http" {
		t.Fatalf("probe = %+v", probe)
	}
	start, err := client.Start(ctx, StartRequest{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if start.State != "started" {
		t.Fatalf("start = %+v", start)
	}
	stop, err := client.Stop(ctx, StopRequest{TimeoutMS: 100})
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !stop.Stopped {
		t.Fatalf("stop = %+v", stop)
	}
}

func TestClientExecStreaming(t *testing.T) {
	client := newPair(t, testHandler())
	var stdout, stderr bytes.Buffer
	result, err := client.Exec(context.Background(), ExecRequest{Args: []string{"echo"}, TTY: true}, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "hello world" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.String() != "warn" {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if result.ExitCode != 7 {
		t.Fatalf("exit = %d", result.ExitCode)
	}
}

func TestClientTypedError(t *testing.T) {
	handler := HandlerFunc(func(context.Context, Message, *Stream) (any, *Error) {
		return nil, Errorf(CodeUnsupported, "not here")
	})
	client := newPair(t, handler)
	_, err := client.Status(context.Background())
	pe, ok := AsError(err)
	if !ok || pe.Code != CodeUnsupported {
		t.Fatalf("err = %v, want unsupported", err)
	}
}

func TestClientTimeout(t *testing.T) {
	handler := HandlerFunc(func(ctx context.Context, _ Message, _ *Stream) (any, *Error) {
		<-ctx.Done()
		return nil, Errorf(CodeCanceled, "canceled")
	})
	client := newPair(t, handler)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := client.Status(ctx)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	// The client deadline and the guest's proactive cancel can race; accept
	// either, but the call must return promptly.
	if !errors.Is(err, context.DeadlineExceeded) {
		if pe, ok := AsError(err); !ok || (pe.Code != CodeCanceled && pe.Code != CodeDeadlineExceeded) {
			t.Fatalf("err = %v, want a deadline or cancel error", err)
		}
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timeout returned after %v", elapsed)
	}
}

// TestServerCancelMessage drives the raw protocol to send a request and then a
// cancel, verifying the guest handler observes cancellation.
func TestServerCancelMessage(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	cfg := HandshakeConfig{Sandbox: "sbx-1", Key: testKey(1)}
	started := make(chan struct{})
	handler := HandlerFunc(func(ctx context.Context, _ Message, _ *Stream) (any, *Error) {
		close(started)
		<-ctx.Done()
		return nil, Errorf(CodeCanceled, "canceled by client")
	})
	srvCh := make(chan *Server, 1)
	go func() {
		s, err := NewServer(context.Background(), serverConn, cfg, handler)
		if err != nil {
			return
		}
		srvCh <- s
		_ = s.Serve(context.Background())
	}()
	client, err := NewClient(context.Background(), clientConn, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := <-srvCh
	defer server.Close()

	raw := NewCodec(clientConn)
	payload, err := MarshalPayload(ExecRequest{Session: 1, Args: []string{"sleep"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.WriteMessage(Message{ID: "x1", Type: TypeExec, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	<-started
	cancelPayload, err := MarshalPayload(CancelRequest{Target: "x1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.WriteMessage(Message{ID: "x2", Type: TypeCancel, Payload: cancelPayload}); err != nil {
		t.Fatal(err)
	}

	if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for {
		fr, err := raw.ReadFrame()
		if err != nil {
			t.Fatalf("read reply: %v", err)
		}
		if fr.Kind != FrameControl {
			continue
		}
		var m Message
		if err := json.Unmarshal(fr.Data, &m); err != nil {
			t.Fatal(err)
		}
		if m.ID != "x1" {
			continue
		}
		if m.Type != TypeError || m.Error == nil || m.Error.Code != CodeCanceled {
			t.Fatalf("reply = %+v, want canceled error", m)
		}
		return
	}
}
