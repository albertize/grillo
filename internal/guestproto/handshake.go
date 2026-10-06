// SPDX-License-Identifier: Apache-2.0

package guestproto

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// Protocol version. The guest and host negotiate to the lower minor version
// within the same major version.
const (
	ProtocolMajor = 1
	ProtocolMinor = 1
)

// ProtocolVersion is a major/minor protocol version.
type ProtocolVersion struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
}

// CurrentVersion is the version this build speaks.
var CurrentVersion = ProtocolVersion{Major: ProtocolMajor, Minor: ProtocolMinor}

func (v ProtocolVersion) String() string { return fmt.Sprintf("%d.%d", v.Major, v.Minor) }

// ErrVersionIncompatible means the two peers share no protocol version.
var ErrVersionIncompatible = errors.New("guestproto: incompatible protocol version")

// ErrAuthFailed means the handshake key proof did not verify.
var ErrAuthFailed = errors.New("guestproto: handshake authentication failed")

// Capability is an optional guest feature advertised during the handshake.
type Capability string

const (
	CapExec           Capability = "exec"
	CapTTY            Capability = "tty"
	CapResize         Capability = "resize"
	CapStdin          Capability = "stdin"
	CapProbe          Capability = "probe"
	CapFiles          Capability = "files"
	CapApplicationDNS Capability = "application-dns"
)

// Role identifies which side of the handshake is running.
type Role uint8

const (
	RoleClient Role = iota
	RoleServer
)

func (r Role) String() string {
	if r == RoleServer {
		return "server"
	}
	return "client"
}

// nonceBytes is the handshake nonce length.
const nonceBytes = 32

// HandshakeConfig configures one side of a handshake. Key is the random per-boot
// key delivered through a private guest channel; it is never logged or placed in
// a message.
type HandshakeConfig struct {
	Sandbox      string
	Key          []byte
	Agent        string
	Capabilities []Capability
	Version      ProtocolVersion
	Timeout      time.Duration
	Rand         io.Reader

	// nonce allows tests to make the handshake deterministic. Production code
	// leaves it nil.
	nonce []byte
}

func (c HandshakeConfig) withDefaults() HandshakeConfig {
	out := c
	if out.Version == (ProtocolVersion{}) {
		out.Version = CurrentVersion
	}
	if out.Timeout == 0 {
		out.Timeout = 10 * time.Second
	}
	if out.Rand == nil {
		out.Rand = rand.Reader
	}
	return out
}

// HandshakeInfo is the negotiated result of a successful handshake.
type HandshakeInfo struct {
	Role         Role
	Version      ProtocolVersion
	Sandbox      string
	PeerAgent    string
	Capabilities []Capability
}

func (h HandshakeInfo) Supports(c Capability) bool {
	for _, have := range h.Capabilities {
		if have == c {
			return true
		}
	}
	return false
}

type helloPayload struct {
	Version ProtocolVersion `json:"version"`
	Sandbox string          `json:"sandbox"`
	Nonce   string          `json:"nonce"`
	Agent   string          `json:"agent,omitempty"`
}

type helloAckPayload struct {
	Version      ProtocolVersion `json:"version"`
	Sandbox      string          `json:"sandbox"`
	Nonce        string          `json:"nonce"`
	Agent        string          `json:"agent,omitempty"`
	Capabilities []Capability    `json:"capabilities,omitempty"`
}

type authPayload struct {
	MAC string `json:"mac"`
}

