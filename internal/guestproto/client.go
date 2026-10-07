// SPDX-License-Identifier: Apache-2.0

package guestproto

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// defaultRequestTimeout bounds an operation when ctx carries no deadline.
const defaultRequestTimeout = 30 * time.Second

// Client is a handshaken host-side endpoint. Operations are serialized: one
// request is in flight per connection. Queued callers retain their deadlines.
type Client struct {
	conn  Conn
	codec *Codec
	info  HandshakeInfo

	calls  chan struct{}
	wmu    sync.Mutex
	next   uint64
	closed atomic.Bool
}

// NewClient performs the initiating handshake over conn. On failure it closes
// conn.
func NewClient(ctx context.Context, conn Conn, cfg HandshakeConfig) (*Client, error) {
	codec := NewCodec(conn)
	info, err := clientHandshake(ctx, conn, codec, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &Client{conn: conn, codec: codec, info: info, calls: make(chan struct{}, 1)}, nil
}

// Info reports the negotiated handshake result.
func (c *Client) Info() HandshakeInfo { return c.info }

// Close closes the connection.
func (c *Client) Close() error {
	if c.closed.Swap(true) {
		return nil
	}
	return c.conn.Close()
}

// Ping sends a heartbeat and waits for the pong.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.call(ctx, TypePing, nil, nil)
	return err
}

// Status requests guest status.
func (c *Client) Status(ctx context.Context) (StatusResult, error) {
	m, err := c.call(ctx, TypeStatus, nil, nil)
	if err != nil {
		return StatusResult{}, err
	}
	var out StatusResult
	if err := UnmarshalPayload(m.Payload, &out); err != nil {
		return StatusResult{}, err
	}
	return out, nil
}

func (c *Client) Metrics(ctx context.Context) (MetricsResult, error) {
	m, err := c.call(ctx, TypeMetrics, nil, nil)
	if err != nil {
		return MetricsResult{}, err
	}
	var out MetricsResult
	if err := UnmarshalPayload(m.Payload, &out); err != nil {
		return MetricsResult{}, err
	}
	if len(out.Containers) > 128 || out.MemoryAvailableBytes != nil && (out.MemoryTotalBytes == nil || *out.MemoryAvailableBytes > *out.MemoryTotalBytes) {
		return MetricsResult{}, fmt.Errorf("guestproto: invalid metrics")
	}
	seen := map[string]bool{}
	for _, container := range out.Containers {
		if container.Name == "" || len(container.Name) > 128 || seen[container.Name] {
			return MetricsResult{}, fmt.Errorf("guestproto: invalid container metrics")
		}
		seen[container.Name] = true
	}
	return out, nil
}

// Logs retrieves a bounded batch of retained container output.
func (c *Client) Logs(ctx context.Context, after uint64) (LogsResult, error) {
	m, err := c.call(ctx, TypeLogs, LogsRequest{After: after}, nil)
	if err != nil {
		return LogsResult{}, err
	}
	var out LogsResult
	if err := UnmarshalPayload(m.Payload, &out); err != nil {
		return LogsResult{}, err
	}
	if len(out.Chunks) > MaxLogBatchChunks {
		return LogsResult{}, fmt.Errorf("guestproto: excessive log batch")
	}
	if out.Gap && len(out.Chunks) == 0 {
		return LogsResult{}, fmt.Errorf("guestproto: empty log gap")
	}
	last := after
	for i, chunk := range out.Chunks {
		if chunk.Sequence != last+1 && !(i == 0 && out.Gap) {
			return LogsResult{}, fmt.Errorf("guestproto: unreported log gap")
		}
		if chunk.Sequence <= last || len(chunk.Data) > MaxLogChunkBytes || chunk.Container == "" || len(chunk.Container) > 128 || (chunk.Stream != "stdout" && chunk.Stream != "stderr") {
			return LogsResult{}, fmt.Errorf("guestproto: invalid log chunk")
		}
		last = chunk.Sequence
	}
	if out.Next != last {
		return LogsResult{}, fmt.Errorf("guestproto: invalid log cursor")
	}
	return out, nil
}

