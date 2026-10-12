//go:build linux

// SPDX-License-Identifier: Apache-2.0

package network

import (
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestProxyAdmissionAndBlockedPeerShutdown(t *testing.T) {
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Go(func() {
		for {
			conn, err := upstream.Accept()
			if err != nil {
				return
			}
			wg.Go(func() { defer conn.Close(); <-stop })
		}
	})
	defer func() { upstream.Close(); close(stop); wg.Wait() }()
	proxy, err := Publish("127.0.0.1", 0, upstream.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	var clients []net.Conn
	defer func() {
		for _, conn := range clients {
			conn.Close()
		}
	}()
	for range 128 {
		conn, err := net.DialTimeout("tcp", proxy.Addr(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, conn)
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(proxy.slots) < 128 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(proxy.slots) != 128 {
		t.Fatal("initial peers not admitted", len(proxy.slots))
	}
	excess, err := net.DialTimeout("tcp", proxy.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer excess.Close()
	excess.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := excess.Read(make([]byte, 1)); err == nil {
		t.Fatal("excess peer not refused")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("excess peer queued instead of closed")
	}
	done := make(chan error, 1)
	go func() { done <- proxy.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("blocked connections survived close")
	}
	if len(proxy.slots) != 0 {
		t.Fatal("slots leaked", len(proxy.slots))
	}
}

func TestProxyWholeDeadlineIncludesBlockedWrites(t *testing.T) {
	client, sender := net.Pipe()
	upstream, nonreader := net.Pipe()
	defer client.Close()
	defer sender.Close()
	defer upstream.Close()
	defer nonreader.Close()
	proxy := &Proxy{closed: make(chan struct{})}
	done := make(chan struct{})
	start := time.Now()
	go func() {
		proxy.forward(client, upstream, start.Add(100*time.Millisecond))
		close(done)
	}()
	written := make(chan struct{})
	go func() {
		sender.SetWriteDeadline(start.Add(time.Second))
		sender.Write(make([]byte, 4096))
		close(written)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("blocked writes outlived whole-connection deadline")
	}
	<-written
	if time.Since(start) < 80*time.Millisecond {
		t.Fatal("forward returned without exercising blocked input/output")
	}
}

func TestProxyPreservesResponseAfterHalfClose(t *testing.T) {
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := upstream.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		io.ReadAll(io.LimitReader(conn, 100))
		io.WriteString(conn, "response-after-eof")
	}()
	proxy, err := Publish("127.0.0.1", 0, upstream.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	conn, err := net.DialTimeout("tcp", proxy.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	io.WriteString(conn, "request")
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(conn)
	if err != nil || string(data) != "response-after-eof" {
		t.Fatal(string(data), err)
	}
	<-done
}
