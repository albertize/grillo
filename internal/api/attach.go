//go:build linux

// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/albertize/grillo/internal/guestproto"
)

// AttachedExec is a CLI-only duplex surface, never forwarded by the web bridge.
type AttachedExec interface {
	ExecAttached(context.Context, string, string, guestproto.ExecRequest, <-chan guestproto.Frame, io.Writer, io.Writer) (int, error)
}

type AttachRequest struct {
	Application string   `json:"application"`
	Container   string   `json:"container"`
	Args        []string `json:"args"`
	TTY         bool     `json:"tty"`
	Rows        uint16   `json:"rows,omitempty"`
	Cols        uint16   `json:"cols,omitempty"`
}

type attachWriter struct {
	conn   net.Conn
	codec  *guestproto.Codec
	mu     *sync.Mutex
	stream guestproto.StreamKind
}

func (w attachWriter) Write(data []byte) (int, error) {
	total := 0
	for len(data) > 0 {
		n := min(len(data), guestproto.MaxDataBytes)
		w.mu.Lock()
		_ = w.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		err := w.codec.WriteFrame(guestproto.Frame{Kind: guestproto.FrameData, Stream: w.stream, Data: data[:n]})
		w.mu.Unlock()
		if err != nil {
			return total, err
		}
		total += n
		data = data[n:]
	}
	return total, nil
}

func (s *Server) handleAttach(w http.ResponseWriter, r *http.Request) {
	core, ok := s.opts.Core.(AttachedExec)
	if !ok {
		writeError(w, 503, "unavailable", "interactive exec unavailable")
		return
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "grillo-attach-v1") {
		writeError(w, 400, "upgrade_required", "interactive exec requires protocol upgrade")
		return
	}
	var request AttachRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	decodeErr := decoder.Decode(&request)
	if decodeErr == nil {
		if err := decoder.Decode(new(any)); err != io.EOF {
			decodeErr = errors.New("trailing attach data")
		}
	}
	if decodeErr != nil || request.Application == "" || request.Container == "" || len(request.Args) == 0 {
		writeError(w, 400, "bad_request", "invalid attach request")
		return
	}
	select {
	case s.attachSlots <- struct{}{}:
		defer func() { <-s.attachSlots }()
	default:
		writeError(w, 429, "busy", "interactive session limit")
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		writeError(w, 500, "unavailable", "interactive transport unavailable")
		return
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(r.Context(), time.Hour)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Time{})
	if _, err := rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: grillo-attach-v1\r\n\r\n"); err != nil {
		return
	}
	if err := rw.Flush(); err != nil {
		return
	}
	codec := guestproto.NewCodecReaderWriter(rw.Reader, conn)
	input := make(chan guestproto.Frame, 16)
	go func() {
		defer cancel()
		for {
			fr, err := codec.ReadFrame()
			if err != nil {
				return
			}
			if fr.Session != 0 || fr.Kind != guestproto.FrameData || fr.Stream != guestproto.StreamStdin && fr.Stream != guestproto.StreamResize || fr.Stream == guestproto.StreamResize && len(fr.Data) != 4 {
				return
			}
			select {
			case input <- fr:
			case <-ctx.Done():
				return
			}
		}
	}()
	var mu sync.Mutex
	stdout := attachWriter{conn, codec, &mu, guestproto.StreamStdout}
	stderr := attachWriter{conn, codec, &mu, guestproto.StreamStderr}
	code, err := core.ExecAttached(ctx, request.Application, request.Container, guestproto.ExecRequest{Args: request.Args, TTY: request.TTY, Stdin: true, Rows: request.Rows, Cols: request.Cols}, input, stdout, stderr)
	mu.Lock()
	defer mu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	message := guestproto.Message{Type: guestproto.TypeExecResult}
	if err != nil {
		message.Type = guestproto.TypeError
		message.Error = guestproto.Errorf(guestproto.CodeInternal, "interactive exec failed")
	} else {
		message.Payload, _ = guestproto.MarshalPayload(guestproto.ExecResult{ExitCode: code})
	}
	_ = codec.WriteMessage(message)
}

// ExecAttached uses a private Unix connection and a strict final-result frame.
func (c *Client) ExecAttached(ctx context.Context, request AttachRequest, input <-chan guestproto.Frame, stdout, stderr io.Writer) (int, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	body, err := json.Marshal(request)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest(http.MethodPost, "http://unix/v1/exec-attach", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "grillo-attach-v1")
	req.Header.Set("Content-Type", "application/json")
	if err := req.Write(conn); err != nil {
		return 0, err
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, req)
	if err != nil {
		return 0, err
	}
	if response.StatusCode != 101 {
		defer response.Body.Close()
		return 0, decodeError(response)
	}
	if !strings.EqualFold(response.Header.Get("Upgrade"), "grillo-attach-v1") {
		return 0, errors.New("api: invalid interactive upgrade")
	}
	_ = conn.SetDeadline(time.Time{})
	codec := guestproto.NewCodecReaderWriter(reader, conn)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case fr, ok := <-input:
				if !ok {
					return
				}
				if fr.Stream != guestproto.StreamStdin && fr.Stream != guestproto.StreamResize || len(fr.Data) > guestproto.MaxDataBytes || fr.Stream == guestproto.StreamResize && len(fr.Data) != 4 {
					_ = conn.Close()
					return
				}
				fr.Kind, fr.Session = guestproto.FrameData, 0
				_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if err := codec.WriteFrame(fr); err != nil {
					_ = conn.Close()
					return
				}
			}
		}
	}()
	for {
		fr, err := codec.ReadFrame()
		if err != nil {
			return 0, fmt.Errorf("api: interactive session ended without result: %w", err)
		}
		if fr.Kind == guestproto.FrameControl {
			var m guestproto.Message
			if err := json.Unmarshal(fr.Data, &m); err != nil {
				return 0, err
			}
			if m.Error != nil {
				return 0, m.Error
			}
			if m.Type != guestproto.TypeExecResult {
				return 0, errors.New("api: invalid attach result")
			}
			var result struct {
				ExitCode *int `json:"exitCode"`
			}
			if err := json.Unmarshal(m.Payload, &result); err != nil || result.ExitCode == nil || *result.ExitCode < 0 || *result.ExitCode > 255 {
				return 0, errors.New("api: invalid attach exit result")
			}
			return *result.ExitCode, nil
		}
		if fr.Session != 0 || fr.Kind != guestproto.FrameData || fr.Stream != guestproto.StreamStdout && fr.Stream != guestproto.StreamStderr {
			return 0, errors.New("api: invalid attach output")
		}
		writer := stdout
		if fr.Stream == guestproto.StreamStderr {
			writer = stderr
		}
		if writer != nil {
			if _, err := writer.Write(fr.Data); err != nil {
				return 0, err
			}
		}
	}
}
