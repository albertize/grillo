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
	"time"
)

// defaultRequestTimeout bounds an operation when ctx carries no deadline.
const defaultRequestTimeout = 30 * time.Second

// Client is a handshaken host-side endpoint. Operations are serialized: one
// request is in flight per connection. Concurrent callers block on the mutex.
type Client struct {
	conn  Conn
	codec *Codec
	info  HandshakeInfo

	mu     sync.Mutex
	next   uint64
	closed bool
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
	return &Client{conn: conn, codec: codec, info: info}, nil
}

// Info reports the negotiated handshake result.
func (c *Client) Info() HandshakeInfo { return c.info }

// Close closes the connection.
func (c *Client) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
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

// Cancel asks the guest to cancel a pending request.
func (c *Client) Cancel(ctx context.Context, target string) error {
	_, err := c.call(ctx, TypeCancel, CancelRequest{Target: target}, nil)
	return err
}

// Exec runs a command, streaming stdout and stderr to the supplied writers until
// the guest sends the terminal exec_result or a deadline expires. stdout and
// stderr may be nil to discard.
func (c *Client) Exec(ctx context.Context, req ExecRequest, stdout, stderr io.Writer) (ExecResult, error) {
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
	m, err := c.call(ctx, TypeExec, req, onFrame)
	if err != nil {
		return ExecResult{}, err
	}
	var out ExecResult
	if err := UnmarshalPayload(m.Payload, &out); err != nil {
		return ExecResult{}, err
	}
	return out, nil
}

// call sends a request and reads frames until the matching response arrives.
// Stream data frames are passed to onFrame.
func (c *Client) call(ctx context.Context, typ MessageType, payload any, onFrame func(Frame) error) (Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
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
	if err := c.codec.WriteMessage(req); err != nil {
		return Message{}, fmt.Errorf("guestproto: send %s: %w", typ, deadlineError(ctx, err))
	}
	for {
		fr, err := c.codec.ReadFrame()
		if err != nil {
			return Message{}, deadlineError(ctx, err)
		}
		switch fr.Kind {
		case FramePing:
			if err := c.codec.WriteFrame(Frame{Kind: FramePong}); err != nil {
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
				if err := c.codec.WriteMessage(Message{ID: m.ID, Type: TypePong}); err != nil {
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
	return binary.BigEndian.Uint32(buf[:])
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
