//go:build linux

// SPDX-License-Identifier: Apache-2.0

package network

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Proxy forwards TCP from a host loopback port to a sandbox target. It is the
// publishing primitive: it needs no root and never exposes the management API.
type Proxy struct {
	listener net.Listener
	target   string

	closeOnce sync.Once
	closed    chan struct{}
	wg        sync.WaitGroup
}

// Publish starts a loopback proxy. The caller owns Close.
func Publish(hostIP string, hostPort int, target string) (*Proxy, error) {
	if !isLoopback(hostIP) {
		return nil, fmt.Errorf("%w: publish host %q must be loopback", ErrInvalid, hostIP)
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(hostIP, fmt.Sprintf("%d", hostPort)))
	if err != nil {
		return nil, fmt.Errorf("%w: %s:%d: %v", ErrPortInUse, hostIP, hostPort, err)
	}
	p := &Proxy{listener: listener, target: target, closed: make(chan struct{})}
	go p.serve()
	return p, nil
}

// Addr returns the bound address.
func (p *Proxy) Addr() string { return p.listener.Addr().String() }

// Close stops accepting and waits for in-flight connections to drain.
func (p *Proxy) Close() error {
	var err error
	p.closeOnce.Do(func() {
		close(p.closed)
		err = p.listener.Close()
	})
	p.wg.Wait()
	return err
}

func (p *Proxy) serve() {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-p.closed:
				return
			default:
				return
			}
		}
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.handle(conn)
		}()
	}
}

func (p *Proxy) handle(client net.Conn) {
	defer client.Close()
	dialer := net.Dialer{Timeout: 5 * time.Second}
	upstream, err := dialer.Dial("tcp", p.target)
	if err != nil {
		return
	}
	defer upstream.Close()
	done := make(chan struct{}, 2)
	copyConn := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if tcp, ok := dst.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		done <- struct{}{}
	}
	go copyConn(upstream, client)
	go copyConn(client, upstream)
	select {
	case <-done:
	case <-p.closed:
		_ = client.SetDeadline(time.Now())
		_ = upstream.SetDeadline(time.Now())
	}
}
