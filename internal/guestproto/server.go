// SPDX-License-Identifier: Apache-2.0

package guestproto

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"
)

// Handler serves guest-side requests. Returning a typed error produces an error
// reply; a nil error produces the response type mapped from the request type.
// stream may be used to send data frames before returning.
type Handler interface {
	Handle(ctx context.Context, req Message, stream *Stream) (any, *Error)
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(ctx context.Context, req Message, stream *Stream) (any, *Error)

// Handle implements Handler.
func (f HandlerFunc) Handle(ctx context.Context, req Message, stream *Stream) (any, *Error) {
	return f(ctx, req, stream)
}

// Errorf builds a typed protocol error.
func Errorf(code ErrorCode, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Server is a handshaken guest-side endpoint.
type Server struct {
	conn    Conn
	codec   *Codec
	info    HandshakeInfo
	handler Handler

	wmu sync.Mutex
	mu  sync.Mutex

	cancels map[string]context.CancelFunc
	wg      sync.WaitGroup
	closed  bool
}

// NewServer performs the responding handshake over conn. On failure it closes
// conn.
func NewServer(ctx context.Context, conn Conn, cfg HandshakeConfig, handler Handler) (*Server, error) {
	codec := NewCodec(conn)
	info, err := serverHandshake(ctx, conn, codec, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if handler == nil {
		handler = HandlerFunc(func(context.Context, Message, *Stream) (any, *Error) {
			return nil, Errorf(CodeUnsupported, "no handler")
		})
	}
	return &Server{
		conn:    conn,
		codec:   codec,
		info:    info,
		handler: handler,
		cancels: make(map[string]context.CancelFunc),
	}, nil
}

// Info reports the negotiated handshake result.
func (s *Server) Info() HandshakeInfo { return s.info }

// Close closes the connection.
func (s *Server) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return s.conn.Close()
}

// Serve reads requests until the connection fails or ctx is canceled. Each
// request runs in its own goroutine so a cancel message can stop it.
func (s *Server) Serve(ctx context.Context) error {
	defer s.wg.Wait()
	defer s.cancelAll()
	for {
		fr, err := s.codec.ReadFrame()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		switch fr.Kind {
		case FramePing:
			if err := s.writeFrame(Frame{Kind: FramePong}); err != nil {
				return err
			}
		case FramePong, FrameData:
			// Heartbeat reply or client stdin, which T06 does not route yet.
		case FrameControl:
			var m Message
			if err := json.Unmarshal(fr.Data, &m); err != nil {
				return fmt.Errorf("%w: decode control frame: %v", ErrCorrupt, err)
			}
			if err := s.dispatch(ctx, m); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: frame kind %d", ErrUnknownFrame, fr.Kind)
		}
	}
}

func (s *Server) dispatch(ctx context.Context, m Message) error {
	switch {
	case m.Type == TypePing:
		return s.writeMessage(Message{ID: m.ID, Type: TypePong})
	case m.Type == TypePong:
		return nil
	case m.Type == TypeCancel:
		var cr CancelRequest
		_ = UnmarshalPayload(m.Payload, &cr)
		s.cancel(cr.Target)
		return nil
	case m.Type.isResponse():
		return nil
	}
	reqCtx, cancel := requestContext(ctx, m)
	s.putCancel(m.ID, cancel)
	s.wg.Add(1)
	go s.handle(reqCtx, cancel, m)
	return nil
}

// requestContext derives a cancellable context for a request, honoring the
// deadline the client sent in the message.
func requestContext(ctx context.Context, m Message) (context.Context, context.CancelFunc) {
	base := ctx
	var deadlineCancel context.CancelFunc
	if m.Deadline != nil {
		base, deadlineCancel = context.WithDeadline(ctx, *m.Deadline)
	}
	reqCtx, cancel := context.WithCancel(base)
	return reqCtx, func() {
		cancel()
		if deadlineCancel != nil {
			deadlineCancel()
		}
	}
}

func (s *Server) handle(ctx context.Context, cancel context.CancelFunc, req Message) {
	defer s.wg.Done()
	defer cancel()
	defer s.delCancel(req.ID)
	stream := &Stream{server: s, session: sessionOf(req)}
	reply, perr := s.handler.Handle(ctx, req, stream)
	msg := Message{ID: req.ID}
	if perr != nil {
		msg.Type = TypeError
		msg.Error = perr
	} else {
		raw, err := MarshalPayload(reply)
		if err != nil {
			msg.Type = TypeError
			msg.Error = Errorf(CodeInternal, "encode reply: %v", err)
		} else {
			msg.Type = responseTypeFor(req.Type)
			msg.Payload = raw
		}
	}
	if err := s.writeMessage(msg); err != nil {
		_ = s.Close()
	}
}

func (s *Server) putCancel(id string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancels[id] = cancel
}

func (s *Server) delCancel(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cancels, id)
}

func (s *Server) cancel(id string) {
	s.mu.Lock()
	cancel := s.cancels[id]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// cancelAll cancels every in-flight request, so Serve can wait for handler
// goroutines to finish when the connection closes.
func (s *Server) cancelAll() {
	s.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(s.cancels))
	for _, cancel := range s.cancels {
		cancels = append(cancels, cancel)
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (s *Server) writeFrame(fr Frame) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	return s.codec.WriteFrame(fr)
}

func (s *Server) writeMessage(m Message) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	return s.codec.WriteMessage(m)
}

// Stream sends stream data frames for one session.
type Stream struct {
	server  *Server
	session uint32
}

// Session reports the stream's session ID.
func (st *Stream) Session() uint32 { return st.session }

// WriteStream sends p as one or more data frames of the given stream kind,
// chunked to the frame limit.
func (st *Stream) WriteStream(kind StreamKind, p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		n := len(p)
		if n > MaxDataBytes {
			n = MaxDataBytes
		}
		if err := st.server.writeFrame(Frame{Kind: FrameData, Session: st.session, Stream: kind, Data: p[:n]}); err != nil {
			return total, err
		}
		total += n
		p = p[n:]
	}
	return total, nil
}

// Write sends stdout data (io.Writer).
func (st *Stream) Write(p []byte) (int, error) { return st.WriteStream(StreamStdout, p) }

// WriteStdout sends stdout data.
func (st *Stream) WriteStdout(p []byte) (int, error) { return st.WriteStream(StreamStdout, p) }

// WriteStderr sends stderr data.
func (st *Stream) WriteStderr(p []byte) (int, error) { return st.WriteStream(StreamStderr, p) }

// Exit sends the terminal exit code.
func (st *Stream) Exit(code int) error {
	return st.server.writeFrame(Frame{Kind: FrameData, Session: st.session, Stream: StreamExit, Data: encodeExit(code)})
}

// Resize sends a terminal window size.
func (st *Stream) Resize(rows, cols uint16) error {
	var buf [4]byte
	binary.BigEndian.PutUint16(buf[0:2], rows)
	binary.BigEndian.PutUint16(buf[2:4], cols)
	return st.server.writeFrame(Frame{Kind: FrameData, Session: st.session, Stream: StreamResize, Data: buf[:]})
}

func sessionOf(m Message) uint32 {
	if m.Type != TypeExec {
		return 0
	}
	var req ExecRequest
	_ = UnmarshalPayload(m.Payload, &req)
	return req.Session
}

func responseTypeFor(t MessageType) MessageType {
	switch t {
	case TypeStatus:
		return TypeStatusResult
	case TypeProbe:
		return TypeProbeResult
	case TypeStart:
		return TypeStarted
	case TypeStop:
		return TypeStopped
	case TypeExec:
		return TypeExecResult
	default:
		return TypeError
	}
}
