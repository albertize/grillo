//go:build linux

// SPDX-License-Identifier: Apache-2.0

package network

import (
	"errors"
	"net"
	"testing"
)

func TestPortsReserveRelease(t *testing.T) {
	dir := t.TempDir()
	ports, err := OpenPorts(dir)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := ports.Reserve("127.0.0.1", 18080, "sbx-1", "app", 8080, "tcp")
	if err != nil {
		t.Fatal(err)
	}
	if reservation.TargetPort != 8080 || reservation.Protocol != "tcp" {
		t.Fatalf("reservation = %+v", reservation)
	}
	if _, err := ports.Reserve("127.0.0.1", 18080, "sbx-1", "app", 8080, "tcp"); err != nil {
		t.Fatalf("idempotent reserve: %v", err)
	}
	if _, err := ports.Reserve("127.0.0.1", 18080, "sbx-2", "app", 9090, "tcp"); !errors.Is(err, ErrPortInUse) {
		t.Fatalf("steal err = %v, want ErrPortInUse", err)
	}
	// Reopen and confirm persistence.
	reopened, _ := OpenPorts(dir)
	if list := reopened.List(); len(list) != 1 {
		t.Fatalf("list after reopen = %+v", list)
	}
	if err := reopened.Release("127.0.0.1", 18080); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Release("127.0.0.1", 18080); err != nil {
		t.Fatalf("release should be idempotent: %v", err)
	}
}

func TestPortsRejectsPrivilegedAndPublic(t *testing.T) {
	ports, _ := OpenPorts(t.TempDir())
	if _, err := ports.Reserve("127.0.0.1", 80, "sbx", "app", 80, "tcp"); !errors.Is(err, ErrPrivilegedPort) {
		t.Fatalf("err = %v, want ErrPrivilegedPort", err)
	}
	if _, err := ports.Reserve("0.0.0.0", 18081, "sbx", "app", 80, "tcp"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestPortsDetectsBoundPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	ports, _ := OpenPorts(t.TempDir())
	if _, err := ports.Reserve("127.0.0.1", port, "sbx", "app", 80, "tcp"); !errors.Is(err, ErrPortInUse) {
		t.Fatalf("err = %v, want ErrPortInUse", err)
	}
}

func TestPortsReconcile(t *testing.T) {
	ports, _ := OpenPorts(t.TempDir())
	if _, err := ports.Reserve("127.0.0.1", 18090, "alive", "app", 80, "tcp"); err != nil {
		t.Fatal(err)
	}
	if _, err := ports.Reserve("127.0.0.1", 18091, "dead", "app", 80, "tcp"); err != nil {
		t.Fatal(err)
	}
	released, err := ports.Reconcile(func(id string) bool { return id == "alive" })
	if err != nil || released != 1 {
		t.Fatalf("released=%d err=%v", released, err)
	}
	if list := ports.List(); len(list) != 1 || list[0].SandboxID != "alive" {
		t.Fatalf("list = %+v", list)
	}
}
