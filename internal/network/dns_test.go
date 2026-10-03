//go:build linux

// SPDX-License-Identifier: Apache-2.0

package network

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func newTestResolver(t *testing.T) (*Resolver, *DNSServer) {
	t.Helper()
	resolver := NewResolver(ResolverConfig{Namespaces: []string{"default"}})
	if err := resolver.UpsertService("postgres", "default", []string{"10.77.0.5"}, []SRVRecord{
		{Target: "postgres.default.svc.cluster.local", Port: 5432, Priority: 0, Weight: 100},
	}); err != nil {
		t.Fatal(err)
	}
	server, err := NewDNSServer("127.0.0.1:0", resolver)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go server.Serve(ctx)
	t.Cleanup(func() {
		cancel()
		server.Close()
	})
	return resolver, server
}

func dnsQuery(t *testing.T, server, name string, qtype dnsmessage.Type, tcp bool) (dnsmessage.Header, []dnsmessage.Resource) {
	t.Helper()
	fqdn, err := dnsmessage.NewName(name)
	if err != nil {
		t.Fatalf("bad name %q: %v", name, err)
	}
	builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: 4242, RecursionDesired: true})
	builder.EnableCompression()
	if err := builder.StartQuestions(); err != nil {
		t.Fatal(err)
	}
	if err := builder.Question(dnsmessage.Question{Name: fqdn, Type: qtype, Class: dnsmessage.ClassINET}); err != nil {
		t.Fatal(err)
	}
	query, err := builder.Finish()
	if err != nil {
		t.Fatal(err)
	}

	network := "udp"
	if tcp {
		network = "tcp"
	}
	conn, err := net.DialTimeout(network, server, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if tcp {
		frame := make([]byte, 2)
		binary.BigEndian.PutUint16(frame, uint16(len(query)))
		if _, err := conn.Write(append(frame, query...)); err != nil {
			t.Fatal(err)
		}
		var respLen [2]byte
		if _, err := readFull(conn, respLen[:]); err != nil {
			t.Fatal(err)
		}
		resp := make([]byte, binary.BigEndian.Uint16(respLen[:]))
		if _, err := readFull(conn, resp); err != nil {
			t.Fatal(err)
		}
		return parseResponse(t, resp)
	}
	if _, err := conn.Write(query); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	return parseResponse(t, buf[:n])
}

func parseResponse(t *testing.T, msg []byte) (dnsmessage.Header, []dnsmessage.Resource) {
	t.Helper()
	var parser dnsmessage.Parser
	header, err := parser.Start(msg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.AllQuestions(); err != nil {
		t.Fatal(err)
	}
	answers, err := parser.AllAnswers()
	if err != nil && err != dnsmessage.ErrSectionDone {
		t.Fatal(err)
	}
	return header, answers
}

func TestDNSAllFourServiceNames(t *testing.T) {
	_, server := newTestResolver(t)
	names := []string{
		"postgres.",
		"postgres.default.",
		"postgres.default.svc.",
		"postgres.default.svc.cluster.local.",
	}
	for _, network := range []bool{false, true} {
		for _, name := range names {
			header, answers := dnsQuery(t, server.Addr(), name, dnsmessage.TypeA, network)
			if header.RCode != dnsmessage.RCodeSuccess {
				t.Fatalf("%s (tcp=%v): rcode = %v", name, network, header.RCode)
			}
			if len(answers) != 1 {
				t.Fatalf("%s (tcp=%v): answers = %+v", name, network, answers)
			}
			a, ok := answers[0].Body.(*dnsmessage.AResource)
			if !ok || net.IP(a.A[:]).String() != "10.77.0.5" {
				t.Fatalf("%s: answer = %+v", name, answers[0].Body)
			}
		}
	}
}

func TestDNSNXDOMAINAndNotOpenResolver(t *testing.T) {
	_, server := newTestResolver(t)
	if header, _ := dnsQuery(t, server.Addr(), "missing.default.", dnsmessage.TypeA, false); header.RCode != dnsmessage.RCodeNameError {
		t.Fatalf("in-zone miss rcode = %v, want NXDOMAIN", header.RCode)
	}
	if header, _ := dnsQuery(t, server.Addr(), "example.com.", dnsmessage.TypeA, false); header.RCode != dnsmessage.RCodeRefused {
		t.Fatalf("external name rcode = %v, want REFUSED", header.RCode)
	}
}

func TestDNSSRV(t *testing.T) {
	_, server := newTestResolver(t)
	header, answers := dnsQuery(t, server.Addr(), "_tcp.postgres.default.svc.cluster.local.", dnsmessage.TypeSRV, false)
	if header.RCode != dnsmessage.RCodeSuccess || len(answers) != 1 {
		t.Fatalf("srv rcode=%v answers=%+v", header.RCode, answers)
	}
	srv, ok := answers[0].Body.(*dnsmessage.SRVResource)
	if !ok || srv.Port != 5432 {
		t.Fatalf("srv record = %+v", answers[0].Body)
	}
}

func TestResolverRemoveService(t *testing.T) {
	resolver := NewResolver(ResolverConfig{})
	if err := resolver.UpsertService("api", "default", []string{"10.77.0.9"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := resolver.LookupA("api.default"); !ok {
		t.Fatal("service not registered")
	}
	resolver.RemoveService("api", "default")
	if _, ok := resolver.LookupA("api.default"); ok {
		t.Fatal("service not removed")
	}
}
