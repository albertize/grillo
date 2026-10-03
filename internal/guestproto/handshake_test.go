// SPDX-License-Identifier: Apache-2.0

package guestproto

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
)

func testKey(seed byte) []byte { return bytes.Repeat([]byte{seed}, 32) }

func testHandshakeConfig() HandshakeConfig {
	return HandshakeConfig{
		Sandbox:      "sbx-1",
		Key:          testKey(1),
		Agent:        "agent-1",
		Capabilities: []Capability{CapExec, CapProbe},
	}
}

func runServerHandshake(conn Conn, cfg HandshakeConfig) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := serverHandshake(context.Background(), conn, NewCodec(conn), cfg)
		done <- err
	}()
	return done
}

func TestHandshakeSuccess(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	cfg := testHandshakeConfig()
	done := runServerHandshake(serverConn, cfg)

	info, err := clientHandshake(context.Background(), clientConn, NewCodec(clientConn), cfg)
	if err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("server handshake: %v", err)
	}
	if info.Sandbox != cfg.Sandbox || info.PeerAgent != cfg.Agent {
		t.Fatalf("info = %+v", info)
	}
	if info.Version != CurrentVersion {
		t.Fatalf("version = %s, want %s", info.Version, CurrentVersion)
	}
	if !info.Supports(CapExec) || !info.Supports(CapProbe) {
		t.Fatalf("capabilities = %v", info.Capabilities)
	}
}

func TestHandshakeVersionMismatch(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	serverCfg := testHandshakeConfig()
	serverCfg.Version = ProtocolVersion{Major: ProtocolMajor + 1, Minor: 0}
	done := runServerHandshake(serverConn, serverCfg)

	_, err := clientHandshake(context.Background(), clientConn, NewCodec(clientConn), testHandshakeConfig())
	if err == nil {
		t.Fatal("expected a version mismatch")
	}
	pe, ok := AsError(err)
	if !ok || pe.Code != CodeVersionMismatch {
		t.Fatalf("err = %v, want version_mismatch", err)
	}
	<-done
}

func TestNegotiateVersion(t *testing.T) {
	got, err := negotiateVersion(ProtocolVersion{1, 5}, ProtocolVersion{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if got != (ProtocolVersion{1, 2}) {
		t.Fatalf("negotiated = %s, want 1.2", got)
	}
	if _, err := negotiateVersion(ProtocolVersion{1, 0}, ProtocolVersion{2, 0}); !errors.Is(err, ErrVersionIncompatible) {
		t.Fatalf("err = %v, want ErrVersionIncompatible", err)
	}
}

func TestHandshakeFailedAuthRejected(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	serverCfg := testHandshakeConfig()
	clientCfg := testHandshakeConfig()
	clientCfg.Key = testKey(2) // different per-boot key
	done := runServerHandshake(serverConn, serverCfg)

	_, err := clientHandshake(context.Background(), clientConn, NewCodec(clientConn), clientCfg)
	if err == nil {
		t.Fatal("expected authentication failure")
	}
	pe, ok := AsError(err)
	if !ok || pe.Code != CodeUnauthorized {
		t.Fatalf("err = %v, want unauthorized", err)
	}
	if serverErr := <-done; serverErr == nil {
		t.Fatal("server should have rejected the handshake")
	}
}

func TestHandshakeSandboxMismatch(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	serverCfg := testHandshakeConfig()
	clientCfg := testHandshakeConfig()
	clientCfg.Sandbox = "other"
	done := runServerHandshake(serverConn, serverCfg)

	_, err := clientHandshake(context.Background(), clientConn, NewCodec(clientConn), clientCfg)
	if err == nil {
		t.Fatal("expected sandbox mismatch")
	}
	if pe, ok := AsError(err); !ok || pe.Code != CodeUnauthorized {
		t.Fatalf("err = %v, want unauthorized", err)
	}
	<-done
}
