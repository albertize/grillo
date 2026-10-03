// SPDX-License-Identifier: Apache-2.0

package guestproto

import (
	"net"
	"time"
)

// Conn is the transport contract the protocol needs. net.Conn and *os.File for
// an AF_VSOCK socket both satisfy it, so the codec stays independent of the
// transport.
type Conn interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Close() error
	SetDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
}

var (
	_ Conn = (net.Conn)(nil)
	_ Conn = (*net.TCPConn)(nil)
	_ Conn = (*net.UnixConn)(nil)
	_ Conn = (*net.IPConn)(nil)
)
