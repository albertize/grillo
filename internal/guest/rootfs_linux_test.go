//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"encoding/json"
	"errors"
	"github.com/albertize/grillo/internal/guestproto"
	"strings"
	"testing"
)

func TestPrivateRootMountPlanAndFailure(t *testing.T) {
	dir := t.TempDir()
	var targets []string
	mount := func(src, target, typ string, flags uintptr, opts string) error {
		if src != "overlay" || typ != "overlay" || !strings.Contains(opts, "lowerdir=/immutable") || !strings.Contains(opts, "upperdir="+dir) {
			t.Fatalf("unsafe mount %q", opts)
		}
		targets = append(targets, target)
		return nil
	}
	for _, name := range []string{"a", "b"} {
		c, err := prepareRoot(dir, guestproto.ContainerSpec{Name: name, Rootfs: "/immutable", PrivateRoot: true, Args: []string{"x"}}, mount)
		if err != nil || c.Rootfs == "/immutable" {
			t.Fatalf("%+v %v", c, err)
		}
	}
	if targets[0] == targets[1] {
		t.Fatal("shared writable upper")
	}
	if _, err := prepareRoot(dir, guestproto.ContainerSpec{Name: "c", Rootfs: "/immutable", PrivateRoot: true, Args: []string{"x"}}, func(string, string, string, uintptr, string) error { return errors.New("overlay unavailable") }); err == nil {
		t.Fatal("mount failure ignored")
	}
}
func TestOCIReadOnlyRoot(t *testing.T) {
	data, err := BuildOCIConfig(guestproto.ContainerSpec{Name: "app", Rootfs: "/root", Args: []string{"x"}, ReadOnlyRootFilesystem: true}, guestproto.SandboxSpec{})
	if err != nil {
		t.Fatal(err)
	}
	var cfg ociConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Root.Readonly {
		t.Fatal("root readonly flag lost")
	}
}
