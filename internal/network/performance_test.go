//go:build linux

// SPDX-License-Identifier: Apache-2.0

package network

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// Loopback service benchmarks are separate from guest/namespace measurements.
// Every successful iteration verifies the response; deadlines bound broken peers.
func BenchmarkDNSLoopbackUDP(b *testing.B) {
	resolver := NewResolver(ResolverConfig{Namespaces: []string{"default"}})
	if err := resolver.UpsertService("postgres", "default", []string{"10.77.0.5"}, nil); err != nil {
		b.Fatal(err)
	}
	server, err := NewDNSServer("127.0.0.1:0", resolver)
	if err != nil {
		b.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	b.Cleanup(func() { cancel(); server.Close(); <-done })
	name, err := dnsmessage.NewName("postgres.default.svc.cluster.local.")
	if err != nil {
		b.Fatal(err)
	}
	message := dnsmessage.Message{Header: dnsmessage.Header{ID: 42}, Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}
	query, err := message.Pack()
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		conn, err := net.DialTimeout("udp", server.Addr(), time.Second)
		if err != nil {
			b.Error(err)
			return
		}
		defer conn.Close()
		data := make([]byte, 4096)
		for pb.Next() {
			if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
				b.Error(err)
				return
			}
			if _, err := conn.Write(query); err != nil {
				b.Error(err)
				return
			}
			n, err := conn.Read(data)
			if err != nil {
				b.Error(err)
				return
			}
			var response dnsmessage.Message
			if err := response.Unpack(data[:n]); err != nil || response.ID != 42 || len(response.Answers) != 1 {
				b.Error("invalid DNS response", err)
				return
			}
		}
	})
}

func BenchmarkProxyLoopbackTCP(b *testing.B) {
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { defer conn.Close(); io.Copy(conn, conn) }()
		}
	}()
	proxy, err := Publish("127.0.0.1", 0, echo.Addr().String())
	if err != nil {
		echo.Close()
		<-done
		b.Fatal(err)
	}
	b.Cleanup(func() { proxy.Close(); echo.Close(); <-done })
	b.SetBytes(4096)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		conn, err := net.DialTimeout("tcp", proxy.Addr(), time.Second)
		if err != nil {
			b.Error(err)
			return
		}
		defer conn.Close()
		data := make([]byte, 4096)
		expected := bytes.Repeat([]byte("grillo!!"), 512)
		for pb.Next() {
			if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
				b.Error(err)
				return
			}
			if _, err := conn.Write(expected); err != nil {
				b.Error(err)
				return
			}
			if _, err := io.ReadFull(conn, data); err != nil {
				b.Error(err)
				return
			}
			if !bytes.Equal(data, expected) {
				b.Error("corrupted proxy response")
				return
			}
		}
	})
}
