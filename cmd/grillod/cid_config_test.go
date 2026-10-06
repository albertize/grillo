//go:build linux

// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestCIDBasesValidateBeforeEffects(t *testing.T) {
	for _, test := range []struct {
		workload, build uint
		valid           bool
	}{
		{20, 200, true}, {32768, 32832, true}, {3, 4294967294, true},
		{0, 200, false}, {2, 200, false}, {20, 1, false}, {20, 20, false}, {4294967295, 200, false},
	} {
		err := validateCIDBases(test.workload, test.build)
		if (err == nil) != test.valid {
			t.Errorf("CID bases %d/%d: %v", test.workload, test.build, err)
		}
	}
}
