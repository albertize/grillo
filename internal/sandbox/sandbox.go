// SPDX-License-Identifier: Apache-2.0

// Package sandbox defines the VMM-independent contracts the runtime uses to run
// one microVM. Concrete adapters (currently internal/backend/qemu) implement
// Backend; nothing here imports a VMM, the guest protocol, or the IR.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// OperationID makes Create idempotent: repeating an operation returns the same
// sandbox instead of creating a second one.
type OperationID string

// Share is a host directory exposed to the guest over a shared-filesystem device.
type Share struct {
	Tag      string
	HostPath string
	ReadOnly bool
}

// Disk is a block device attached to the guest.
type Disk struct {
	Path     string
	ReadOnly bool
	Format   string // "raw"
}

// Network is a pre-rendered QEMU netdev/device pair. The network layer (T11)
// owns address allocation; an empty Network disables networking.
type Network struct {
	Netdev string
	Device string
}

// Spec is the resolved configuration for one sandbox.
type Spec struct {
	ID           string
	Application  string
	Kernel       string
	Initramfs    string
	KernelArgs   string
	VCPU         int
	MemoryMiB    int
	Shares       []Share
	Disks        []Disk
	Network      *Network
	VsockCID     uint32
	VsockPort    uint32
	GuestKey     []byte
	GuestSandbox string
}

// Validate checks the required fields of a spec.
func (s Spec) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("%w: missing sandbox id", ErrInvalidSpec)
	}
	if s.Kernel == "" || s.Initramfs == "" {
		return fmt.Errorf("%w: missing kernel or initramfs", ErrInvalidSpec)
	}
	if s.VsockPort == 0 {
		return fmt.Errorf("%w: missing vsock port", ErrInvalidSpec)
	}
	if len(s.GuestKey) < 16 {
		return fmt.Errorf("%w: guest key too short", ErrInvalidSpec)
	}
	seen := map[string]bool{}
	for _, share := range s.Shares {
		if share.Tag == "" || share.HostPath == "" {
			return fmt.Errorf("%w: share needs a tag and host path", ErrInvalidSpec)
		}
		if seen[share.Tag] {
			return fmt.Errorf("%w: duplicate share tag %q", ErrInvalidSpec, share.Tag)
		}
		seen[share.Tag] = true
	}
	for _, disk := range s.Disks {
		if disk.Path == "" {
			return fmt.Errorf("%w: disk needs a path", ErrInvalidSpec)
		}
	}
	return nil
}

// Handle identifies a sandbox for later operations.
type Handle struct {
	ID        string
	Operation OperationID
}

// State is the observed lifecycle state of a sandbox.
type State string

const (
	StateAbsent  State = "absent"
	StateCreated State = "created"
	StateRunning State = "running"
	StateStopped State = "stopped"
	StateFailed  State = "failed"
)

// Observation is the result of Inspect.
type Observation struct {
	State           State
	PID             int
	GuestState      string
	GuestAlive      bool
	GuestZombies    int
	GuestContainers int
	Reason          string
}

// Capabilities reports what the host can actually do. A theoretically possible
// feature is not an available feature: each is Supported only after a probe.
type Capabilities struct {
	KVM      Feature
	VirtioFS Feature
	Vsock    Feature
}

// Feature is one probed capability with a human-readable reason when unavailable.
type Feature struct {
	Supported bool
	Reason    string
}

// Backend runs sandboxes. Every blocking operation accepts a context; Create is
// idempotent by OperationID; stopping or deleting an absent sandbox succeeds.
type Backend interface {
	Capabilities(ctx context.Context) (Capabilities, error)
	Create(ctx context.Context, spec Spec, op OperationID) (Handle, error)
	Start(ctx context.Context, handle Handle) error
	Inspect(ctx context.Context, handle Handle) (Observation, error)
	Stop(ctx context.Context, handle Handle, grace time.Duration) error
	Delete(ctx context.Context, handle Handle) error
}

// Errors returned by backends.
var (
	ErrNotFound    = errors.New("sandbox: not found")
	ErrInvalidSpec = errors.New("sandbox: invalid spec")
	ErrUnsupported = errors.New("sandbox: unsupported")
	ErrConflict    = errors.New("sandbox: operation conflicts with an existing sandbox")
)
