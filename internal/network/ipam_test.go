//go:build linux

// SPDX-License-Identifier: Apache-2.0

package network

import (
	"errors"
	"testing"
)

func TestIPAMAllocateReleaseAndPersist(t *testing.T) {
	dir := t.TempDir()
	ipam, err := OpenIPAM(dir, "10.77.0.0/24", "10.77.0.1")
	if err != nil {
		t.Fatal(err)
	}
	first, err := ipam.Allocate("sbx-1", "app", "op-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if first.IP != "10.77.0.2" {
		t.Fatalf("first IP = %s, want 10.77.0.2", first.IP)
	}
	second, err := ipam.Allocate("sbx-2", "app", "op-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if second.IP != "10.77.0.3" {
		t.Fatalf("second IP = %s, want 10.77.0.3", second.IP)
	}
	again, err := ipam.Allocate("sbx-1", "app", "op-1", "")
	if err != nil || again.IP != first.IP {
		t.Fatalf("idempotent allocate = %+v err=%v", again, err)
	}

	// Reopen: leases survive a restart.
	reopened, err := OpenIPAM(dir, "10.77.0.0/24", "10.77.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if lease, ok, _ := reopened.Get("sbx-1"); !ok || lease.IP != first.IP {
		t.Fatalf("lease lost after reopen: %+v %v", lease, ok)
	}

	if err := reopened.Release("sbx-1"); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Release("sbx-1"); err != nil {
		t.Fatalf("release should be idempotent: %v", err)
	}
	third, err := reopened.Allocate("sbx-3", "app", "op-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if third.IP != first.IP {
		t.Fatalf("freed address not reused: %s", third.IP)
	}
}

func TestIPAMExhaustion(t *testing.T) {
	// A /30 has one usable host address (.1 gateway, .2 host, .3 broadcast).
	ipam, err := OpenIPAM(t.TempDir(), "10.0.0.0/30", "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ipam.Allocate("a", "app", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := ipam.Allocate("b", "app", "", ""); !errors.Is(err, ErrExhausted) {
		t.Fatalf("err = %v, want ErrExhausted", err)
	}
}

func TestIPAMReconcile(t *testing.T) {
	ipam, _ := OpenIPAM(t.TempDir(), "10.77.0.0/24", "10.77.0.1")
	if _, err := ipam.Allocate("alive", "app", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := ipam.Allocate("dead", "app", "", ""); err != nil {
		t.Fatal(err)
	}
	released, err := ipam.Reconcile(func(id string) bool { return id == "alive" })
	if err != nil {
		t.Fatal(err)
	}
	if len(released) != 1 || released[0] != "dead" {
		t.Fatalf("released = %v", released)
	}
	if _, ok, _ := ipam.Get("dead"); ok {
		t.Fatal("dead lease survived reconcile")
	}
}

func TestIPAMInvalid(t *testing.T) {
	if _, err := OpenIPAM(t.TempDir(), "not-a-subnet", "10.0.0.1"); err == nil {
		t.Fatal("expected subnet error")
	}
	if _, err := OpenIPAM(t.TempDir(), "10.0.0.0/24", "10.1.0.1"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("gateway outside subnet err = %v", err)
	}
}
