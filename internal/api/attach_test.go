//go:build linux

// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/guestproto"
)

type attachCore struct {
	fakeCore
	exited chan struct{}
}

func (c *attachCore) ExecAttached(ctx context.Context, _, _ string, req guestproto.ExecRequest, input <-chan guestproto.Frame, stdout, stderr io.Writer) (int, error) {
	if c.exited != nil {
		defer close(c.exited)
		<-ctx.Done()
		return 0, ctx.Err()
	}
	if c.err != nil {
		return 0, c.err
	}
	if !req.Stdin {
		return 0, errors.New("stdin omitted")
	}
	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case fr := <-input:
			if fr.Stream == guestproto.StreamStdin {
				if len(fr.Data) == 0 {
					return 23, nil
				}
				if _, err := stdout.Write(fr.Data); err != nil {
					return 0, err
				}
				if _, err := stderr.Write([]byte("stderr")); err != nil {
					return 0, err
				}
			}
		}
	}
}
func TestAttachDuplexResultAndRedactedErrors(t *testing.T) {
	for _, failure := range []bool{false, true} {
		core := &attachCore{}
		if failure {
			core.err = errors.New("synthetic-private-value")
		}
		socket, stop := startServer(t, Options{Core: core})
		client := NewClient(socket)
		input := make(chan guestproto.Frame, 2)
		input <- guestproto.Frame{Stream: guestproto.StreamStdin, Data: []byte("literal-input")}
		input <- guestproto.Frame{Stream: guestproto.StreamStdin}
		var stdout, stderr bytes.Buffer
		code, err := client.ExecAttached(context.Background(), AttachRequest{Application: "demo", Container: "app", Args: []string{"cat"}}, input, &stdout, &stderr)
		stop()
		if failure {
			if err == nil || strings.Contains(err.Error(), "synthetic-private-value") {
				t.Fatal("attach error absent/unredacted")
			}
			continue
		}
		if err != nil || code != 23 || stdout.String() != "literal-input" || stderr.String() != "stderr" {
			t.Fatalf("code=%d err=%v stdout=%q stderr=%q", code, err, stdout.String(), stderr.String())
		}
	}
}
func TestAttachCancellationAndDaemonShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		core := &attachCore{exited: make(chan struct{})}
		socket, stop := startServer(t, Options{Core: core})
		defer stop()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			_, err := NewClient(socket).ExecAttached(ctx, AttachRequest{Application: "demo", Container: "app", Args: []string{"sleep"}}, make(chan guestproto.Frame), io.Discard, io.Discard)
			done <- err
		}()
		time.Sleep(100 * time.Millisecond)
		if shutdown {
			stop()
		} else {
			cancel()
		}
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("truncated attach treated as success")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("attach did not cancel")
		}
		select {
		case <-core.exited:
		case <-time.After(3 * time.Second):
			t.Fatal("core session survived disconnect")
		}
	}
}
func TestAttachRejectsUnknownTrailingAndOversizedBodies(t *testing.T) {
	handler := NewServer(Options{Core: &attachCore{}}).Handler()
	for _, body := range []string{`{"application":"demo","container":"app","args":["cat"],"unknown":true}`, `{"application":"demo","container":"app","args":["cat"]} {}`, strings.Repeat("x", 65<<10)} {
		req := httptest.NewRequest(http.MethodPost, "/v1/exec-attach", strings.NewReader(body))
		req.Header.Set("Upgrade", "grillo-attach-v1")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != 400 {
			t.Fatalf("body accepted: %d", response.Code)
		}
	}
}
