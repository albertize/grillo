//go:build linux

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"fmt"
	"grillo.local/grillo/internal/guestproto"
	"grillo.local/grillo/internal/model"
)

func containerSecurity(c model.Container) (guestproto.UserSpec, bool, error) {
	user, err := parseUser(c.User)
	if err != nil {
		return user, false, err
	}
	p := c.SecurityProfile
	if p == nil {
		return user, false, nil
	}
	if p.Privileged || p.SeccompProfile != "" || len(p.CapabilitiesAdd) > 0 || len(p.CapabilitiesDrop) > 0 {
		return user, false, fmt.Errorf("executor: container %q requests unsupported security policy", c.Name)
	}
	for _, id := range []*int64{p.RunAsUser, p.RunAsGroup} {
		if id != nil && (*id < 0 || *id > 1<<32-1) {
			return user, false, fmt.Errorf("executor: security identity must be a uint32")
		}
	}
	if p.RunAsUser != nil {
		user.UID = uint32(*p.RunAsUser)
	}
	if p.RunAsGroup != nil {
		user.GID = uint32(*p.RunAsGroup)
	}
	return user, p.ReadOnlyRootFilesystem, nil
}

// ValidateDesired rejects unsupported policy before any runtime effects. It is
// also used by the daemon for native API input, not just frontend compilation.
func ValidateDesired(app model.Application) error {
	for _, w := range app.Workloads {
		for _, c := range allContainers(w) {
			if c.SecurityProfile == nil {
				c.SecurityProfile = &w.Template.SecurityProfile
			}
			if _, _, err := containerSecurity(c); err != nil {
				return err
			}
			if _, err := resources(c.Resources); err != nil {
				return err
			}
			for _, m := range c.Mounts {
				if m.SubPath != "" {
					return fmt.Errorf("executor: subPath mounts are not implemented")
				}
			}
		}
	}
	return nil
}
