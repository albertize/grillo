//go:build linux

// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// Listen binds the API socket with 0600 permissions under a 0700 directory.
func (s *Server) Listen(socketPath string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(socketPath); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("api: %s exists and is not a socket", socketPath)
		}
		if err := os.Remove(socketPath); err != nil {
			return nil, err
		}
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = listener.Close()
		return nil, err
	}
	uid := s.opts.PeerUID
	if uid == 0 {
		uid = uint32(os.Getuid())
	}
	return &peerListener{Listener: listener, uid: uid}, nil
}

// Serve runs the API on socketPath until ctx is canceled.
func (s *Server) Serve(ctx context.Context, socketPath string) error {
	listener, err := s.Listen(socketPath)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler:           s.Handler(),
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		s.ops.CancelAll()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	err = server.Serve(listener)
	_ = os.Remove(socketPath)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

type peerListener struct {
	net.Listener
	uid uint32
}

// Accept rejects connections from any other UID.
func (l *peerListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		unixConn, ok := conn.(*net.UnixConn)
		if !ok {
			_ = conn.Close()
			continue
		}
		uid, err := peerUID(unixConn)
		if err != nil || uid != l.uid {
			_ = conn.Close()
			continue
		}
		return conn, nil
	}
}

func peerUID(conn *net.UnixConn) (uint32, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var uid uint32
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if err != nil {
			sockErr = err
			return
		}
		uid = cred.Uid
	}); err != nil {
		return 0, err
	}
	return uid, sockErr
}