// clientHandshake runs the initiating side: hello, hello_ack, auth, auth_ok.
func clientHandshake(ctx context.Context, conn Conn, codec *Codec, cfg HandshakeConfig) (HandshakeInfo, error) {
	cfg = cfg.withDefaults()
	if len(cfg.Key) < 16 {
		return HandshakeInfo{}, errors.New("guestproto: handshake key too short")
	}
	stop, err := applyDeadline(ctx, conn, cfg.Timeout)
	if err != nil {
		return HandshakeInfo{}, err
	}
	defer stop()

	clientNonce, err := handshakeNonce(cfg)
	if err != nil {
		return HandshakeInfo{}, err
	}
	hello := helloPayload{
		Version: cfg.Version,
		Sandbox: cfg.Sandbox,
		Nonce:   base64.StdEncoding.EncodeToString(clientNonce),
		Agent:   cfg.Agent,
	}
	rawHello, err := MarshalPayload(hello)
	if err != nil {
		return HandshakeInfo{}, err
	}
	if err := codec.WriteMessage(Message{ID: "hello", Type: TypeHello, Payload: rawHello}); err != nil {
		return HandshakeInfo{}, fmt.Errorf("guestproto: send hello: %w", err)
	}

	reply, err := readHandshakeMessage(codec)
	if err != nil {
		return HandshakeInfo{}, err
	}
	if reply.Type == TypeError {
		return HandshakeInfo{}, reply.Error
	}
	if reply.Type != TypeHelloAck {
		return HandshakeInfo{}, fmt.Errorf("%w: expected hello_ack, got %s", ErrCorrupt, reply.Type)
	}
	var ack helloAckPayload
	if err := UnmarshalPayload(reply.Payload, &ack); err != nil {
		return HandshakeInfo{}, err
	}
	if cfg.Sandbox != "" && ack.Sandbox != cfg.Sandbox {
		return HandshakeInfo{}, &Error{Code: CodeUnauthorized, Message: "sandbox id mismatch"}
	}
	version, err := negotiateVersion(cfg.Version, ack.Version)
	if err != nil {
		return HandshakeInfo{}, err
	}
	serverNonce, err := base64.StdEncoding.DecodeString(ack.Nonce)
	if err != nil || len(serverNonce) != nonceBytes {
		return HandshakeInfo{}, &Error{Code: CodeBadRequest, Message: "invalid server nonce"}
	}

	clientMAC := handshakeMAC(cfg.Key, RoleClient, ack.Sandbox, clientNonce, serverNonce)
	if err := codec.WriteMessage(Message{ID: "auth", Type: TypeAuth, Payload: mustPayload(authPayload{MAC: base64.StdEncoding.EncodeToString(clientMAC)})}); err != nil {
		return HandshakeInfo{}, fmt.Errorf("guestproto: send auth: %w", err)
	}
	serverAuth, err := readHandshakeMessage(codec)
	if err != nil {
		return HandshakeInfo{}, err
	}
	if serverAuth.Type == TypeError {
		return HandshakeInfo{}, serverAuth.Error
	}
	if serverAuth.Type != TypeAuth {
		return HandshakeInfo{}, fmt.Errorf("%w: expected auth, got %s", ErrCorrupt, serverAuth.Type)
	}
	var proof authPayload
	if err := UnmarshalPayload(serverAuth.Payload, &proof); err != nil {
		return HandshakeInfo{}, err
	}
	want := handshakeMAC(cfg.Key, RoleServer, ack.Sandbox, clientNonce, serverNonce)
	got, err := base64.StdEncoding.DecodeString(proof.MAC)
	if err != nil || subtle.ConstantTimeCompare(want, got) != 1 {
		return HandshakeInfo{}, ErrAuthFailed
	}
	return HandshakeInfo{
		Role:         RoleClient,
		Version:      version,
		Sandbox:      ack.Sandbox,
		PeerAgent:    ack.Agent,
		Capabilities: ack.Capabilities,
	}, nil
}

// serverHandshake runs the guest side: read hello, send hello_ack, verify auth,
// prove auth.
func serverHandshake(ctx context.Context, conn Conn, codec *Codec, cfg HandshakeConfig) (HandshakeInfo, error) {
	cfg = cfg.withDefaults()
	if len(cfg.Key) < 16 {
		return HandshakeInfo{}, errors.New("guestproto: handshake key too short")
	}
	stop, err := applyDeadline(ctx, conn, cfg.Timeout)
	if err != nil {
		return HandshakeInfo{}, err
	}
	defer stop()

	req, err := readHandshakeMessage(codec)
	if err != nil {
		return HandshakeInfo{}, err
	}
	if req.Type != TypeHello {
		return HandshakeInfo{}, failHandshake(codec, &Error{Code: CodeBadRequest, Message: fmt.Sprintf("expected hello, got %s", req.Type)})
	}
	var hello helloPayload
	if err := UnmarshalPayload(req.Payload, &hello); err != nil {
		return HandshakeInfo{}, failHandshake(codec, &Error{Code: CodeBadRequest, Message: "invalid hello"})
	}
	if cfg.Sandbox != "" && hello.Sandbox != cfg.Sandbox {
		return HandshakeInfo{}, failHandshake(codec, &Error{Code: CodeUnauthorized, Message: "sandbox id mismatch"})
	}
	version, err := negotiateVersion(hello.Version, cfg.Version)
	if err != nil {
		return HandshakeInfo{}, failHandshake(codec, &Error{Code: CodeVersionMismatch, Message: fmt.Sprintf("client %s, guest %s", hello.Version, cfg.Version)})
	}
	clientNonce, err := base64.StdEncoding.DecodeString(hello.Nonce)
	if err != nil || len(clientNonce) != nonceBytes {
		return HandshakeInfo{}, failHandshake(codec, &Error{Code: CodeBadRequest, Message: "invalid client nonce"})
	}
	serverNonce, err := handshakeNonce(cfg)
	if err != nil {
		return HandshakeInfo{}, err
	}
	rawAck, err := MarshalPayload(helloAckPayload{
		Version:      cfg.Version,
		Sandbox:      cfg.Sandbox,
		Nonce:        base64.StdEncoding.EncodeToString(serverNonce),
		Agent:        cfg.Agent,
		Capabilities: cfg.Capabilities,
	})
	if err != nil {
		return HandshakeInfo{}, err
	}
	if err := codec.WriteMessage(Message{ID: req.ID, Type: TypeHelloAck, Payload: rawAck}); err != nil {
		return HandshakeInfo{}, fmt.Errorf("guestproto: send hello_ack: %w", err)
	}

	auth, err := readHandshakeMessage(codec)
	if err != nil {
		return HandshakeInfo{}, err
	}
	if auth.Type != TypeAuth {
		return HandshakeInfo{}, failHandshake(codec, &Error{Code: CodeBadRequest, Message: fmt.Sprintf("expected auth, got %s", auth.Type)})
	}
	var proof authPayload
	if err := UnmarshalPayload(auth.Payload, &proof); err != nil {
		return HandshakeInfo{}, failHandshake(codec, &Error{Code: CodeBadRequest, Message: "invalid auth"})
	}
	wantClient := handshakeMAC(cfg.Key, RoleClient, hello.Sandbox, clientNonce, serverNonce)
	gotClient, err := base64.StdEncoding.DecodeString(proof.MAC)
	if err != nil || subtle.ConstantTimeCompare(wantClient, gotClient) != 1 {
		return HandshakeInfo{}, failHandshake(codec, &Error{Code: CodeUnauthorized, Message: "client authentication failed"})
	}
	serverMAC := handshakeMAC(cfg.Key, RoleServer, hello.Sandbox, clientNonce, serverNonce)
	if err := codec.WriteMessage(Message{ID: auth.ID, Type: TypeAuth, Payload: mustPayload(authPayload{MAC: base64.StdEncoding.EncodeToString(serverMAC)})}); err != nil {
		return HandshakeInfo{}, fmt.Errorf("guestproto: send auth: %w", err)
	}
	return HandshakeInfo{
		Role:         RoleServer,
		Version:      version,
		Sandbox:      hello.Sandbox,
		PeerAgent:    hello.Agent,
		Capabilities: cfg.Capabilities,
	}, nil
}

