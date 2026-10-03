//go:build linux

// SPDX-License-Identifier: Apache-2.0

package network

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestPublishForwardsTCP(t *testing.T) {
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			conn, err := echo.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()

	proxy, err := Publish("127.0.0.1", 0, echo.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()

	conn, err := net.Dial("tcp", proxy.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "ping" {
		t.Fatalf("echo = %q", buf)
	}
}

func TestPublishRejectsPublicBind(t *testing.T) {
	if _, err := Publish("0.0.0.0", 0, "127.0.0.1:1"); err == nil {
		t.Fatal("expected non-loopback bind to be rejected")
	}
}
