//go:build linux

// SPDX-License-Identifier: Apache-2.0

package network

import (
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func FuzzDNSRespond(f *testing.F) {
	resolver := NewResolver(ResolverConfig{Namespaces: []string{"default"}})
	if err := resolver.UpsertService("postgres", "default", []string{"10.77.0.5"}, nil); err != nil {
		f.Fatal(err)
	}
	server, err := NewDNSServer("127.0.0.1:0", resolver)
	if err != nil {
		f.Fatal(err)
	}
	defer server.Close()
	name, err := dnsmessage.NewName("postgres.default.svc.cluster.local.")
	if err != nil {
		f.Fatal(err)
	}
	query, err := (&dnsmessage.Message{Header: dnsmessage.Header{ID: 42}, Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}).Pack()
	if err != nil {
		f.Fatal(err)
	}
	f.Add(query)
	f.Add([]byte{})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		// No serving goroutine or external forwarder: only the production codec.
		response, err := server.respond(data)
		if err != nil {
			return
		}
		var msg dnsmessage.Message
		if err := msg.Unpack(response); err != nil {
			t.Fatalf("invalid generated response: %v", err)
		}
		if !msg.Header.Response {
			t.Fatal("generated packet is not a response")
		}
	})
}
