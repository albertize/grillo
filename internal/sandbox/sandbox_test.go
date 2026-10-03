// SPDX-License-Identifier: Apache-2.0

package sandbox

import (
	"bytes"
	"testing"
)

func validSpec() Spec {
	return Spec{
		ID:        "sbx-1",
		Kernel:    "/kernel",
		Initramfs: "/initrd",
		VsockPort: 1024,
		GuestKey:  bytes.Repeat([]byte{1}, 32),
	}
}

func TestSpecValidate(t *testing.T) {
	if err := validSpec().Validate(); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
	cases := map[string]func(*Spec){
		"no id":         func(s *Spec) { s.ID = "" },
		"no kernel":     func(s *Spec) { s.Kernel = "" },
		"no vsock":      func(s *Spec) { s.VsockPort = 0 },
		"short key":     func(s *Spec) { s.GuestKey = []byte("short") },
		"empty share":   func(s *Spec) { s.Shares = []Share{{Tag: "s"}} },
		"duplicate tag": func(s *Spec) { s.Shares = []Share{{Tag: "s", HostPath: "/a"}, {Tag: "s", HostPath: "/b"}} },
		"disk no path":  func(s *Spec) { s.Disks = []Disk{{}} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := validSpec()
			mutate(&s)
			if err := s.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
