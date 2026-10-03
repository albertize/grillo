// SPDX-License-Identifier: Apache-2.0

package guestproto

import (
	"errors"
	"fmt"
	"strings"
)

// SandboxSpec is the resolved, guest-side description of one microVM's workload.
// The host builds it from the application IR; the guest agent turns each
// ContainerSpec into an OCI bundle and runs it with runc. It is deliberately a
// narrow subset of the IR, not the whole Kubernetes API.
type SandboxSpec struct {
	ID          string          `json:"id"`
	Hostname    string          `json:"hostname,omitempty"`
	Nameservers []string        `json:"nameservers,omitempty"`
	Containers  []ContainerSpec `json:"containers"`
}

// ContainerSpec describes one container to run inside the guest.
type ContainerSpec struct {
	Name            string       `json:"name"`
	Rootfs          string       `json:"rootfs"`
	Init            bool         `json:"init,omitempty"`
	Args            []string     `json:"args"`
	Env             []string     `json:"env,omitempty"`
	WorkingDir      string       `json:"workingDir,omitempty"`
	User            UserSpec     `json:"user,omitempty"`
	TTY             bool         `json:"tty,omitempty"`
	Stdin           bool         `json:"stdin,omitempty"`
	Mounts          []MountSpec  `json:"mounts,omitempty"`
	Resources       ResourceSpec `json:"resources,omitempty"`
	Capabilities    []string     `json:"capabilities,omitempty"`
	NoNewPrivileges bool         `json:"noNewPrivileges,omitempty"`
}

// UserSpec is the container process identity.
type UserSpec struct {
	UID            uint32   `json:"uid"`
	GID            uint32   `json:"gid"`
	AdditionalGIDs []uint32 `json:"additionalGids,omitempty"`
}

// MountSpec is a bind or tmpfs mount inside a container.
type MountSpec struct {
	Source   string   `json:"source,omitempty"`
	Target   string   `json:"target"`
	Type     string   `json:"type,omitempty"`
	Options  []string `json:"options,omitempty"`
	ReadOnly bool     `json:"readOnly,omitempty"`
}

// ResourceSpec holds enforced cgroup v2 limits.
type ResourceSpec struct {
	CPUQuotaMicros  int64  `json:"cpuQuotaMicros,omitempty"`
	CPUPeriodMicros uint64 `json:"cpuPeriodMicros,omitempty"`
	MemoryBytes     int64  `json:"memoryBytes,omitempty"`
	PidsLimit       int64  `json:"pidsLimit,omitempty"`
}

// Validate checks a sandbox spec before the guest acts on it.
func (s SandboxSpec) Validate() error {
	if s.ID == "" {
		return errors.New("guestproto: sandbox id is required")
	}
	if len(s.Containers) == 0 {
		return errors.New("guestproto: sandbox has no containers")
	}
	seen := make(map[string]bool, len(s.Containers))
	nameless := 0
	for i, c := range s.Containers {
		if c.Name == "" {
			return fmt.Errorf("guestproto: container %d has no name", i)
		}
		if seen[c.Name] {
			return fmt.Errorf("guestproto: duplicate container name %q", c.Name)
		}
		seen[c.Name] = true
		if err := c.Validate(); err != nil {
			return fmt.Errorf("guestproto: container %q: %w", c.Name, err)
		}
		if !c.Init {
			nameless++
		}
	}
	if nameless == 0 {
		return errors.New("guestproto: sandbox has no application container")
	}
	return nil
}

// Validate checks one container spec.
func (c ContainerSpec) Validate() error {
	if c.Rootfs == "" {
		return errors.New("rootfs is required")
	}
	if !strings.HasPrefix(c.Rootfs, "/") {
		return fmt.Errorf("rootfs %q must be an absolute path", c.Rootfs)
	}
	if len(c.Args) == 0 {
		return errors.New("args are required")
	}
	for _, m := range c.Mounts {
		if m.Target == "" {
			return errors.New("mount target is required")
		}
		if !strings.HasPrefix(m.Target, "/") {
			return fmt.Errorf("mount target %q must be absolute", m.Target)
		}
		if m.Type == "" {
			m.Type = "bind"
		}
	}
	if c.Resources.MemoryBytes < 0 || c.Resources.PidsLimit < 0 {
		return errors.New("resource limits must not be negative")
	}
	return nil
}

// DefaultCapabilities is the least-privilege capability set applied when a
// container does not request one. It matches the Docker/containerd default.
func DefaultCapabilities() []string {
	return []string{
		"CAP_CHOWN",
		"CAP_DAC_OVERRIDE",
		"CAP_FOWNER",
		"CAP_FSETID",
		"CAP_KILL",
		"CAP_SETGID",
		"CAP_SETUID",
		"CAP_SETPCAP",
		"CAP_NET_BIND_SERVICE",
		"CAP_NET_RAW",
		"CAP_SYS_CHROOT",
		"CAP_MKNOD",
		"CAP_AUDIT_WRITE",
		"CAP_SETFCAP",
	}
}
