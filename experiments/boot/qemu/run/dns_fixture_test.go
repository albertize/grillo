//go:build linux && amd64

// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/binary"
	"testing"
)

// buildQuery emulates real resolvers: withEDNS appends an EDNS0 OPT record.
func buildQuery(name string, qtype uint16, withEDNS bool) []byte {
	if name == "" || name[len(name)-1] != '.' {
		name += "."
	}
	q := make([]byte, 12)
	binary.BigEndian.PutUint16(q[0:2], 0x1234)
	binary.BigEndian.PutUint16(q[2:4], 0x0100) // RD
	binary.BigEndian.PutUint16(q[4:6], 1)      // QDCOUNT
	label := ""
	for _, r := range name {
		if r == '.' {
			q = append(q, byte(len(label)))
			q = append(q, label...)
			label = ""
			continue
		}
		label += string(r)
	}
	q = append(q, 0) // root label
	tmp := make([]byte, 4)
	binary.BigEndian.PutUint16(tmp[0:2], qtype)
	binary.BigEndian.PutUint16(tmp[2:4], 1) // IN
	q = append(q, tmp...)
	if withEDNS {
		// OPT: root name, type 41, class 4096, ttl 0, rdlen 0.
		q = append(q, 0, 0, 41, 0x10, 0x00, 0, 0, 0, 0, 0, 0)
	}
	return q
}

func TestFixtureDNSHandlesEDNS0(t *testing.T) {
	for _, withEDNS := range []bool{false, true} {
		query := buildQuery("server.f0.test.", 1, withEDNS)
		resp, err := fixtureDNS(query)
		if err != nil {
			t.Fatalf("EDNS=%v: unexpected error: %v", withEDNS, err)
		}
		if binary.BigEndian.Uint16(resp[6:8]) != 1 {
			t.Fatalf("EDNS=%v: ANCOUNT=%d, want 1", withEDNS, binary.BigEndian.Uint16(resp[6:8]))
		}
		if binary.BigEndian.Uint16(resp[10:12]) != 0 {
			t.Fatalf("EDNS=%v: ARCOUNT=%d, want 0", withEDNS, binary.BigEndian.Uint16(resp[10:12]))
		}
		if got := binary.BigEndian.Uint32(resp[len(resp)-4:]); got != 0x0a4d000b {
			t.Fatalf("EDNS=%v: A record=%#x, want 10.77.0.11", withEDNS, got)
		}
		if resp[2]&0x80 == 0 {
			t.Fatalf("EDNS=%v: QR not set in response", withEDNS)
		}
	}
}

func TestFixtureDNSOtherCases(t *testing.T) {
	// AAAA: NOERROR with no answer, never an error.
	resp, err := fixtureDNS(buildQuery("server.f0.test.", 28, true))
	if err != nil {
		t.Fatalf("AAAA: %v", err)
	}
	if binary.BigEndian.Uint16(resp[6:8]) != 0 || resp[3]&0x0f != 0 {
		t.Fatalf("AAAA: ANCOUNT/rcode = %d/%d, want 0/0", binary.BigEndian.Uint16(resp[6:8]), resp[3]&0x0f)
	}
	// Unknown name: NXDOMAIN.
	resp, err = fixtureDNS(buildQuery("missing.f0.test.", 1, true))
	if err != nil {
		t.Fatalf("missing: %v", err)
	}
	if resp[3]&0x0f != 3 {
		t.Fatalf("missing: rcode=%d, want NXDOMAIN", resp[3]&0x0f)
	}
	// Malformed inputs are rejected without panicking.
	for _, bad := range [][]byte{nil, {0}, buildQuery("server.f0.test.", 1, false)[:14]} {
		if _, err := fixtureDNS(bad); err == nil {
			t.Fatalf("malformed query accepted: %v", bad)
		}
	}
	// A response (QR=1) must never be treated as a query.
	response := buildQuery("server.f0.test.", 1, true)
	response[2] |= 0x80
	if _, err := fixtureDNS(response); err == nil {
		t.Fatal("QR=1 packet accepted as a query")
	}
}
