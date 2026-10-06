//go:build linux

// SPDX-License-Identifier: Apache-2.0

package network

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// forward preserves DNS wire data (including EDNS and truncation), but bounds
// the entire exchange and verifies the transaction/question before relaying.
func (s *DNSServer) forward(query []byte, question dnsmessage.Question, id uint16, tcp bool) ([]byte, error) {
	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
	defer cancel()
	transport := "udp"
	if tcp {
		transport = "tcp"
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, transport, s.resolver.cfg.Forwarder)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	var response []byte
	if tcp {
		frame := make([]byte, 2+len(query))
		binary.BigEndian.PutUint16(frame, uint16(len(query)))
		copy(frame[2:], query)
		if _, err = conn.Write(frame); err != nil {
			return nil, err
		}
		var size [2]byte
		if _, err = io.ReadFull(conn, size[:]); err != nil {
			return nil, err
		}
		response = make([]byte, int(binary.BigEndian.Uint16(size[:])))
		if _, err = io.ReadFull(conn, response); err != nil {
			return nil, err
		}
	} else {
		if _, err = conn.Write(query); err != nil {
			return nil, err
		}
		response = make([]byte, 4096)
		n, err := conn.Read(response)
		if err != nil {
			return nil, err
		}
		response = response[:n]
	}
	var parser dnsmessage.Parser
	header, err := parser.Start(response)
	if err != nil || !header.Response || header.ID != id {
		return nil, fmt.Errorf("invalid DNS upstream transaction")
	}
	questions, err := parser.AllQuestions()
	if err != nil || len(questions) != 1 || !strings.EqualFold(questions[0].Name.String(), question.Name.String()) || questions[0].Type != question.Type || questions[0].Class != question.Class {
		return nil, fmt.Errorf("invalid DNS upstream question")
	}
	return response, nil
}
