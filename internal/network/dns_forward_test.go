//go:build linux

// SPDX-License-Identifier: Apache-2.0

package network

import (
	"context"
	"golang.org/x/net/dns/dnsmessage"
	"net"
	"testing"
	"time"
)

func TestDNSForwardUDPAndTCPWithoutLeakingPrivateNames(t *testing.T) {
	upstreamResolver := NewResolver(ResolverConfig{})
	if err := upstreamResolver.UpsertService("external.example", "default", []string{"192.0.2.9"}, nil); err != nil {
		t.Fatal(err)
	}
	upstream, err := NewDNSServer("127.0.0.1:0", upstreamResolver)
	if err != nil {
		t.Fatal(err)
	}
	go upstream.Serve(context.Background())
	defer upstream.Close()
	local := NewResolver(ResolverConfig{Forwarder: upstream.Addr()})
	if err := local.UpsertService("web", "default", []string{"10.78.0.2"}, nil); err != nil {
		t.Fatal(err)
	}
	server, err := NewDNSServer("127.0.0.1:0", local)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(context.Background()) }()
	defer server.Close()
	for _, tcp := range []bool{false, true} {
		header, answers := dnsQuery(t, server.Addr(), "external.example.", dnsmessage.TypeA, tcp)
		if header.RCode != dnsmessage.RCodeSuccess || len(answers) != 1 {
			t.Fatalf("forwarded tcp=%v: header=%v answers=%v", tcp, header, answers)
		}
		if a, ok := answers[0].Body.(*dnsmessage.AResource); !ok || a.A != [4]byte{192, 0, 2, 9} {
			t.Fatal("wrong upstream address")
		}
		header, answers = dnsQuery(t, server.Addr(), "web.default.svc.cluster.local.", dnsmessage.TypeA, tcp)
		if header.RCode != dnsmessage.RCodeSuccess || len(answers) != 1 {
			t.Fatal("local zone was forwarded")
		}
		header, _ = dnsQuery(t, server.Addr(), "missing.default.svc.cluster.local.", dnsmessage.TypeA, tcp)
		if header.RCode != dnsmessage.RCodeNameError {
			t.Fatal("private unknown name leaked to upstream")
		}
	}
	// An idle TCP client must not postpone server shutdown until its timeout.
	conn, err := net.Dial("tcp", server.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = server.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("DNS close left handlers alive")
	}
}

func TestDNSForwarderRequiresLiteralNonzeroIPv4Address(t *testing.T) {
	for _, address := range []string{"localhost:53", "127.0.0.1:0", "[::1]:53"} {
		server, err := NewDNSServer("127.0.0.1:0", NewResolver(ResolverConfig{Forwarder: address}))
		if err == nil {
			server.Close()
			t.Fatalf("accepted %q", address)
		}
	}
}
