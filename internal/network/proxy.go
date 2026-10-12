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
	serveDone chan struct{}
	slots     chan struct{}
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
	p := &Proxy{listener: listener, target: target, closed: make(chan struct{}), serveDone: make(chan struct{}), slots: make(chan struct{}, 128)}
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
	// Stop all Add calls before Wait, including an accept racing with Close.
	<-p.serveDone
	p.wg.Wait()
	return err
}

func (p *Proxy) serve() {
	defer close(p.serveDone)
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
		select {
		case p.slots <- struct{}{}:
		case <-p.closed:
			conn.Close()
			return
		default:
			// TCP has no structured management diagnostic channel. Refuse excess
			// connections without creating workers or an unbounded waiting queue.
			conn.Close()
			continue
		}
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			defer func() { <-p.slots }()
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
	p.forward(client, upstream, time.Now().Add(5*time.Minute))
}

// forward bounds the whole connection lifetime, including blocked writes.
func (p *Proxy) forward(client, upstream net.Conn, deadline time.Time) {
	if client.SetDeadline(deadline) != nil || upstream.SetDeadline(deadline) != nil {
		return
	}
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
	for range 2 {
		select {
		case <-done:
		case <-p.closed:
			_ = client.SetDeadline(time.Now())
			_ = upstream.SetDeadline(time.Now())
			<-done
		}
	}
}
