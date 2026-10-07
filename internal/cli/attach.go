//go:build linux

// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/albertize/grillo/internal/api"
	"github.com/albertize/grillo/internal/guestproto"
	"golang.org/x/sys/unix"
)

type attachedClient interface {
	ExecAttached(context.Context, api.AttachRequest, <-chan guestproto.Frame, io.Writer, io.Writer) (int, error)
}

func (a *App) attach(ctx context.Context, client attachedClient, request api.AttachRequest) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stdout, restoreOut, err := boundedTerminalOutput(ctx, a.Stdout)
	if err != nil {
		return 0, err
	}
	defer restoreOut()
	stderr, restoreErr, err := boundedTerminalOutput(ctx, a.Stderr)
	if err != nil {
		return 0, err
	}
	defer restoreErr()
	// os.Stdout writes otherwise terminate the Go process on SIGPIPE before
	// terminal-restoration defers run. Treat a closed output pipe as an error.
	brokenPipe := make(chan os.Signal, 1)
	signal.Notify(brokenPipe, syscall.SIGPIPE)
	defer signal.Stop(brokenPipe)
	input := make(chan guestproto.Frame, 16)
	send := func(frame guestproto.Frame) bool {
		select {
		case input <- frame:
			return true
		case <-ctx.Done():
			return false
		}
	}
	if request.TTY {
		if size, ok := a.Terminal.(interface{ Size() (uint16, uint16) }); ok {
			request.Rows, request.Cols = size.Size()
		}
	}
	resized := make(chan os.Signal, 1)
	if request.TTY {
		signal.Notify(resized, syscall.SIGWINCH)
		defer signal.Stop(resized)
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-resized:
				if size, ok := a.Terminal.(interface{ Size() (uint16, uint16) }); ok {
					rows, cols := size.Size()
					if !send(guestproto.SizeFrame(rows, cols)) {
						return
					}
				}
			}
		}
	}()
	go func() {
		buf := make([]byte, 4096)
		for {
			if file, ok := a.Stdin.(*os.File); ok {
				fds := []unix.PollFd{{Fd: int32(file.Fd()), Events: unix.POLLIN}}
				for ctx.Err() == nil {
					n, err := unix.Poll(fds, 100)
					if err == unix.EINTR {
						continue
					}
					if err != nil {
						return
					}
					if n > 0 {
						break
					}
				}
				if ctx.Err() != nil {
					return
				}
			}
			n, err := a.Stdin.Read(buf)
			if n > 0 && !send(guestproto.Frame{Stream: guestproto.StreamStdin, Data: append([]byte(nil), buf[:n]...)}) {
				return
			}
			if err != nil {
				if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EINTR) {
					continue
				}
				if err != io.EOF {
					cancel()
					return
				}
				send(guestproto.Frame{Stream: guestproto.StreamStdin})
				return
			}
			if ctx.Err() != nil {
				return
			}
		}
	}()
	code, err := client.ExecAttached(ctx, request, input, stdout, stderr)
	cancel() // stop stdin/resize producers before restoring shared descriptor flags.
	return code, err
}
