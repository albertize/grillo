//go:build linux

// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestAPIConnectionAdmission(t *testing.T) {
	server := NewServer(Options{})
	socket := filepath.Join(t.TempDir(), "api.sock")
	listener, err := server.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	accepted := make(chan net.Conn, 128)
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			accepted <- conn
		}
	}()
	var clients []net.Conn
	defer func() {
		listener.Close()
		<-done
		for _, conn := range clients {
			conn.Close()
		}
	}()
	var servers []net.Conn
	defer func() {
		for _, conn := range servers {
			conn.Close()
		}
	}()
	for range 128 {
		conn, err := net.DialTimeout("unix", socket, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, conn)
		select {
		case conn := <-accepted:
			servers = append(servers, conn)
		case <-time.After(time.Second):
			t.Fatal("admission stalled")
		}
	}
	excess, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer excess.Close()
	excess.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := excess.Read(make([]byte, 1)); err == nil {
		t.Fatal("excess connection accepted")
	} else if e, ok := err.(net.Error); ok && e.Timeout() {
		t.Fatal("excess connection queued")
	}
	for _, conn := range servers {
		conn.Close()
		conn.Close()
	}
	if n := len(listener.(*peerListener).slots); n != 0 {
		t.Fatal("connection slots retained", n)
	}
}

func TestAPISlowBodyWholeReadDeadline(t *testing.T) {
	core := &fakeCore{}
	socket, stop := startServer(t, Options{Core: core})
	defer stop()
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(20 * time.Second))
	start := time.Now()
	if _, err := fmt.Fprint(conn, "POST /v1/applications HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: 100000\r\n\r\n{"); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err == nil {
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatal(response.StatusCode)
		}
	} else if e, ok := err.(net.Error); ok && e.Timeout() {
		t.Fatal("client deadline fired instead of server read budget")
	}
	elapsed := time.Since(start)
	if elapsed < 14*time.Second || elapsed > 18*time.Second {
		t.Fatal("unexpected whole-read deadline", elapsed)
	}
	core.mu.Lock()
	defer core.mu.Unlock()
	if core.applied != 0 {
		t.Fatal("partial request mutated runtime")
	}
}
