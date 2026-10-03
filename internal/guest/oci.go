// SPDX-License-Identifier: Apache-2.0

// Package guest implements the Grillo guest agent: PID 1 responsibilities,
// filesystem and cgroup setup, child reaping, OCI bundle generation, and a runc
// adapter. It runs inside the microVM; the host never imports the runtime parts.
package guest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"grillo.local/grillo/internal/guestproto"
)

// ociVersion is the OCI runtime-spec version the generated bundles target.
const ociVersion = "1.1.0"

// BuildOCIConfig renders the OCI runtime config.json for one container. Network
// and IPC namespaces are deliberately omitted so all containers in the sandbox
// share the guest's Pod network and IPC namespaces; each container gets its own
// mount, PID, UTS, and cgroup namespaces.
func BuildOCIConfig(spec guestproto.ContainerSpec, sandbox guestproto.SandboxSpec) ([]byte, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	caps := spec.Capabilities
	if len(caps) == 0 {
		caps = guestproto.DefaultCapabilities()
	}
	env := spec.Env
	if !hasEnv(env, "PATH") {
		env = append([]string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}, env...)
	}
	cwd := spec.WorkingDir
	if cwd == "" {
		cwd = "/"
	}
	cfg := ociConfig{
		OCIVersion: ociVersion,
		Process: &ociProcess{
			Terminal: spec.TTY,
			User: ociUser{
				UID:            spec.User.UID,
				GID:            spec.User.GID,
				AdditionalGIDs: spec.User.AdditionalGIDs,
			},
			Args:            spec.Args,
			Env:             env,
			Cwd:             cwd,
			Capabilities:    capabilitySets(caps),
			NoNewPrivileges: true,
			RLimits: []ociRLimit{
				{Type: "RLIMIT_NOFILE", Hard: 1024, Soft: 1024},
			},
		},
		Root:     &ociRoot{Path: spec.Rootfs},
		Hostname: sandbox.Hostname,
		Mounts:   ociMounts(spec.Mounts),
		Linux: &ociLinux{
			Namespaces: []ociNamespace{
				{Type: "pid"},
				{Type: "mount"},
				{Type: "uts"},
				{Type: "cgroup"},
			},
			Resources:     buildOCIResources(spec.Resources),
			MaskedPaths:   defaultMaskedPaths(),
			ReadonlyPaths: defaultReadonlyPaths(),
		},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

// WriteBundle writes a bundle directory containing config.json for spec.
func WriteBundle(dir string, spec guestproto.ContainerSpec, sandbox guestproto.SandboxSpec) error {
	data, err := BuildOCIConfig(spec, sandbox)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("guest: create bundle dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600); err != nil {
		return fmt.Errorf("guest: write config.json: %w", err)
	}
	return nil
}

func hasEnv(env []string, key string) bool {
	for _, kv := range env {
		if len(kv) >= len(key)+1 && kv[:len(key)+1] == key+"=" {
			return true
		}
	}
	return false
}

func capabilitySets(caps []string) *ociCapabilities {
	return &ociCapabilities{
		Bounding:    append([]string(nil), caps...),
		Effective:   append([]string(nil), caps...),
		Inheritable: append([]string(nil), caps...),
		Permitted:   append([]string(nil), caps...),
		Ambient:     append([]string(nil), caps...),
	}
}

func ociMounts(mounts []guestproto.MountSpec) []ociMount {
	out := []ociMount{
		{Destination: "/proc", Type: "proc", Source: "proc"},
		{Destination: "/dev", Type: "tmpfs", Source: "tmpfs", Options: []string{"nosuid", "strictatime", "mode=755", "size=65536k"}},
		{Destination: "/dev/pts", Type: "devpts", Source: "devpts", Options: []string{"nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620", "gid=5"}},
		{Destination: "/dev/shm", Type: "tmpfs", Source: "shm", Options: []string{"nosuid", "noexec", "nodev", "mode=1777", "size=65536k"}},
		{Destination: "/dev/mqueue", Type: "mqueue", Source: "mqueue", Options: []string{"nosuid", "noexec", "nodev"}},
		{Destination: "/sys", Type: "sysfs", Source: "sysfs", Options: []string{"nosuid", "noexec", "nodev", "ro"}},
		{Destination: "/sys/fs/cgroup", Type: "cgroup", Source: "cgroup", Options: []string{"nosuid", "noexec", "nodev", "relatime", "ro"}},
		{Destination: "/etc/resolv.conf", Type: "bind", Source: "/etc/resolv.conf", Options: []string{"rbind", "ro"}},
	}
	for _, m := range mounts {
		typ := m.Type
		if typ == "" {
			typ = "bind"
		}
		options := append([]string{"rbind"}, m.Options...)
		if m.ReadOnly {
			options = append(options, "ro")
		} else {
			options = append(options, "rw")
		}
		out = append(out, ociMount{Destination: m.Target, Type: typ, Source: m.Source, Options: options})
	}
	return out
}

func buildOCIResources(r guestproto.ResourceSpec) *ociResources {
	if r.CPUQuotaMicros == 0 && r.CPUPeriodMicros == 0 && r.MemoryBytes == 0 && r.PidsLimit == 0 {
		return nil
	}
	out := &ociResources{}
	if r.CPUQuotaMicros != 0 || r.CPUPeriodMicros != 0 {
		out.CPU = &ociCPU{Quota: r.CPUQuotaMicros, Period: r.CPUPeriodMicros}
	}
	if r.MemoryBytes != 0 {
		out.Memory = &ociMemory{Limit: r.MemoryBytes}
	}
	if r.PidsLimit != 0 {
		out.Pids = &ociPids{Limit: r.PidsLimit}
	}
	return out
}

func defaultMaskedPaths() []string {
	return []string{
		"/proc/acpi",
		"/proc/asound",
		"/proc/kcore",
		"/proc/keys",
		"/proc/latency_stats",
		"/proc/timer_list",
		"/proc/timer_stats",
		"/proc/sched_debug",
		"/sys/firmware",
		"/proc/scsi",
	}
}

func defaultReadonlyPaths() []string {
	return []string{
		"/proc/bus",
		"/proc/fs",
		"/proc/irq",
		"/proc/sys",
		"/proc/sysrq-trigger",
	}
}

// ociConfig is the subset of the OCI runtime specification the guest writes.
type ociConfig struct {
	OCIVersion string      `json:"ociVersion"`
	Process    *ociProcess `json:"process"`
	Root       *ociRoot    `json:"root"`
	Hostname   string      `json:"hostname,omitempty"`
	Mounts     []ociMount  `json:"mounts,omitempty"`
	Linux      *ociLinux   `json:"linux,omitempty"`
}

type ociProcess struct {
	Terminal        bool             `json:"terminal,omitempty"`
	User            ociUser          `json:"user"`
	Args            []string         `json:"args"`
	Env             []string         `json:"env,omitempty"`
	Cwd             string           `json:"cwd"`
	Capabilities    *ociCapabilities `json:"capabilities,omitempty"`
	NoNewPrivileges bool             `json:"noNewPrivileges,omitempty"`
	RLimits         []ociRLimit      `json:"rlimits,omitempty"`
}

type ociUser struct {
	UID            uint32   `json:"uid"`
	GID            uint32   `json:"gid"`
	AdditionalGIDs []uint32 `json:"additionalGids,omitempty"`
}

type ociCapabilities struct {
	Bounding    []string `json:"bounding"`
	Effective   []string `json:"effective"`
	Inheritable []string `json:"inheritable"`
	Permitted   []string `json:"permitted"`
	Ambient     []string `json:"ambient"`
}

type ociRLimit struct {
	Type string `json:"type"`
	Hard uint64 `json:"hard"`
	Soft uint64 `json:"soft"`
}

type ociRoot struct {
	Path     string `json:"path"`
	Readonly bool   `json:"readonly,omitempty"`
}

type ociMount struct {
	Destination string   `json:"destination"`
	Type        string   `json:"type,omitempty"`
	Source      string   `json:"source,omitempty"`
	Options     []string `json:"options,omitempty"`
}

type ociLinux struct {
	Namespaces    []ociNamespace `json:"namespaces,omitempty"`
	Resources     *ociResources  `json:"resources,omitempty"`
	MaskedPaths   []string       `json:"maskedPaths,omitempty"`
	ReadonlyPaths []string       `json:"readonlyPaths,omitempty"`
}

type ociNamespace struct {
	Type string `json:"type"`
	Path string `json:"path,omitempty"`
}

type ociResources struct {
	CPU    *ociCPU    `json:"cpu,omitempty"`
	Memory *ociMemory `json:"memory,omitempty"`
	Pids   *ociPids   `json:"pids,omitempty"`
}

type ociCPU struct {
	Quota  int64  `json:"quota,omitempty"`
	Period uint64 `json:"period,omitempty"`
}

type ociMemory struct {
	Limit int64 `json:"limit,omitempty"`
}

type ociPids struct {
	Limit int64 `json:"limit"`
}
