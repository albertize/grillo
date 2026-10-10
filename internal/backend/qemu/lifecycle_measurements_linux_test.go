//go:build linux

// SPDX-License-Identifier: Apache-2.0

package qemu

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func lifecyclePercentile(values []int64, percent int) (int64, error) {
	if len(values) == 0 || percent < 1 || percent > 100 {
		return 0, fmt.Errorf("invalid percentile sample")
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	for _, value := range sorted {
		if value < 0 {
			return 0, fmt.Errorf("negative measurement")
		}
	}
	return sorted[(percent*len(sorted)+99)/100-1], nil
}
func logLifecycleSamples(t *testing.T, name string, values []int64) {
	t.Helper()
	median, err := lifecyclePercentile(values, 50)
	if err != nil {
		t.Fatal(err)
	}
	p95, err := lifecyclePercentile(values, 95)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: samples=%d median=%d p95=%d", name, len(values), median, p95)
}
func parseLifecyclePSS(data []byte) (int64, error) {
	var found bool
	var pss int64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "Pss:" {
			continue
		}
		if found || len(fields) != 3 || fields[2] != "kB" {
			return 0, fmt.Errorf("invalid PSS record")
		}
		value, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || value <= 0 || value > 1<<40 {
			return 0, fmt.Errorf("invalid PSS value")
		}
		pss = value * 1024
		found = true
	}
	if !found {
		return 0, fmt.Errorf("PSS unavailable")
	}
	return pss, nil
}
func readLifecyclePSS(pid int) (int64, error) {
	file, err := os.Open(fmt.Sprintf("/proc/%d/smaps_rollup", pid))
	if err != nil {
		return 0, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return 0, fmt.Errorf("invalid bounded PSS snapshot")
	}
	return parseLifecyclePSS(data)
}
func lifecycleLogicalBytes(ctx context.Context, root string) (int64, error) {
	var total int64
	count := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		count++
		if count > 1024 {
			return fmt.Errorf("measurement entry limit")
		}
		if entry.IsDir() || entry.Type()&os.ModeSocket != 0 {
			return nil // sockets have no regular-file payload bytes
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsafe measurement entry")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() < 0 || info.Size() > 2<<30 || total > 4<<30-info.Size() {
			return fmt.Errorf("measurement byte limit")
		}
		total += info.Size()
		return nil
	})
	return total, err
}
func TestLifecycleMeasurements(t *testing.T) {
	values := []int64{100, 1, 2, 3}
	p, err := lifecyclePercentile(values, 50)
	if err != nil || p != 2 || values[0] != 100 {
		t.Fatal(p, err)
	}
	p, err = lifecyclePercentile(values, 95)
	if err != nil || p != 100 {
		t.Fatal(p, err)
	}
	for _, values := range [][]int64{nil, {-1}} {
		if _, err := lifecyclePercentile(values, 50); err == nil {
			t.Fatal("invalid sample accepted")
		}
	}
	for _, data := range []string{"", "Pss: 1 MB", "Pss: -1 kB", "Pss: 1 kB\nPss: 2 kB", "Pss: 9223372036854775807 kB"} {
		if _, err := parseLifecyclePSS([]byte(data)); err == nil {
			t.Fatal("invalid/missing PSS accepted")
		}
	}
	if p, err := parseLifecyclePSS([]byte("Rss: 99 kB\nPss: 3 kB\n")); err != nil || p != 3072 {
		t.Fatal(p, err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "file")
	if err := os.WriteFile(path, []byte("123"), 0600); err != nil {
		t.Fatal(err)
	}
	if value, err := lifecycleLogicalBytes(context.Background(), root); err != nil || value != 3 {
		t.Fatal(value, err)
	}
	listener, err := net.Listen("unix", filepath.Join(root, "qmp.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if value, err := lifecycleLogicalBytes(context.Background(), root); err != nil || value != 3 {
		t.Fatal("socket counted as payload", value, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := lifecycleLogicalBytes(ctx, root); err == nil {
		t.Fatal("cancellation ignored")
	}
	if err := os.Symlink(path, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycleLogicalBytes(context.Background(), root); err == nil {
		t.Fatal("measurement followed a link")
	}
}