func negotiateVersion(client, server ProtocolVersion) (ProtocolVersion, error) {
	if client.Major != server.Major {
		return ProtocolVersion{}, fmt.Errorf("%w: client %s, server %s", ErrVersionIncompatible, client, server)
	}
	minor := client.Minor
	if server.Minor < minor {
		minor = server.Minor
	}
	return ProtocolVersion{Major: client.Major, Minor: minor}, nil
}

func handshakeMAC(key []byte, role Role, sandbox string, clientNonce, serverNonce []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("grillo-guest-handshake\x00"))
	mac.Write([]byte(role.String()))
	mac.Write([]byte{0})
	mac.Write([]byte(sandbox))
	mac.Write([]byte{0})
	mac.Write(clientNonce)
	mac.Write([]byte{0})
	mac.Write(serverNonce)
	return mac.Sum(nil)
}

func handshakeNonce(cfg HandshakeConfig) ([]byte, error) {
	if cfg.nonce != nil {
		out := make([]byte, len(cfg.nonce))
		copy(out, cfg.nonce)
		return out, nil
	}
	buf := make([]byte, nonceBytes)
	if _, err := io.ReadFull(cfg.Rand, buf); err != nil {
		return nil, fmt.Errorf("guestproto: read nonce: %w", err)
	}
	return buf, nil
}

// readHandshakeMessage reads a control message, answering heartbeats and
// rejecting stream frames during the handshake.
func readHandshakeMessage(codec *Codec) (Message, error) {
	for {
		fr, err := codec.ReadFrame()
		if err != nil {
			return Message{}, err
		}
		switch fr.Kind {
		case FramePing:
			if err := codec.WriteFrame(Frame{Kind: FramePong}); err != nil {
				return Message{}, err
			}
		case FramePong:
			continue
		case FrameControl:
			var m Message
			if err := json.Unmarshal(fr.Data, &m); err != nil {
				return Message{}, fmt.Errorf("%w: decode control frame: %v", ErrCorrupt, err)
			}
			return m, nil
		default:
			return Message{}, fmt.Errorf("%w: stream frame during handshake", ErrCorrupt)
		}
	}
}

func failHandshake(codec *Codec, e *Error) error {
	_ = codec.WriteMessage(Message{Type: TypeError, Error: e})
	return e
}

func mustPayload(v any) json.RawMessage {
	raw, err := MarshalPayload(v)
	if err != nil {
		panic(err)
	}
	return raw
}

// applyDeadline sets a connection deadline from ctx or timeout and arranges for
// cancellation to unblock pending reads and writes. The returned stop function
// clears the deadline and the cancellation hook.
func applyDeadline(ctx context.Context, conn Conn, timeout time.Duration) (func(), error) {
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	return func() {
		stop()
		_ = conn.SetDeadline(time.Time{})
	}, nil
}
