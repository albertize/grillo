//go:build linux

// SPDX-License-Identifier: Apache-2.0

package network

import (
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

// idBackend accepts one connection and writes its id.
func idBackend(t *testing.T, id byte) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_, _ = conn.Write([]byte{id})
			_ = conn.Close()
		}
	}()
	return listener.Addr().String(), func() { _ = listener.Close() }
}

func backendPort(t *testing.T, addr string) int {
	t.Helper()
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port := 0
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		t.Fatal(err)
	}
	return port
}

func TestServiceBalancesAcrossReadyEndpoints(t *testing.T) {
	registry, err := OpenServiceRegistry(t.TempDir(), "10.96.0.0/16", "10.96.0.1")
	if err != nil {
		t.Fatal(err)
	}
	vip, err := registry.Upsert(Service{Name: "api", Namespace: "default", Ports: []ServicePort{{Name: "http", Port: 80}}})
	if err != nil {
		t.Fatal(err)
	}
	if vip != "10.96.0.2" {
		t.Fatalf("VIP = %s, want 10.96.0.2", vip)
	}

	addrA, closeA := idBackend(t, 'A')
	defer closeA()
	addrB, closeB := idBackend(t, 'B')
	defer closeB()
	addrC, closeC := idBackend(t, 'C')
	defer closeC()
	unready, closeU := idBackend(t, 'X')
	defer closeU()

	registry.SetEndpoints("default", "api", []Endpoint{
		{IP: "127.0.0.1", Ready: true, Ports: map[string]int{"http": backendPort(t, addrA)}},
		{IP: "127.0.0.1", Ready: true, Ports: map[string]int{"http": backendPort(t, addrB)}},
		{IP: "127.0.0.1", Ready: true, Ports: map[string]int{"http": backendPort(t, addrC)}},
		{IP: "127.0.0.1", Ready: false, Ports: map[string]int{"http": backendPort(t, unready)}},
	})

	proxy, err := NewServiceProxy("127.0.0.1:0", NewBalancer(registry, "default", "api", "http"))
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()

	seen := map[byte]int{}
	for i := 0; i < 9; i++ {
		conn, err := net.DialTimeout("tcp", proxy.Addr(), 3*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		var buf [1]byte
		if _, err := io.ReadFull(conn, buf[:]); err != nil {
			t.Fatal(err)
		}
		seen[buf[0]]++
		_ = conn.Close()
	}
	if seen['X'] != 0 {
		t.Fatalf("unready endpoint served %d connections", seen['X'])
	}
	for _, id := range []byte{'A', 'B', 'C'} {
		if seen[id] != 3 {
			t.Fatalf("round-robin distribution = %v", seen)
		}
	}
}

func TestServiceRejectsUDP(t *testing.T) {
	registry, _ := OpenServiceRegistry(t.TempDir(), "10.96.0.0/16", "10.96.0.1")
	_, err := registry.Upsert(Service{Name: "dns", Namespace: "default", Ports: []ServicePort{{Name: "udp", Port: 53, Protocol: "udp"}}})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}

func TestBalancerNoReadyEndpoints(t *testing.T) {
	registry, _ := OpenServiceRegistry(t.TempDir(), "10.96.0.0/16", "10.96.0.1")
	if _, err := registry.Upsert(Service{Name: "api", Namespace: "default", Ports: []ServicePort{{Name: "http", Port: 80}}}); err != nil {
		t.Fatal(err)
	}
	balancer := NewBalancer(registry, "default", "api", "http")
	if _, ok := balancer.Next(); ok {
		t.Fatal("balancer returned a target with no ready endpoints")
	}
	registry.SetEndpoints("default", "api", []Endpoint{{IP: "127.0.0.1", Ready: true, Ports: map[string]int{"http": 8080}}})
	target, ok := balancer.Next()
	if !ok || target != "127.0.0.1:8080" {
		t.Fatalf("target = %q ok=%v", target, ok)
	}
}
