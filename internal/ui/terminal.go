//go:build linux

// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/albertize/grillo/internal/api"
	"github.com/albertize/grillo/internal/guestproto"
)

type terminalCore interface {
	ExecAttached(context.Context, api.AttachRequest, <-chan guestproto.Frame, io.Writer, io.Writer) (int, error)
}
type terminalSession struct {
	input  chan guestproto.Frame
	cancel context.CancelFunc
	ctx    context.Context
}

// All three operations require both the private bridge cookie and same origin.
// A random session ID is additional scoping, never a replacement for auth.
func terminalMutation(w http.ResponseWriter, r *http.Request) bool {
	if !sameOrigin(r.Header.Get("Origin"), r.Host) {
		http.Error(w, "same origin required", 403)
		return false
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		http.Error(w, "JSON required", 415)
		return false
	}
	return true
}
func terminalDecode(w http.ResponseWriter, r *http.Request, value any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid terminal request", 400)
		return false
	}
	return true
}
func (b *Bridge) handleTerminal(w http.ResponseWriter, r *http.Request) {
	if !terminalMutation(w, r) {
		return
	}
	core, ok := b.core.(terminalCore)
	if !ok {
		http.Error(w, "interactive terminal unavailable", 503)
		return
	}
	var request struct {
		Application string `json:"application"`
		Container   string `json:"container"`
		Rows        uint16 `json:"rows"`
		Cols        uint16 `json:"cols"`
	}
	if !terminalDecode(w, r, &request) {
		return
	}
	if request.Application == "" || len(request.Application) > 256 || request.Container == "" || len(request.Container) > 512 || request.Rows < 1 || request.Rows > 300 || request.Cols < 1 || request.Cols > 500 {
		http.Error(w, "invalid terminal target or dimensions", 400)
		return
	}
	select {
	case b.execs <- struct{}{}:
		defer func() { <-b.execs }()
	default:
		http.Error(w, "too many exec sessions", 429)
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	session := &terminalSession{input: make(chan guestproto.Frame, 16), cancel: cancel, ctx: ctx}
	id := randomToken()
	b.mu.Lock()
	b.terminals[id] = session
	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.terminals, id); b.mu.Unlock() }()
	w.Header().Set("Content-Type", "text/event-stream")
	writer := &terminalWriter{w: w, ctx: ctx, cancel: cancel}
	if err := writer.event(map[string]any{"type": "session", "id": id}); err != nil {
		return
	}
	// The only command is a guest /bin/sh, never a host command or shell string.
	code, err := core.ExecAttached(ctx, api.AttachRequest{Application: request.Application, Container: request.Container, Args: []string{"/bin/sh", "-c", "export TERM=xterm-256color; exec /bin/sh -i"}, TTY: true, Rows: request.Rows, Cols: request.Cols}, session.input, writer, writer)
	if err != nil {
		_ = writer.event(map[string]any{"type": "error", "message": "Terminal ended or failed. Check the container and availability of /bin/sh."})
		return
	}
	_ = writer.event(map[string]any{"type": "exit", "exitCode": code})
}

type terminalWriter struct {
	mu     sync.Mutex
	w      http.ResponseWriter
	ctx    context.Context
	cancel context.CancelFunc
	total  int
}

func (w *terminalWriter) event(value any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.eventLocked(value)
}
func (w *terminalWriter) eventLocked(value any) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_ = http.NewResponseController(w.w).SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err = fmt.Fprintf(w.w, "data: %s\n\n", payload); err == nil {
		err = http.NewResponseController(w.w).Flush()
	}
	if err != nil {
		w.cancel()
	}
	return err
}
func (w *terminalWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	written := 0
	for len(data) > 0 {
		n := min(len(data), 8192)
		if w.total+n > 16<<20 {
			w.cancel()
			return written, errors.New("terminal output limit")
		}
		if err := w.eventLocked(map[string]any{"type": "output", "data": base64.StdEncoding.EncodeToString(data[:n])}); err != nil {
			return written, err
		}
		w.total += n
		written += n
		data = data[n:]
	}
	return written, nil
}
func (b *Bridge) terminal(r *http.Request) *terminalSession {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.terminals[r.PathValue("id")]
}
func (b *Bridge) handleTerminalInput(w http.ResponseWriter, r *http.Request) {
	if !terminalMutation(w, r) {
		return
	}
	var request struct {
		Data  string `json:"data"`
		Bytes string `json:"bytes"`
		Rows  uint16 `json:"rows"`
		Cols  uint16 `json:"cols"`
	}
	if !terminalDecode(w, r, &request) {
		return
	}
	frame := guestproto.Frame{Kind: guestproto.FrameData, Stream: guestproto.StreamStdin, Data: []byte(request.Data)}
	if request.Bytes != "" {
		decoded, err := base64.StdEncoding.Strict().DecodeString(request.Bytes)
		if err != nil || request.Data != "" || request.Rows != 0 || request.Cols != 0 {
			http.Error(w, "invalid binary input", 400)
			return
		}
		frame.Data = decoded
	}
	if request.Rows != 0 || request.Cols != 0 {
		if request.Data != "" || request.Rows < 1 || request.Rows > 300 || request.Cols < 1 || request.Cols > 500 {
			http.Error(w, "invalid resize", 400)
			return
		}
		frame.Stream = guestproto.StreamResize
		frame.Data = make([]byte, 4)
		binary.BigEndian.PutUint16(frame.Data, request.Rows)
		binary.BigEndian.PutUint16(frame.Data[2:], request.Cols)
	} else if len(frame.Data) == 0 || len(frame.Data) > 8192 {
		http.Error(w, "invalid input size", 400)
		return
	}
	session := b.terminal(r)
	if session == nil || session.ctx.Err() != nil {
		http.Error(w, "terminal not found", 404)
		return
	}
	select {
	case session.input <- frame:
		w.WriteHeader(204)
	case <-session.ctx.Done():
		http.Error(w, "terminal ended", 410)
	default:
		http.Error(w, "terminal input busy", 429)
	}
}
func (b *Bridge) handleTerminalClose(w http.ResponseWriter, r *http.Request) {
	if !terminalMutation(w, r) {
		return
	}
	var body struct{}
	if !terminalDecode(w, r, &body) {
		return
	}
	if session := b.terminal(r); session != nil {
		session.cancel()
	}
	w.WriteHeader(204) // idempotent, including sessions already exited
}
