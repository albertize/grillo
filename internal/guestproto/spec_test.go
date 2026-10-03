// SPDX-License-Identifier: Apache-2.0

package guestproto

import (
	"strings"
	"testing"
)

func validSandbox() SandboxSpec {
	return SandboxSpec{
		ID: "sbx-1",
		Containers: []ContainerSpec{
			{Name: "init", Init: true, Rootfs: "/run/grillo/rootfs/init", Args: []string{"/bin/sh", "-c", "echo hi > /data/ready"}},
			{Name: "app", Rootfs: "/run/grillo/rootfs/app", Args: []string{"/bin/server"}},
			{Name: "sidecar", Rootfs: "/run/grillo/rootfs/sidecar", Args: []string{"/bin/sidecar"}},
		},
	}
}

func TestSandboxSpecValidate(t *testing.T) {
	if err := validSandbox().Validate(); err != nil {
		t.Fatalf("valid sandbox rejected: %v", err)
	}
	cases := map[string]func(*SandboxSpec){
		"missing id":       func(s *SandboxSpec) { s.ID = "" },
		"no containers":    func(s *SandboxSpec) { s.Containers = nil },
		"duplicate name":   func(s *SandboxSpec) { s.Containers[1].Name = "init" },
		"no app container": func(s *SandboxSpec) { s.Containers = s.Containers[:1] },
		"relative rootfs":  func(s *SandboxSpec) { s.Containers[1].Rootfs = "rootfs/app" },
		"missing args":     func(s *SandboxSpec) { s.Containers[1].Args = nil },
		"relative mount":   func(s *SandboxSpec) { s.Containers[1].Mounts = []MountSpec{{Target: "data"}} },
		"negative memory":  func(s *SandboxSpec) { s.Containers[1].Resources.MemoryBytes = -1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := validSandbox()
			mutate(&s)
			if err := s.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestDefaultCapabilities(t *testing.T) {
	caps := DefaultCapabilities()
	if len(caps) == 0 {
		t.Fatal("no default capabilities")
	}
	for _, c := range caps {
		if !strings.HasPrefix(c, "CAP_") {
			t.Fatalf("capability %q does not start with CAP_", c)
		}
	}
}

func TestSandboxSpecJSONRoundTrip(t *testing.T) {
	spec := validSandbox()
	spec.Containers[1].Mounts = []MountSpec{{Source: "/host/data", Target: "/data", ReadOnly: true}}
	spec.Containers[1].Resources.MemoryBytes = 64 << 20
	raw, err := MarshalPayload(spec)
	if err != nil {
		t.Fatal(err)
	}
	var got SandboxSpec
	if err := UnmarshalPayload(raw, &got); err != nil {
		t.Fatal(err)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	if got.Containers[1].Mounts[0].Target != "/data" || !got.Containers[1].Mounts[0].ReadOnly {
		t.Fatalf("mount lost in round trip: %+v", got.Containers[1].Mounts)
	}
}
