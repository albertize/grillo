//go:build linux

// SPDX-License-Identifier: Apache-2.0

package netns

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"time"
)

func (s *Supervisor) relay(dialCtx, lifetime context.Context, conn net.Conn, target string) {
	upstream, err := s.datapath.Dial(dialCtx, target)
	if err != nil {
		_ = json.NewEncoder(conn).Encode(Response{Error: "Service connection unavailable"})
		return
	}
	defer upstream.Close()
	if err := json.NewEncoder(conn).Encode(Response{OK: true}); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Minute))
	_ = upstream.SetDeadline(time.Now().Add(2 * time.Minute))
	done := make(chan struct{}, 2)
	pump := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if c, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = c.CloseWrite()
		}
		done <- struct{}{}
	}
	go pump(upstream, conn)
	go pump(conn, upstream)
	for remaining := 2; remaining > 0; {
		select {
		case <-done:
			remaining--
		case <-lifetime.Done():
			_ = conn.Close()
			_ = upstream.Close()
			for ; remaining > 0; remaining-- {
				<-done
			}
		}
	}
}
