//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMetricParsingBoundsAndMissingValues(t *testing.T) {
	total, available := memoryMetrics("MemTotal: 1000 kB\nMemAvailable: 400 kB\n")
	if total == nil || *total != 1024000 || available == nil || *available != 409600 {
		t.Fatal("memory units incorrect")
	}
	for _, text := range []string{"", "MemTotal: 1 kB\nMemAvailable: 2 kB\n", "MemTotal: 18446744073709551615 kB\nMemAvailable: 1 kB\n"} {
		a, b := memoryMetrics(text)
		if a != nil || b != nil {
			t.Fatal("invalid memory accepted")
		}
	}
	busy, idle := cpuMetrics("cpu 1 2 3 4 5 6 7 8 1 1\ncpu0 ...")
	if busy == nil || *busy != 19 || idle == nil || *idle != 9 {
		t.Fatal("CPU accounting incorrect")
	}
	for _, text := range []string{"cpu 1 2", "cpu x 0 0 0 0 0 0 0", "cpu 18446744073709551615 1 0 0 0 0 0 0"} {
		a, b := cpuMetrics(text)
		if a != nil || b != nil {
			t.Fatal("invalid CPU accepted")
		}
	}
}
func TestCgroupMetricProvenanceAndMissingValues(t *testing.T) {
	proc := t.TempDir()
	groups := t.TempDir()
	pidDir := filepath.Join(proc, "42")
	group := filepath.Join(groups, "app")
	if err := os.Mkdir(pidDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(group, 0o700); err != nil {
		t.Fatal(err)
	}
	for path, text := range map[string]string{filepath.Join(pidDir, "cgroup"): "0::/app\n", filepath.Join(group, "memory.current"): "123456\n", filepath.Join(group, "cpu.stat"): "usage_usec 98765\nuser_usec 1\n"} {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sample := cgroupMetrics(proc, groups, 42)
	if sample.MemoryBytes == nil || *sample.MemoryBytes != 123456 || sample.CPUUsec == nil || *sample.CPUUsec != 98765 {
		t.Fatal("missing cgroup sample")
	}
	if err := os.Remove(filepath.Join(group, "memory.current")); err != nil {
		t.Fatal(err)
	}
	sample = cgroupMetrics(proc, groups, 42)
	if sample.MemoryBytes != nil || sample.CPUUsec == nil {
		t.Fatal("missing metric fabricated")
	}
	if err := os.WriteFile(filepath.Join(pidDir, "cgroup"), []byte("0::/../escape\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sample = cgroupMetrics(proc, groups, 42)
	if sample.MemoryBytes != nil || sample.CPUUsec != nil {
		t.Fatal("unsafe cgroup accepted")
	}
}
