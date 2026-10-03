// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"grillo.local/grillo/internal/guestproto"
)

func sampleSandbox() guestproto.SandboxSpec {
	return guestproto.SandboxSpec{
		ID:       "sbx-1",
		Hostname: "pod-1",
		Containers: []guestproto.ContainerSpec{
			{
				Name:   "app",
				Rootfs: "/run/grillo/rootfs/app",
				Args:   []string{"/bin/server", "--port", "8080"},
				Env:    []string{"FOO=bar"},
				User:   guestproto.UserSpec{UID: 1000, GID: 1000},
				Mounts: []guestproto.MountSpec{
					{Source: "/run/grillo/volumes/data", Target: "/data", ReadOnly: true},
					{Target: "/tmp", Type: "tmpfs", Options: []string{"size=16m"}},
				},
				Resources: guestproto.ResourceSpec{CPUQuotaMicros: 50000, CPUPeriodMicros: 100000, MemoryBytes: 64 << 20, PidsLimit: 64},
			},
		},
	}
}

func TestBuildOCIConfig(t *testing.T) {
	sandbox := sampleSandbox()
	raw, err := BuildOCIConfig(sandbox.Containers[0], sandbox)
	if err != nil {
		t.Fatal(err)
	}
	var cfg ociConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("generated config is not valid JSON: %v", err)
	}
	if cfg.OCIVersion != ociVersion {
		t.Errorf("ociVersion = %q", cfg.OCIVersion)
	}
	if cfg.Root == nil || cfg.Root.Path != "/run/grillo/rootfs/app" {
		t.Fatalf("root = %+v", cfg.Root)
	}
	if cfg.Hostname != "pod-1" {
		t.Errorf("hostname = %q", cfg.Hostname)
	}
	if cfg.Process == nil || len(cfg.Process.Args) != 3 || cfg.Process.Cwd != "/" {
		t.Fatalf("process = %+v", cfg.Process)
	}
	if cfg.Process.User.UID != 1000 || cfg.Process.User.GID != 1000 {
		t.Errorf("user = %+v", cfg.Process.User)
	}
	if !cfg.Process.NoNewPrivileges {
		t.Error("noNewPrivileges should default to true")
	}
	if cfg.Process.Capabilities == nil || len(cfg.Process.Capabilities.Bounding) == 0 {
		t.Fatal("capabilities not set")
	}
	if !hasEnv(cfg.Process.Env, "PATH") {
		t.Errorf("PATH not defaulted: %v", cfg.Process.Env)
	}
	if !hasEnv(cfg.Process.Env, "FOO") {
		t.Errorf("explicit env lost: %v", cfg.Process.Env)
	}

	// Network and IPC namespaces must be absent so containers share the guest's.
	for _, ns := range cfg.Linux.Namespaces {
		if ns.Type == "network" || ns.Type == "ipc" {
			t.Fatalf("container must share the guest %s namespace", ns.Type)
		}
	}
	if !hasNamespace(cfg.Linux.Namespaces, "pid") || !hasNamespace(cfg.Linux.Namespaces, "mount") {
		t.Fatalf("missing required namespaces: %+v", cfg.Linux.Namespaces)
	}

	if cfg.Linux.Resources == nil || cfg.Linux.Resources.Memory.Limit != 64<<20 {
		t.Fatalf("memory limit = %+v", cfg.Linux.Resources)
	}
	if cfg.Linux.Resources.CPU.Quota != 50000 || cfg.Linux.Resources.CPU.Period != 100000 {
		t.Fatalf("cpu = %+v", cfg.Linux.Resources.CPU)
	}
	if cfg.Linux.Resources.Pids.Limit != 64 {
		t.Fatalf("pids = %+v", cfg.Linux.Resources.Pids)
	}

	// The host source mount must be read-only and the tmpfs must be converted.
	var dataMount, tmpMount *ociMount
	for i := range cfg.Mounts {
		switch cfg.Mounts[i].Destination {
		case "/data":
			dataMount = &cfg.Mounts[i]
		case "/tmp":
			tmpMount = &cfg.Mounts[i]
		}
	}
	if dataMount == nil || !contains(dataMount.Options, "ro") || dataMount.Source != "/run/grillo/volumes/data" {
		t.Fatalf("data mount = %+v", dataMount)
	}
	if tmpMount == nil || tmpMount.Type != "tmpfs" {
		t.Fatalf("tmpfs mount = %+v", tmpMount)
	}
}

func TestWriteBundle(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bundle")
	sandbox := sampleSandbox()
	if err := WriteBundle(dir, sandbox.Containers[0], sandbox); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config.json mode = %v, want 0600", info.Mode().Perm())
	}
}

func hasNamespace(ns []ociNamespace, typ string) bool {
	for _, n := range ns {
		if n.Type == typ {
			return true
		}
	}
	return false
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