// Probe runs a health probe.
func (c *Client) Probe(ctx context.Context, req ProbeRequest) (ProbeResult, error) {
	m, err := c.call(ctx, TypeProbe, req, nil)
	if err != nil {
		return ProbeResult{}, err
	}
	var out ProbeResult
	if err := UnmarshalPayload(m.Payload, &out); err != nil {
		return ProbeResult{}, err
	}
	return out, nil
}

// Start asks the guest to start its workload.
func (c *Client) Start(ctx context.Context, req StartRequest) (StartResult, error) {
	m, err := c.call(ctx, TypeStart, req, nil)
	if err != nil {
		return StartResult{}, err
	}
	var out StartResult
	if err := UnmarshalPayload(m.Payload, &out); err != nil {
		return StartResult{}, err
	}
	return out, nil
}

// Stop asks the guest to stop its workload.
func (c *Client) Stop(ctx context.Context, req StopRequest) (StopResult, error) {
	m, err := c.call(ctx, TypeStop, req, nil)
	if err != nil {
		return StopResult{}, err
	}
	var out StopResult
	if err := UnmarshalPayload(m.Payload, &out); err != nil {
		return StopResult{}, err
	}
	return out, nil
}

// Restart restarts a container in place.
func (c *Client) Restart(ctx context.Context, container string) (RestartResult, error) {
	m, err := c.call(ctx, TypeRestart, RestartRequest{Container: container}, nil)
	if err != nil {
		return RestartResult{}, err
	}
	var out RestartResult
	if err := UnmarshalPayload(m.Payload, &out); err != nil {
		return RestartResult{}, err
	}
	return out, nil
}

// Cancel asks the guest to cancel a pending request.
func (c *Client) Cancel(ctx context.Context, target string) error {
	_, err := c.call(ctx, TypeCancel, CancelRequest{Target: target}, nil)
	return err
}

// Exec runs a command, streaming stdout and stderr to the supplied writers until
// the guest sends the terminal exec_result or a deadline expires. stdout and
// stderr may be nil to discard.
func (c *Client) Exec(ctx context.Context, req ExecRequest, stdout, stderr io.Writer) (ExecResult, error) {
	return c.ExecAttached(ctx, req, nil, stdout, stderr)
}

// ExecAttached runs a bounded full-duplex session on a dedicated connection.
func (c *Client) ExecAttached(ctx context.Context, req ExecRequest, input <-chan Frame, stdout, stderr io.Writer) (ExecResult, error) {
	if req.Session == 0 {
		req.Session = newSessionID()
	}
	onFrame := func(fr Frame) error {
		if fr.Session != req.Session {
			return nil
		}
		switch fr.Stream {
		case StreamStdout:
			if stdout != nil {
				_, err := stdout.Write(fr.Data)
				return err
			}
		case StreamStderr:
			if stderr != nil {
				_, err := stderr.Write(fr.Data)
				return err
			}
		}
		return nil
	}
	m, err := c.callInput(ctx, TypeExec, req, onFrame, input, req.Session)
	if err != nil {
		return ExecResult{}, err
	}
	var out ExecResult
	if err := UnmarshalPayload(m.Payload, &out); err != nil {
		return ExecResult{}, err
	}
	return out, nil
}

// Run executes a build command, streaming output to stdout and stderr.
func (c *Client) Run(ctx context.Context, req RunRequest, stdout, stderr io.Writer) (RunResult, error) {
	if req.Session == 0 {
		req.Session = newSessionID()
	}
	onFrame := func(fr Frame) error {
		if fr.Session != req.Session {
			return nil
		}
		switch fr.Stream {
		case StreamStdout:
			if stdout != nil {
				_, err := stdout.Write(fr.Data)
				return err
			}
		case StreamStderr:
			if stderr != nil {
				_, err := stderr.Write(fr.Data)
				return err
			}
		}
		return nil
	}
	m, err := c.call(ctx, TypeRun, req, onFrame)
	if err != nil {
		return RunResult{}, err
	}
	var out RunResult
	if err := UnmarshalPayload(m.Payload, &out); err != nil {
		return RunResult{}, err
	}
	return out, nil
}

