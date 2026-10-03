//go:build linux && amd64

// SPDX-License-Identifier: Apache-2.0
package spike

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func exchange(t *testing.T, ctx context.Context, timeout time.Duration, reply func(net.Conn)) (string, string, int, error) {
	t.Helper()
	host, guest := net.Pipe()
	defer host.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer guest.Close()
		if _, err := bufio.NewReader(guest).ReadString('\n'); err == nil {
			reply(guest)
			// Keep the transport alive until the host consumes buffered frames.
			// net.Pipe rejects deadline updates after the peer closes.
			_, _ = io.Copy(io.Discard, guest)
		}
	}()
	c := &vsockConn{c: host, r: bufio.NewReader(host)}
	out, stderr, code, err := c.runVerb(ctx, timeout, "EXEC", "/fixture")
	host.Close()
	<-done
	return out, stderr, code, err
}

func TestExecStreamsAndExit(t *testing.T) {
	out, stderr, code, err := exchange(t, context.Background(), time.Second, func(c net.Conn) {
		fmt.Fprint(c, "OUT\taGk=\nERR\tZXJy\nEXIT\t7\n")
	})
	if err != nil || out != "hi" || stderr != "err" || code != 7 {
		t.Fatalf("%q %q %d %v", out, stderr, code, err)
	}
}

func TestExecBounds(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		send       func(net.Conn)
	}{
		{"frame", "frame exceeds", func(c net.Conn) { io.WriteString(c, strings.Repeat("x", maxFrameBytes+1)) }},
		{"output", "output exceeds", func(c net.Conn) {
			line := "OUT\t" + base64.StdEncoding.EncodeToString(make([]byte, 4096)) + "\n"
			for i := 0; i <= maxOutputBytes/4096; i++ {
				if _, err := io.WriteString(c, line); err != nil {
					return
				}
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := exchange(t, context.Background(), 2*time.Second, tc.send)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestExecTotalDeadlineWithContinuousReplies(t *testing.T) {
	start := time.Now()
	_, _, _, err := exchange(t, context.Background(), 80*time.Millisecond, func(c net.Conn) {
		for {
			if _, err := io.WriteString(c, "PID\t1\n"); err != nil {
				return
			}
			time.Sleep(time.Millisecond)
		}
	})
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("deadline not enforced: %v", err)
	}
}

func TestExecCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, _, _, err := exchange(t, ctx, time.Second, func(c net.Conn) { cancel(); io.Copy(io.Discard, c) })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestExecWriteDeadline(t *testing.T) {
	host, guest := net.Pipe()
	defer host.Close()
	defer guest.Close()
	c := &vsockConn{c: host, r: bufio.NewReader(host)}
	_, _, _, err := c.runVerb(context.Background(), 30*time.Millisecond, "EXEC", "/fixture")
	if err == nil {
		t.Fatal("non-reading peer did not time out")
	}
}

func TestExecRejectsInvalidRequest(t *testing.T) {
	for _, arg := range []string{"x\nSTOP", strings.Repeat("x", maxFrameBytes)} {
		host, guest := net.Pipe()
		c := &vsockConn{c: host, r: bufio.NewReader(host)}
		_, _, _, err := c.runVerb(context.Background(), time.Second, "EXEC", "/fixture", arg)
		host.Close()
		guest.Close()
		if err == nil || !strings.Contains(err.Error(), "request") {
			t.Fatalf("got %v", err)
		}
	}
}
