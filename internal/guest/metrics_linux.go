//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"context"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/albertize/grillo/internal/guestproto"
)

func memoryMetrics(text string) (*uint64, *uint64) {
	var total, available *uint64
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[2] != "kB" {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || value > math.MaxUint64/1024 {
			continue
		}
		value *= 1024
		switch fields[0] {
		case "MemTotal:":
			total = &value
		case "MemAvailable:":
			available = &value
		}
	}
	if total == nil || available == nil || *available > *total {
		return nil, nil
	}
	return total, available
}
func cpuMetrics(text string) (*uint64, *uint64) {
	fields := strings.Fields(strings.SplitN(text, "\n", 2)[0])
	if len(fields) < 9 || fields[0] != "cpu" {
		return nil, nil
	}
	values := make([]uint64, 8)
	for i := range values {
		value, err := strconv.ParseUint(fields[i+1], 10, 64)
		if err != nil {
			return nil, nil
		}
		values[i] = value
	}
	// guest/guest_nice are already included in user/nice, so do not add them.
	var busy, idle uint64
	for i, value := range values {
		if i == 7 {
			continue
		} // steal is host scheduling loss, not guest CPU work.
		target := &busy
		if i == 3 || i == 4 {
			target = &idle
		}
		if *target > math.MaxUint64-value {
			return nil, nil
		}
		*target += value
	}
	return &busy, &idle
}
func usageCounter(text, key string) *uint64 {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == key {
			value, err := strconv.ParseUint(fields[1], 10, 64)
			if err == nil {
				return &value
			}
		}
	}
	return nil
}

func cgroupMetrics(procRoot, groupRoot string, pid int) guestproto.ContainerMetrics {
	var out guestproto.ContainerMetrics
	if pid <= 1 {
		return out
	}
	data, err := readCapped(filepath.Join(procRoot, strconv.Itoa(pid), "cgroup"), 16<<10)
	if err != nil {
		return out
	}
	group := ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "0::/") {
			group = strings.TrimPrefix(line, "0::/")
			break
		}
	}
	if group == "" || strings.Contains(group, "..") || strings.ContainsAny(group, "\x00\n") {
		return out
	}
	target := filepath.Join(groupRoot, group)
	memory, err := readCapped(filepath.Join(target, "memory.current"), 128)
	if err == nil {
		value, err := strconv.ParseUint(strings.TrimSpace(string(memory)), 10, 64)
		if err == nil {
			out.MemoryBytes = &value
		}
	}
	cpu, err := readCapped(filepath.Join(target, "cpu.stat"), 4096)
	if err == nil {
		out.CPUUsec = usageCounter(string(cpu), "usage_usec")
	}
	return out
}
func (a *Agent) metrics(ctx context.Context) (any, *guestproto.Error) {
	result := guestproto.MetricsResult{}
	if data, err := readCapped("/proc/meminfo", 16<<10); err == nil {
		result.MemoryTotalBytes, result.MemoryAvailableBytes = memoryMetrics(string(data))
	}
	if data, err := readCapped("/proc/stat", 64<<10); err == nil {
		result.CPUBusyTicks, result.CPUIdleTicks = cpuMetrics(string(data))
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, container := range a.sandbox.Containers {
		if err := ctx.Err(); err != nil {
			return nil, guestproto.Errorf(guestproto.CodeCanceled, "metrics canceled")
		}
		sample := guestproto.ContainerMetrics{Name: container.Name}
		if !container.Init {
			if state, err := a.Runtime.State(ctx, container.Name); err == nil && state.Status == "running" {
				sample = cgroupMetrics("/proc", cgroupRoot, state.PID)
				sample.Name = container.Name
			}
		}
		result.Containers = append(result.Containers, sample)
	}
	return result, nil
}
