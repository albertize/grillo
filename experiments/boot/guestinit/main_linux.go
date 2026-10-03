//go:build linux

// SPDX-License-Identifier: Apache-2.0

// Command guestinit is the T01 feasibility guest PID 1.
//
// It is NOT the production Grillo guest agent (see T06/T07). It deliberately
// implements only a minimal, throwaway newline framing to prove that a rootless
// Firecracker microVM can boot, accept a vsock connection, execute a guest
// command, stream output, and be stopped. It runs no OCI runtime and provides no
// container isolation.
package main

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const (
	vsockPort   = 1024
	protoVer    = "1"
	agentVer    = "0.1.0-t01"
	streamChunk = 4096
)

func main() {
	// PID 1 inherits /dev/console as stdio, so these messages reach the serial log.
	if err := setupFilesystems(); err != nil {
		fmt.Fprintln(os.Stderr, "grillo-t01: filesystem setup failed:", err)
		os.Exit(1)
	}
	fmt.Printf("grillo-t01: guest init started, proto=%s agent=%s\n", protoVer, agentVer)

	ln, err := listenVsock(vsockPort)
	if err != nil {
		fmt.Fprintln(os.Stderr, "grillo-t01: vsock listen failed:", err)
		os.Exit(1)
	}
	fmt.Printf("grillo-t01: listening on vsock port %d\n", vsockPort)

	for {
		conn, err := ln.Accept()
		if err != nil {
			// A transient accept error must not kill PID 1; report and continue.
			fmt.Fprintln(os.Stderr, "grillo-t01: accept:", err)
			continue
		}
		serve(conn)
		_ = conn.Close()
	}
}

// setupFilesystems mounts the pseudo-filesystems the guest needs. The initramfs
// provides /proc, /sys, and /dev directories; devtmpfs is auto-mounted by the
// kernel when CONFIG_DEVTMPFS_MOUNT is set, but we mount explicitly to keep the
// experiment independent of that default.
func setupFilesystems() error {
	mounts := []struct{ target, fstype string }{
		{"/proc", "proc"},
		{"/sys", "sysfs"},
		{"/dev", "devtmpfs"},
	}
	for _, m := range mounts {
		if err := unix.Mount(m.fstype, m.target, m.fstype, 0, ""); err != nil {
			// EBUSY means it is already mounted correctly (e.g. devtmpfs auto-mount).
			if errors.Is(err, unix.EBUSY) {
				continue
			}
			return fmt.Errorf("mount %s: %w", m.target, err)
		}
	}
	return nil
}

// listenVsock creates an AF_VSOCK stream listener. Any CID is accepted because
// Firecracker assigns the guest CID.
func listenVsock(port uint32) (netListener, error) {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return netListener{}, fmt.Errorf("socket: %w", err)
	}
	sa := &unix.SockaddrVM{CID: unix.VMADDR_CID_ANY, Port: port}
	if err := unix.Bind(fd, sa); err != nil {
		_ = unix.Close(fd)
		return netListener{}, fmt.Errorf("bind: %w", err)
	}
	if err := unix.Listen(fd, 1); err != nil {
		_ = unix.Close(fd)
		return netListener{}, fmt.Errorf("listen: %w", err)
	}
	return netListener{fd: fd}, nil
}

// netListener wraps a raw vsock file descriptor in an os.File for io use.
type netListener struct{ fd int }

func (l netListener) Accept() (conn io.ReadWriteCloser, err error) {
	nfd, _, err := unix.Accept4(l.fd, unix.SOCK_CLOEXEC)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(nfd), fmt.Sprintf("vsock:%d", nfd)), nil
}

// serve reads tab-separated commands until the peer disconnects. The grammar is
// intentionally trivial and is replaced by the versioned protocol in T06.
//
//	PING                         -> PONG
//	EXEC\t<path>\t<arg>...       -> OUT/ERR/EXIT lines
//	STOP                         -> BYE
func serve(conn io.ReadWriteCloser) {
	mu := &sync.Mutex{}
	_, _ = fmt.Fprintf(conn, "GRILLO-T01 %s %s\n", protoVer, agentVer)
	br := bufio.NewReader(conn)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		fields := strings.Split(strings.TrimRight(line, "\n"), "\t")
		switch fields[0] {
		case "PING":
			mu.Lock()
			_, _ = fmt.Fprint(conn, "PONG\n")
			mu.Unlock()
		case "EXEC":
			if len(fields) < 2 || fields[1] == "" {
				mu.Lock()
				_, _ = fmt.Fprint(conn, "EXIT\t-1\n")
				mu.Unlock()
				continue
			}
			runExec(conn, mu, fields[1], fields[2:])
		case "STOP":
			mu.Lock()
			_, _ = fmt.Fprint(conn, "BYE\n")
			mu.Unlock()
			// Give the host a moment to read BYE, then reset the VM. With
			// reboot=k the x86 machine_restart path uses the keyboard-controller
			// reset, which Firecracker turns into process exit. The guest kernel
			// config has CONFIG_VT but no i8042 driver, so a host-injected
			// SendCtrlAltDel would have no effect; a guest-initiated restart does.
			time.Sleep(100 * time.Millisecond)
			if err := unix.Reboot(unix.LINUX_REBOOT_CMD_RESTART); err != nil {
				fmt.Fprintln(os.Stderr, "grillo-t01: reboot failed:", err)
			}
		default:
			mu.Lock()
			_, _ = fmt.Fprintf(conn, "ERR\t%s\n", base64.StdEncoding.EncodeToString([]byte("unknown command "+fields[0])))
			mu.Unlock()
		}
	}
}

// runExec executes one guest binary (no shell, no argument interpolation) and
// streams base64-encoded output so arbitrary bytes survive the line framing.
func runExec(conn io.Writer, mu *sync.Mutex, path string, args []string) {
	cmd := exec.Command(path, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeExit(conn, mu, -1)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		writeExit(conn, mu, -1)
		return
	}
	if err := cmd.Start(); err != nil {
		mu.Lock()
		_, _ = fmt.Fprintf(conn, "ERR\t%s\n", base64.StdEncoding.EncodeToString([]byte("start: "+err.Error())))
		mu.Unlock()
		writeExit(conn, mu, -1)
		return
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go stream(&wg, conn, mu, "OUT", stdout)
	go stream(&wg, conn, mu, "ERR", stderr)
	wg.Wait()
	code := 0
	if err := cmd.Wait(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	writeExit(conn, mu, code)
}

func stream(wg *sync.WaitGroup, conn io.Writer, mu *sync.Mutex, kind string, r io.Reader) {
	defer wg.Done()
	buf := make([]byte, streamChunk)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			encoded := base64.StdEncoding.EncodeToString(buf[:n])
			mu.Lock()
			_, _ = fmt.Fprintf(conn, "%s\t%s\n", kind, encoded)
			mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func writeExit(conn io.Writer, mu *sync.Mutex, code int) {
	mu.Lock()
	_, _ = fmt.Fprintf(conn, "EXIT\t%d\n", code)
	mu.Unlock()
}
