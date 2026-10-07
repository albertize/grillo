//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/albertize/grillo/internal/guestproto"
	linux "github.com/albertize/grillo/internal/platform/linux"
	"golang.org/x/sys/unix"
)

func (r *Runc) ExecAttached(ctx context.Context, req guestproto.ExecRequest, stream *guestproto.Stream) (ExitStatus, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	dir, err := os.MkdirTemp("", "grillo-attach-")
	if err != nil {
		return ExitStatus{}, err
	}
	defer os.RemoveAll(dir)
	pidFile := filepath.Join(dir, "pid")
	args := []string{"exec", "--pid-file", pidFile}
	if req.TTY {
		args = append(args, "-t")
	}
	args = append(args, req.Container)
	args = append(args, req.Args...)
	cmd := r.command(args...)
	var stdin, outR, errR *os.File
	var files []*os.File
	defer func() {
		for _, f := range files {
			_ = f.Close()
		}
	}()
	if req.TTY {
		master, slave, e := linux.OpenPTY()
		if e != nil {
			return ExitStatus{}, errors.New("guest: console unavailable")
		}
		files = append(files, master, slave)
		stdin, outR = master, master
		cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
		// Foreground runc owns the container PTY and propagates SIGWINCH from its
		// controlling terminal. It stays the exec process parent for pidfd cleanup.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
		rows, cols := req.Rows, req.Cols
		if rows == 0 {
			rows = 24
		}
		if cols == 0 {
			cols = 80
		}
		if err := unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: rows, Col: cols}); err != nil {
			return ExitStatus{}, err
		}
	} else {
		inR, inW, e := os.Pipe()
		if e != nil {
			return ExitStatus{}, e
		}
		files = append(files, inR, inW)
		stdin = inW
		cmd.Stdin = inR
		var outW, errW *os.File
		outR, outW, err = os.Pipe()
		if err != nil {
			return ExitStatus{}, err
		}
		files = append(files, outR, outW)
		cmd.Stdout = outW
		errR, errW, err = os.Pipe()
		if err != nil {
			return ExitStatus{}, err
		}
		files = append(files, errR, errW)
		cmd.Stderr = errW
	}
	if err := r.Reaper.Start(cmd); err != nil {
		return ExitStatus{}, err
	}
	for _, f := range files {
		if f == cmd.Stdin || f == cmd.Stdout || f == cmd.Stderr {
			_ = f.Close()
		}
	}
	done := make(chan struct{})
	var status ExitStatus
	var waitErr error
	go func() {
		status, waitErr = r.waitWithCancel(ctx, cmd.Process.Pid, func() error { return stopExec(pidFile, cmd.Process.Pid) })
		close(done)
	}()
	defer func() { cancel(); <-done }()
	var pumps sync.WaitGroup
	pumpErrors := make(chan error, 2)
	copyOutput := func(reader *os.File, writer io.Writer) {
		pumps.Add(1)
		go func() {
			defer pumps.Done()
			_, e := io.Copy(writer, reader)
			if e != nil && !errors.Is(e, unix.EIO) {
				pumpErrors <- e
				cancel()
			}
		}()
	}
	copyOutput(outR, stream)
	if !req.TTY {
		copyOutput(errR, stderrWriter{stream})
	}
	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case frame, ok := <-stream.Input():
				if !ok {
					return
				}
				if frame.Stream == guestproto.StreamResize {
					if req.TTY {
						rows, cols := guestproto.DecodeSize(frame.Data)
						if rows == 0 || cols == 0 {
							cancel()
							return
						}
						if e := unix.IoctlSetWinsize(int(stdin.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: rows, Col: cols}); e != nil {
							cancel()
							return
						}
					}
				} else if frame.Stream == guestproto.StreamStdin {
					payload := frame.Data
					if len(payload) == 0 {
						if req.TTY {
							payload = []byte{4}
						} else {
							_ = stdin.Close()
							continue
						}
					}
					_ = stdin.SetWriteDeadline(time.Now().Add(5 * time.Second))
					if _, e := stdin.Write(payload); e != nil {
						cancel()
						return
					}
				}
			}
		}
	}()
	<-done
	cancel()
	if !req.TTY {
		_ = stdin.Close()
	}
	for _, f := range files {
		_ = f.SetReadDeadline(time.Now().Add(time.Second))
	}
	pumps.Wait()
	<-inputDone
	if len(pumpErrors) > 0 && waitErr == nil {
		waitErr = errors.New("guest: attached output unavailable")
	}
	return status, waitErr
}