// call sends a request and reads frames until the matching response arrives.
// Stream data frames are passed to onFrame.
func (c *Client) call(ctx context.Context, typ MessageType, payload any, onFrame func(Frame) error) (Message, error) {
	return c.callInput(ctx, typ, payload, onFrame, nil, 0)
}

func (c *Client) writeFrame(fr Frame) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.codec.WriteFrame(fr)
}
func (c *Client) writeMessage(m Message) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.codec.WriteMessage(m)
}

func (c *Client) callInput(ctx context.Context, typ MessageType, payload any, onFrame func(Frame) error, input <-chan Frame, session uint32) (Message, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultRequestTimeout)
		defer cancel()
	}
	select {
	case c.calls <- struct{}{}:
		defer func() { <-c.calls }()
	case <-ctx.Done():
		return Message{}, ctx.Err()
	}
	if c.closed.Load() {
		return Message{}, fmt.Errorf("guestproto: client closed")
	}
	c.next++
	id := fmt.Sprintf("req-%d", c.next)
	raw, err := MarshalPayload(payload)
	if err != nil {
		return Message{}, err
	}
	req := Message{ID: id, Type: typ, Payload: raw}
	if d, ok := ctx.Deadline(); ok {
		deadline := d
		req.Deadline = &deadline
	}
	stop, err := applyDeadline(ctx, c.conn, defaultRequestTimeout)
	if err != nil {
		return Message{}, err
	}
	defer stop()
	if err := c.writeMessage(req); err != nil {
		return Message{}, fmt.Errorf("guestproto: send %s: %w", typ, deadlineError(ctx, err))
	}
	done := make(chan struct{})
	defer close(done)
	if input != nil {
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
					fr.Kind, fr.Session = FrameData, session
					if (fr.Stream != StreamStdin && fr.Stream != StreamResize) || len(fr.Data) > MaxDataBytes || fr.Stream == StreamResize && len(fr.Data) != 4 {
						_ = c.conn.Close()
						return
					}
					if err := c.writeFrame(fr); err != nil {
						_ = c.conn.Close()
						return
					}
				}
			}
		}()
	}
	for {
		fr, err := c.codec.ReadFrame()
		if err != nil {
			return Message{}, deadlineError(ctx, err)
		}
		switch fr.Kind {
		case FramePing:
			if err := c.writeFrame(Frame{Kind: FramePong}); err != nil {
				return Message{}, err
			}
		case FramePong:
			continue
		case FrameData:
			if onFrame == nil {
				return Message{}, fmt.Errorf("%w: unexpected stream frame", ErrCorrupt)
			}
			if err := onFrame(fr); err != nil {
				return Message{}, err
			}
		case FrameControl:
			var m Message
			if err := json.Unmarshal(fr.Data, &m); err != nil {
				return Message{}, fmt.Errorf("%w: decode control frame: %v", ErrCorrupt, err)
			}
			if m.Type == TypePing {
				if err := c.writeMessage(Message{ID: m.ID, Type: TypePong}); err != nil {
					return Message{}, err
				}
				continue
			}
			if m.ID != id {
				return Message{}, fmt.Errorf("%w: response id %q, want %q", ErrCorrupt, m.ID, id)
			}
			if m.Error != nil {
				return m, m.Error
			}
			if m.Type == TypeError {
				return m, &Error{Code: CodeInternal, Message: "error reply without a typed error"}
			}
			return m, nil
		default:
			return Message{}, fmt.Errorf("%w: frame kind %d", ErrUnknownFrame, fr.Kind)
		}
	}
}

// deadlineError maps a transport timeout caused by a context deadline to the
// context error, since the deadline and the context timer can race.
func deadlineError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		if _, ok := ctx.Deadline(); ok {
			return context.DeadlineExceeded
		}
	}
	return err
}

func newSessionID() uint32 {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 1
	}
	id := binary.BigEndian.Uint32(buf[:])
	if id == 0 {
		id = 1
	}
	return id
}

// encodeExit encodes an exit code for a StreamExit frame.
func encodeExit(code int) []byte {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], uint32(int32(code)))
	return buf[:]
}

// decodeExit decodes a StreamExit frame body (empty means 0).
func decodeExit(data []byte) int {
	if len(data) < 4 {
		return 0
	}
	return int(int32(binary.BigEndian.Uint32(data)))
}
