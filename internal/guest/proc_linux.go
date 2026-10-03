//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"os"
	"strings"
)

// countZombies returns the number of zombie processes visible to PID 1. A
// correct reaper keeps this at zero; tests assert it to catch orphaned children.
func countZombies() int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	zombies := 0
	for _, entry := range entries {
		name := entry.Name()
		if !isPID(name) {
			continue
		}
		data, err := os.ReadFile("/proc/" + name + "/stat")
		if err != nil {
			continue
		}
		// Layout: "pid (comm) state ..."; comm may contain spaces or parentheses,
		// so read the state after the final ')'.
		i := strings.LastIndexByte(string(data), ')')
		if i >= 0 && i+2 < len(data) && data[i+2] == 'Z' {
			zombies++
		}
	}
	return zombies
}

func isPID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
