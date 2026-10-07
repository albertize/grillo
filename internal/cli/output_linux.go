//go:build linux

// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

type terminalOutput struct {
	ctx context.Context
	fd  int
}

func (w terminalOutput) Write(data []byte) (int, error) {
	total := 0
	for len(data) > 0 {
		if err := w.ctx.Err(); err != nil {
			return total, err
		}
		poll := []unix.PollFd{{Fd: int32(w.fd), Events: unix.POLLOUT}}
		n, err := unix.Poll(poll, 100)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return total, err
		}
		if n == 0 {
			continue
		}
		size := min(len(data), 4096)
		count, err := unix.Write(w.fd, data[:size])
		if err == unix.EAGAIN || err == unix.EINTR {
			continue
		}
		if err != nil {
			return total, err
		}
		if count == 0 {
			return total, io.ErrShortWrite
		}
		total += count
		data = data[count:]
	}
	return total, nil
}

// boundedTerminalOutput owns only a duplicated descriptor. Preserve original
// open-file flags (which dup shares) and never close a caller's stdout/stderr.
func boundedTerminalOutput(ctx context.Context, writer io.Writer) (io.Writer, func(), error) {
	file, ok := writer.(*os.File)
	if !ok {
		return writer, func() {}, nil
	}
	flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
	if err != nil {
		return nil, nil, err
	}
	fd, err := unix.FcntlInt(file.Fd(), unix.F_DUPFD_CLOEXEC, 3)
	if err != nil {
		return nil, nil, err
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFL, flags|unix.O_NONBLOCK); err != nil {
		_ = unix.Close(fd)
		return nil, nil, err
	}
	return terminalOutput{ctx, fd}, func() { _, _ = unix.FcntlInt(uintptr(fd), unix.F_SETFL, flags); _ = unix.Close(fd) }, nil
}
