//go:build linux

// SPDX-License-Identifier: Apache-2.0

package netns

import (
	"github.com/albertize/grillo/internal/network"
	"net/netip"
	"testing"
)

func TestServiceSnapshotRejectsInvalidTargetsAndTransports(t *testing.T) {
	prefix := netip.MustParsePrefix("10.77.0.0/24")
	valid := func() ServiceUpdate {
		return ServiceUpdate{Service: network.Service{Name: "web", Namespace: "default", Ports: []network.ServicePort{{Name: "http", Port: 80, Protocol: "tcp"}}}, Endpoints: []network.Endpoint{{IP: "10.77.0.2", Ready: true, Ports: map[string]int{"http": 8080}}}}
	}
	if err := validateServiceUpdates([]ServiceUpdate{valid()}, prefix); err != nil {
		t.Fatal(err)
	}
	for _, ip := range []string{"127.0.0.1", "10.78.0.2", "10.77.0.1", "192.0.2.1", "::1"} {
		update := valid()
		update.Endpoints[0].IP = ip
		if err := validateServiceUpdates([]ServiceUpdate{update}, prefix); err == nil {
			t.Fatalf("accepted endpoint %q", ip)
		}
	}
	update := valid()
	update.Service.Ports[0].Protocol = "udp"
	if err := validateServiceUpdates([]ServiceUpdate{update}, prefix); err == nil {
		t.Fatal("UDP accepted")
	}
	update = valid()
	update.Endpoints[0].Ports["http"] = 65536
	if err := validateServiceUpdates([]ServiceUpdate{update}, prefix); err == nil {
		t.Fatal("invalid target port accepted")
	}
	if err := validateServiceUpdates([]ServiceUpdate{valid(), valid()}, prefix); err == nil {
		t.Fatal("duplicate Service accepted")
	}
}
