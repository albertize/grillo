//go:build linux

// SPDX-License-Identifier: Apache-2.0

package main

import "fmt"

// Guest CIDs are host-wide, even with independent per-user/XDG daemon state.
// Configuration is explicit; this is not a global CID allocator or a guarantee
// that an entire range is free. Actual collisions still fail at VMM startup.
func validateCIDBases(workload, build uint) error {
	for _, base := range []uint{workload, build} {
		if base < 3 || uint64(base) > uint64(1<<32)-2 {
			return fmt.Errorf("guest CID bases must be between 3 and 4294967294")
		}
	}
	if workload == build {
		return fmt.Errorf("workload and build CID bases must differ; use nonconflicting ranges")
	}
	return nil
}
