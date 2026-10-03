//go:build linux

// SPDX-License-Identifier: Apache-2.0

package observe

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// clockTicksPerSecond is the Linux USER_HZ assumed for /proc CPU accounting.
const clockTicksPerSecond = 100

// ResourceSnapshot is a point-in-time sample of a process's resource usage.
type ResourceSnapshot struct {
	Time       time.Time `json:"time"`
	PID        int       `json:"pid"`
	CPUTimeMS  int64     `json:"cpuTimeMs"`
	CPUPercent float64   `json:"cpuPercent"`
	RSSBytes   int64     `json:"rssBytes"`
	VSZBytes   int64     `json:"vszBytes"`
	Threads    int       `json:"threads"`
}

// SampleProcess reads CPU time, memory, and thread count from /proc.
func SampleProcess(pid int) (ResourceSnapshot, error) {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ResourceSnapshot{}, err
	}
	utime, stime, err := cpuTicks(stat)
	if err != nil {
		return ResourceSnapshot{}, err
	}
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return ResourceSnapshot{}, err
	}
	rss, vsz, threads := parseStatus(status)
	return ResourceSnapshot{
		Time:      time.Now().UTC(),
		PID:       pid,
		CPUTimeMS: int64(utime+stime) * 1000 / clockTicksPerSecond,
		RSSBytes:  rss,
		VSZBytes:  vsz,
		Threads:   threads,
	}, nil
}

func cpuTicks(stat []byte) (utime, stime uint64, err error) {
	s := string(stat)
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+1 >= len(s) {
		return 0, 0, fmt.Errorf("observe: malformed proc stat")
	}
	fields := strings.Fields(s[i+1:])
	// After state (index 0): utime is field 14 (index 11), stime field 15 (index 12).
	if len(fields) <= 12 {
		return 0, 0, fmt.Errorf("observe: short proc stat")
	}
	utime, err = strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	stime, err = strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	return utime, stime, nil
}

func parseStatus(status []byte) (rss, vsz int64, threads int) {
	for _, line := range strings.Split(string(status), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "VmRSS":
			rss = parseKB(value)
		case "VmSize":
			vsz = parseKB(value)
		case "Threads":
			threads, _ = strconv.Atoi(strings.Fields(value)[0])
		}
	}
	return rss, vsz, threads
}

func parseKB(value string) int64 {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0
	}
	kb, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0
	}
	return kb * 1024
}

// Clock is the time source, injectable for deterministic tests.
type Clock interface {
	Now() time.Time
}

// RealClock is the system clock.
type RealClock struct{}

// Now returns the current time.
func (RealClock) Now() time.Time { return time.Now() }

// Sampler samples a fixed set of processes and computes CPU percentages between
// consecutive samples.
type Sampler struct {
	clock Clock

	mu   sync.Mutex
	prev map[int]ResourceSnapshot
}

// NewSampler returns a sampler using clock (default RealClock).
func NewSampler(clock Clock) *Sampler {
	if clock == nil {
		clock = RealClock{}
	}
	return &Sampler{clock: clock, prev: map[int]ResourceSnapshot{}}
}

// Sample returns a snapshot per live pid. Dead processes are skipped and
// forgotten.
func (s *Sampler) Sample(pids []int) []ResourceSnapshot {
	now := s.clock.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ResourceSnapshot, 0, len(pids))
	for _, pid := range pids {
		snapshot, err := SampleProcess(pid)
		if err != nil {
			delete(s.prev, pid)
			continue
		}
		snapshot.Time = now
		if previous, ok := s.prev[pid]; ok {
			elapsedMS := now.Sub(previous.Time).Milliseconds()
			if elapsedMS > 0 {
				snapshot.CPUPercent = float64(snapshot.CPUTimeMS-previous.CPUTimeMS) / float64(elapsedMS) * 100
			}
		}
		s.prev[pid] = snapshot
		out = append(out, snapshot)
	}
	return out
}
