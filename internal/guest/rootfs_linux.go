//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/albertize/grillo/internal/guestproto"
	"golang.org/x/sys/unix"
)

// prepareRoot creates an overlay in guest runtime tmpfs. Its upper layer is
// private to this container and dies with the VM, never modifying the host CAS.
// Fail closed when overlayfs is unavailable; there is no shared writable fallback.
func prepareRoot(workDir string, c guestproto.ContainerSpec, mount func(string, string, string, uintptr, string) error) (guestproto.ContainerSpec, error) {
	if !c.PrivateRoot {
		return c, nil
	}
	if err := c.Validate(); err != nil {
		return c, err
	}
	if strings.ContainsAny(c.Rootfs, ",:\\") {
		return c, fmt.Errorf("guest: invalid overlay lower path")
	}
	dir := filepath.Join(workDir, "roots", c.Name)
	if strings.ContainsAny(dir, ",:\\") {
		return c, fmt.Errorf("guest: invalid overlay work path")
	}
	for _, sub := range []string{"upper", "work", "merged"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0700); err != nil {
			return c, err
		}
	}
	target := filepath.Join(dir, "merged")
	opts := "lowerdir=" + c.Rootfs + ",upperdir=" + filepath.Join(dir, "upper") + ",workdir=" + filepath.Join(dir, "work")
	if err := mount("overlay", target, "overlay", 0, opts); err != nil {
		return c, fmt.Errorf("guest: private root for %s: %w", c.Name, err)
	}
	c.Rootfs = target
	return c, nil
}

func prepareContainerRoot(workDir string, c guestproto.ContainerSpec) (guestproto.ContainerSpec, error) {
	return prepareRoot(workDir, c, unix.Mount)
}
