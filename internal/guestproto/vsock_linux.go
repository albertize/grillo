//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guestproto

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

var _ Conn = (*os.File)(nil)

// DialVsock connects to a guest CID/port over AF_VSOCK (vhost-vsock). The
// returned *os.File is non-blocking so deadlines work with the runtime poller.
func DialVsock(ctx context.Context, cid, port uint32) (Conn, error) {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("guestproto: vsock socket: %w", err)
	}
	if err := unix.Connect(fd, &unix.SockaddrVM{CID: cid, Port: port}); err != nil {
		if !errors.Is(err, unix.EINPROGRESS) {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("guestproto: vsock connect cid %d port %d: %w", cid, port, err)
		}
		if err := waitWritable(ctx, fd); err != nil {
			_ = unix.Close(fd)
			return nil, err
		}
		soErr, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ERROR)
		if err != nil {
			_ = unix.Close(fd)
			return nil, err
		}
		if soErr != 0 {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("guestproto: vsock connect cid %d port %d: %w", cid, port, unix.Errno(soErr))
		}
	}
	return os.NewFile(uintptr(fd), fmt.Sprintf("vsock-%d-%d", cid, port)), nil
}

// ListenVsock listens for guest connections on an AF_VSOCK port. net.FileListener
// does not support AF_VSOCK, so accepted sockets are wrapped directly.
func ListenVsock(port uint32) (net.Listener, error) {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("guestproto: vsock socket: %w", err)
	}
	if err := unix.Bind(fd, &unix.SockaddrVM{CID: unix.VMADDR_CID_ANY, Port: port}); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("guestproto: vsock bind port %d: %w", port, err)
	}
	if err := unix.Listen(fd, 8); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("guestproto: vsock listen: %w", err)
	}
	return &vsockListener{fd: fd, port: port}, nil
}

type vsockListener struct {
	fd   int
	port uint32
}

// Accept blocks until a guest connects. The listener fd is non-blocking and
// polled, so a blocking accept still honors the runtime poller.
func (l *vsockListener) Accept() (net.Conn, error) {
	for {
		fd, _, err := unix.Accept4(l.fd, unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK)
		if err == nil {
			return &vsockConn{File: os.NewFile(uintptr(fd), fmt.Sprintf("vsock-conn-%d", fd))}, nil
		}
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EWOULDBLOCK) {
			return nil, err
		}
		pfd := []unix.PollFd{{Fd: int32(l.fd), Events: unix.POLLIN}}
		if _, perr := unix.Poll(pfd, -1); perr != nil && !errors.Is(perr, unix.EINTR) {
			return nil, perr
		}
	}
}

func (l *vsockListener) Close() error { return unix.Close(l.fd) }

func (l *vsockListener) Addr() net.Addr { return vsockAddr{port: l.port} }

// vsockConn adds the net.Conn address methods to an *os.File socket.
type vsockConn struct{ *os.File }

func (c *vsockConn) LocalAddr() net.Addr  { return vsockAddr{} }
func (c *vsockConn) RemoteAddr() net.Addr { return vsockAddr{} }

type vsockAddr struct {
	cid  uint32
	port uint32
}

func (a vsockAddr) Network() string { return "vsock" }
func (a vsockAddr) String() string  { return fmt.Sprintf("%d:%d", a.cid, a.port) }

func waitWritable(ctx context.Context, fd int) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		timeoutMS := 200
		if d, ok := ctx.Deadline(); ok {
			remaining := time.Until(d)
			if remaining <= 0 {
				return context.DeadlineExceeded
			}
			if ms := int(remaining.Milliseconds()); ms < timeoutMS {
				timeoutMS = ms
				if timeoutMS < 1 {
					timeoutMS = 1
				}
			}
		}
		pfd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLOUT}}
		n, err := unix.Poll(pfd, timeoutMS)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return fmt.Errorf("guestproto: vsock poll: %w", err)
		}
		if n > 0 {
			return nil
		}
	}
}
