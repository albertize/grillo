//go:build linux && kvm

// SPDX-License-Identifier: Apache-2.0

package distribution

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/observe"
	linux "github.com/albertize/grillo/internal/platform/linux"
	"golang.org/x/sys/unix"
)

// This opt-in collector measures only processes owned by this unique installed
// test directory and their descendants. Peaks are sampled (100ms), not guaranteed
// instantaneous maxima. Summed RSS double-counts shared pages and is not PSS.
type installedResources struct {
	mu       sync.Mutex
	peakRSS  int64
	samples  int
	err      error
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

func startInstalledResources(t *testing.T, work string) *installedResources {
	t.Helper()
	c := &installedResources{stop: make(chan struct{}), done: make(chan struct{})}
	t.Cleanup(c.close)
	go func() {
		defer close(c.done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			rss, _, err := installedProcessUsage(work)
			c.mu.Lock()
			if err != nil && c.err == nil {
				c.err = err
			}
			if rss > c.peakRSS {
				c.peakRSS = rss
			}
			c.samples++
			c.mu.Unlock()
			select {
			case <-c.stop:
				return
			case <-ticker.C:
			}
		}
	}()
	return c
}

func installedProcessUsage(work string) (int64, int64, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, 0, err
	}
	type process struct {
		id     linux.ProcessID
		parent int
		ticks  int64
		owned  bool
	}
	processes := map[int]process{}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		id, err := linux.Identify(pid)
		if err != nil {
			continue
		} // processes that exit during discovery are absent
		stat, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		end := bytes.LastIndexByte(stat, ')')
		if end < 0 {
			return 0, 0, fmt.Errorf("invalid process stat")
		}
		fields := strings.Fields(string(stat[end+1:]))
		if len(fields) < 20 {
			return 0, 0, fmt.Errorf("short process stat")
		}
		parent, err := strconv.Atoi(fields[1])
		if err != nil {
			return 0, 0, err
		}
		user, err := strconv.ParseInt(fields[11], 10, 64)
		if err != nil {
			return 0, 0, err
		}
		system, err := strconv.ParseInt(fields[12], 10, 64)
		if err != nil {
			return 0, 0, err
		}
		cmdline, _ := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		processes[pid] = process{id: id, parent: parent, ticks: user + system, owned: bytes.Contains(cmdline, []byte(work))}
	}
	for range 128 {
		changed := false
		for pid, p := range processes {
			if !p.owned && processes[p.parent].owned {
				p.owned = true
				processes[pid] = p
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	var rss, ticks int64
	for _, p := range processes {
		if !p.owned || !p.id.Alive() {
			continue
		}
		sample, err := observe.SampleProcess(p.id.PID)
		if err != nil {
			if !p.id.Alive() {
				continue
			}
			return 0, 0, err
		}
		rss += sample.RSSBytes
		ticks += p.ticks
	}
	return rss, ticks, nil
}

func installedDiskUsage(root string) (int64, int64, error) {
	var logical, allocated int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		var stat unix.Stat_t
		if err := unix.Lstat(path, &stat); err != nil {
			return err
		}
		allocated += stat.Blocks * 512 // includes metadata directory blocks, not targets
		if entry.Type().IsRegular() {
			logical += stat.Size
		}
		return nil
	})
	return logical, allocated, err
}

func (c *installedResources) close() {
	c.stopOnce.Do(func() { close(c.stop) })
	<-c.done
}

func finishInstalledResources(t *testing.T, c *installedResources, work, phase string, start time.Time) {
	t.Helper()
	c.close()
	ready := time.Since(start)
	idleRSS, before, err := installedProcessUsage(work)
	if err != nil {
		t.Fatal(err)
	}
	idleStart := time.Now()
	time.Sleep(time.Second)
	_, after, err := installedProcessUsage(work)
	if err != nil || after < before {
		t.Fatal("idle sample invalid", err)
	}
	// Linux /proc CPU ticks are USER_HZ; report raw ticks, not inferred percentages.
	logical, physical, err := installedDiskUsage(work)
	if err != nil || c.err != nil {
		t.Fatal("resource collection", err, c.err)
	}
	data, err := json.Marshal(map[string]any{"phase": phase, "ready_us": ready.Microseconds(), "sampled_peak_rss_bytes": c.peakRSS, "rss_samples": c.samples, "idle_rss_bytes": idleRSS, "idle_cpu_user_hz_ticks": after - before, "idle_interval_us": time.Since(idleStart).Microseconds(), "logical_bytes": logical, "allocated_bytes": physical})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("RESOURCE_SAMPLE", string(data))
}
