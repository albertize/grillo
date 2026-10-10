//go:build linux

// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/api"
	"github.com/albertize/grillo/internal/guestproto"
)

// Deterministic transport fixture, not evidence of a guest PTY.
func (c *richCore) ExecAttached(ctx context.Context, request api.AttachRequest, input <-chan guestproto.Frame, stdout, stderr io.Writer) (int, error) {
	if c.fail {
		return 0, errors.New("private-backend-token")
	}
	if !request.TTY || len(request.Args) != 3 || request.Args[0] != "/bin/sh" || request.Args[1] != "-c" || request.Args[2] != "export TERM=xterm-256color; exec /bin/sh -i" || request.Rows == 0 || request.Cols == 0 {
		return 0, io.ErrUnexpectedEOF
	}
	if _, err := io.WriteString(stdout, "<img src=x onerror=window.pwned=true>\r\n$ "); err != nil {
		return 0, err
	}
	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case frame := <-input:
			if frame.Stream == guestproto.StreamResize {
				if len(frame.Data) != 4 {
					return 0, io.ErrUnexpectedEOF
				}
				continue
			}
			if string(frame.Data) == "exit\r" {
				return 7, nil
			}
			if _, err := stdout.Write(frame.Data); err != nil {
				return 0, err
			}
		}
	}
}
func terminalRequest(t *testing.T, server *httptest.Server, b *Bridge, path, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", server.URL)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: b.session})
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
func terminalEvent(t *testing.T, reader *bufio.Reader) map[string]any {
	t.Helper()
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	return event
}
func TestTerminalDuplexExitAndCancellation(t *testing.T) {
	b := New(&richCore{})
	server := httptest.NewServer(b.Handler())
	defer server.Close()
	start := func() (*http.Response, *bufio.Reader, string) {
		response := terminalRequest(t, server, b, "/v1/terminal", `{"application":"demo","container":"pod/app","rows":24,"cols":80}`)
		if response.StatusCode != 200 {
			t.Fatalf("start: %d", response.StatusCode)
		}
		reader := bufio.NewReader(response.Body)
		event := terminalEvent(t, reader)
		id := event["id"].(string)
		event = terminalEvent(t, reader)
		data, _ := base64.StdEncoding.DecodeString(event["data"].(string))
		if !strings.Contains(string(data), "<img") {
			t.Fatal("missing output")
		}
		return response, reader, id
	}
	response, reader, id := start()
	for _, body := range []string{`{"rows":30,"cols":100}`, `{"data":"hello\r"}`} {
		resp := terminalRequest(t, server, b, "/v1/terminal/"+id+"/input", body)
		resp.Body.Close()
		if resp.StatusCode != 204 {
			t.Fatalf("input: %d", resp.StatusCode)
		}
	}
	event := terminalEvent(t, reader)
	data, _ := base64.StdEncoding.DecodeString(event["data"].(string))
	if string(data) != "hello\r" {
		t.Fatalf("input not propagated: %q", data)
	}
	for _, body := range []string{`{"bytes":"!"}`, `{"bytes":"AJv/","data":"mixed"}`, `{"bytes":"AJv/","rows":24,"cols":80}`, `{"bytes":""}`} {
		r := terminalRequest(t, server, b, "/v1/terminal/"+id+"/input", body)
		r.Body.Close()
		if r.StatusCode != 400 {
			t.Fatalf("unsafe binary input accepted: %d", r.StatusCode)
		}
	}
	binaryResponse := terminalRequest(t, server, b, "/v1/terminal/"+id+"/input", `{"bytes":"AJv/"}`)
	binaryResponse.Body.Close()
	if binaryResponse.StatusCode != 204 {
		t.Fatal("binary input rejected")
	}
	binaryEvent := terminalEvent(t, reader)
	if binaryEvent["data"] != "AJv/" {
		t.Fatal("binary stdin corrupted")
	}
	resp := terminalRequest(t, server, b, "/v1/terminal/"+id+"/input", `{"data":"exit\r"}`)
	resp.Body.Close()
	event = terminalEvent(t, reader)
	if event["type"] != "exit" || event["exitCode"] != float64(7) {
		t.Fatalf("exit: %v", event)
	}
	response.Body.Close()
	response, _, id = start()
	response.Body.Close() // output disconnect owns cancellation
	deadline := time.Now().Add(2 * time.Second)
	for {
		b.mu.Lock()
		count := len(b.terminals)
		b.mu.Unlock()
		if count == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("disconnected session leaked")
		}
		time.Sleep(time.Millisecond)
	}
	resp = terminalRequest(t, server, b, "/v1/terminal/"+id+"/close", `{}`)
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatal("close not idempotent")
	}
}
func TestTerminalRejectsUnsafeRequestsAndBounds(t *testing.T) {
	b := New(&richCore{})
	handler := b.Handler()
	for _, tc := range []struct {
		path, body, origin, content string
		auth                        bool
		want                        int
	}{
		{"/v1/terminal", `{}`, "http://localhost", "application/json", false, 401},
		{"/v1/terminal", `{}`, "http://evil.test", "application/json", true, 403},
		{"/v1/terminal", `{}`, "", "application/json", true, 403},
		{"/v1/terminal", `{}`, "http://localhost", "text/plain", true, 415},
		{"/v1/terminal", `{"application":"demo","container":"pod/app","rows":0,"cols":80}`, "http://localhost", "application/json", true, 400},
		{"/v1/terminal", `{"application":"demo","container":"pod/app","rows":24,"cols":80,"args":["evil"]}`, "http://localhost", "application/json", true, 400},
		{"/v1/terminal/missing/input", `{"data":"x","rows":24,"cols":80}`, "http://localhost", "application/json", true, 400},
		{"/v1/terminal/missing/input", `{"data":"` + strings.Repeat("x", 8193) + `"}`, "http://localhost", "application/json", true, 400},
		{"/v1/terminal/missing/input", `{"data":"x"}`, "http://localhost", "application/json", true, 404},
	} {
		req := httptest.NewRequest("POST", "http://localhost"+tc.path, strings.NewReader(tc.body))
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Content-Type", tc.content)
		if tc.auth {
			req.AddCookie(&http.Cookie{Name: sessionCookie, Value: b.session})
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != tc.want {
			t.Fatalf("%s: got %d want %d", tc.path, response.Code, tc.want)
		}
	}
	for i := 0; i < cap(b.execs); i++ {
		b.execs <- struct{}{}
	}
	req := httptest.NewRequest("POST", "http://localhost/v1/terminal", strings.NewReader(`{"application":"demo","container":"pod/app","rows":24,"cols":80}`))
	req.Header.Set("Origin", "http://localhost")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: b.session})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != 429 {
		t.Fatal("session limit not enforced")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queued := &terminalSession{input: make(chan guestproto.Frame, 1), ctx: ctx, cancel: cancel}
	queued.input <- guestproto.Frame{}
	b.mu.Lock()
	b.terminals["queued"] = queued
	b.mu.Unlock()
	inputReq := httptest.NewRequest("POST", "http://localhost/v1/terminal/queued/input", strings.NewReader(`{"data":"x"}`))
	inputReq.Header.Set("Origin", "http://localhost")
	inputReq.Header.Set("Content-Type", "application/json")
	inputReq.AddCookie(&http.Cookie{Name: sessionCookie, Value: b.session})
	inputResponse := httptest.NewRecorder()
	handler.ServeHTTP(inputResponse, inputReq)
	if inputResponse.Code != 429 {
		t.Fatal("input queue was not bounded")
	}
	writer := &terminalWriter{w: httptest.NewRecorder(), ctx: ctx, cancel: cancel, total: 16 << 20}
	if _, err := writer.Write([]byte("x")); err == nil || ctx.Err() == nil {
		t.Fatal("aggregate output limit did not cancel")
	}
}

func TestTerminalFailureIsSanitizedAndReleasesSlot(t *testing.T) {
	for _, core := range []Core{&richCore{fail: true}, fakeCore{}} {
		bridge := New(core)
		req := httptest.NewRequest("POST", "http://localhost/v1/terminal", strings.NewReader(`{"application":"demo","container":"pod/app","rows":24,"cols":80}`))
		req.Header.Set("Origin", "http://localhost")
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: bridge.session})
		response := httptest.NewRecorder()
		bridge.Handler().ServeHTTP(response, req)
		if strings.Contains(response.Body.String(), "private-backend-token") {
			t.Fatal("terminal leaked backend detail")
		}
		if response.Code == 200 && !strings.Contains(response.Body.String(), `"type":"error"`) {
			t.Fatal("terminal failure reported as success")
		}
		if len(bridge.terminals) != 0 || len(bridge.execs) != 0 {
			t.Fatal("failed session leaked a slot")
		}
	}
}
