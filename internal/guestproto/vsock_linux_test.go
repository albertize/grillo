//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guestproto

import (
	"context"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// TestVsockLoopback exercises the AF_VSOCK adapter against the local loopback
// device. It skips (rather than fails) when the host forbids AF_VSOCK, so the
// protocol tests do not depend on host capabilities.
func TestVsockLoopback(t *testing.T) {
	const port = 50123
	ln, err := ListenVsock(port)
	if err != nil {
		t.Skipf("AF_VSOCK listen unavailable: %v", err)
	}
	defer ln.Close()

	serverErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		codec := NewCodec(conn)
		msg, err := codec.ReadMessage()
		if err != nil {
			serverErr <- err
			return
		}
		serverErr <- codec.WriteMessage(Message{ID: msg.ID, Type: TypePong})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := DialVsock(ctx, unix.VMADDR_CID_LOCAL, port)
	if err != nil {
		t.Skipf("AF_VSOCK dial unavailable: %v", err)
	}
	defer conn.Close()
	codec := NewCodec(conn)
	if err := codec.WriteMessage(Message{ID: "1", Type: TypePing}); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	reply, err := codec.ReadMessage()
	if err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if reply.Type != TypePong {
		t.Fatalf("reply type = %s, want pong", reply.Type)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}
