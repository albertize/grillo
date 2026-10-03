//go:build linux

// SPDX-License-Identifier: Apache-2.0

package network

import (
	"errors"
	"fmt"
)

// ErrUnsupported is returned for network topologies the rootless backend cannot
// isolate. They are rejected rather than silently flattened.
var ErrUnsupported = errors.New("network: unsupported topology")

// NetworkAttachment describes which logical networks a sandbox joins.
type NetworkAttachment struct {
	Application string
	Networks    []string
}

// PeersAllowed reports whether two sandboxes may communicate. Sandboxes in the
// same application share the application network. Different applications are
// isolated; cross-application sharing requires an explicitly implemented shared
// network and is not allowed here.
func PeersAllowed(a, b NetworkAttachment) bool {
	if a.Application == "" || b.Application == "" {
		return false
	}
	return a.Application == b.Application
}

// Validate rejects attachments the backend cannot isolate. Multiple logical
// networks on one sandbox would be flattened by a single network namespace, so
// they are rejected until per-network isolation exists.
func Validate(attachments []NetworkAttachment) error {
	for _, attachment := range attachments {
		if len(attachment.Networks) > 1 {
			return fmt.Errorf("%w: sandbox in application %q joins %d networks", ErrUnsupported, attachment.Application, len(attachment.Networks))
		}
	}
	return nil
}
