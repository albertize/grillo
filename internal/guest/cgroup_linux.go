//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// cgroupRoot is the cgroup v2 mount point in the guest.
const cgroupRoot = "/sys/fs/cgroup"

// EnableCgroupControllers enables the named controllers on the cgroup v2 root so
// runc can apply per-container limits. Enabling an already-enabled controller is
// a no-op.
func EnableCgroupControllers(controllers ...string) error {
	if len(controllers) == 0 {
		return nil
	}
	var b strings.Builder
	for _, c := range controllers {
		b.WriteString("+" + c + " ")
	}
	path := filepath.Join(cgroupRoot, "cgroup.subtree_control")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(b.String())), 0o644); err != nil {
		if errors.Is(err, unix.EBUSY) {
			return nil
		}
		return fmt.Errorf("guest: enable cgroup controllers: %w", err)
	}
	return nil
}

// ApplyCgroupLimits writes resource limits into a cgroup v2 directory. It is used
// for limits the guest enforces outside runc; runc applies the bundle's own
// linux.resources.
func ApplyCgroupLimits(dir string, r ResourceLimits) error {
	if r.MemoryBytes > 0 {
		if err := writeCgroup(filepath.Join(dir, "memory.max"), fmt.Sprintf("%d", r.MemoryBytes)); err != nil {
			return err
		}
	}
	if r.PidsLimit > 0 {
		if err := writeCgroup(filepath.Join(dir, "pids.max"), fmt.Sprintf("%d", r.PidsLimit)); err != nil {
			return err
		}
	}
	if r.CPUQuotaMicros > 0 || r.CPUPeriodMicros > 0 {
		period := r.CPUPeriodMicros
		if period == 0 {
			period = 100000
		}
		if err := writeCgroup(filepath.Join(dir, "cpu.max"), fmt.Sprintf("%d %d", r.CPUQuotaMicros, period)); err != nil {
			return err
		}
	}
	return nil
}

func writeCgroup(path, value string) error {
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		return fmt.Errorf("guest: write %s: %w", path, err)
	}
	return nil
}

// ResourceLimits mirrors the enforced subset of guestproto.ResourceSpec without
// importing it, keeping the cgroup helper usable on its own.
type ResourceLimits struct {
	CPUQuotaMicros  int64
	CPUPeriodMicros uint64
	MemoryBytes     int64
	PidsLimit       int64
}
