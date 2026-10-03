//go:build linux && amd64

// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// fixtureDNS answers one fixed test name, with no recursion or forwarding.
// This bounded fixture is NOT the production DNS implementation (T12).
func fixtureDNS(query []byte) ([]byte, error) {
	if len(query) < 12 || len(query) > 512 || binary.BigEndian.Uint16(query[4:6]) != 1 || query[2]&0x80 != 0 {
		return nil, errors.New("invalid fixture query")
	}
	pos := 12
	var labels []string
	for {
		if pos >= len(query) {
			return nil, io.ErrUnexpectedEOF
		}
		n := int(query[pos])
		pos++
		if n == 0 {
			break
		}
		if n > 63 || pos+n > len(query) {
			return nil, errors.New("invalid label")
		}
		labels = append(labels, string(query[pos:pos+n]))
		pos += n
	}
	if pos+4 > len(query) {
		return nil, errors.New("invalid question length")
	}
	typ := binary.BigEndian.Uint16(query[pos : pos+2])
	class := binary.BigEndian.Uint16(query[pos+2 : pos+4])
	// Real resolvers (including Go) append EDNS0 OPT records; echo only the
	// question section and answer with ARCOUNT=0 so the reply stays well formed.
	question := query[12 : pos+4]
	result := make([]byte, 0, 12+len(question)+16)
	result = append(result, query[0], query[1]) // ID
	result = append(result, 0x85, 0x00)         // QR, AA, RD; no recursion
	result = append(result, 0, 1, 0, 0)         // QDCOUNT=1, ANCOUNT=0
	result = append(result, 0, 0, 0, 0)         // NSCOUNT=0, ARCOUNT=0
	result = append(result, question...)
	if strings.ToLower(strings.Join(labels, ".")) != "server.f0.test" || class != 1 {
		result[3] = 3 // NXDOMAIN
		return result, nil
	}
	if typ != 1 {
		return result, nil // NOERROR with no answer (e.g. AAAA)
	}
	result[7] = 1 // ANCOUNT=1
	return append(result, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 1, 0, 4, 10, 77, 0, 11), nil
}
func startFixtureDNS(ctx context.Context) (func(), error) {
	udp, err := net.ListenPacket("udp4", "10.77.0.1:53")
	if err != nil {
		return nil, err
	}
	tcp, err := net.Listen("tcp4", "10.77.0.1:53")
	if err != nil {
		udp.Close()
		return nil, err
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		buf := make([]byte, 513)
		for {
			n, peer, err := udp.ReadFrom(buf)
			if err != nil {
				return
			}
			if b, err := fixtureDNS(buf[:n]); err == nil {
				udp.WriteTo(b, peer)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for {
			c, err := tcp.Accept()
			if err != nil {
				return
			}
			c.SetDeadline(time.Now().Add(time.Second))
			var header [2]byte
			if _, err = io.ReadFull(c, header[:]); err == nil {
				n := int(binary.BigEndian.Uint16(header[:]))
				if n <= 512 {
					b := make([]byte, n)
					if _, err = io.ReadFull(c, b); err == nil {
						if r, err := fixtureDNS(b); err == nil {
							binary.BigEndian.PutUint16(header[:], uint16(len(r)))
							c.Write(append(header[:], r...))
						}
					}
				}
			}
			c.Close()
			if ctx.Err() != nil {
				return
			}
		}
	}()
	return func() { udp.Close(); tcp.Close(); wg.Wait() }, nil
}
