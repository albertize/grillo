//go:build linux && amd64

// SPDX-License-Identifier: Apache-2.0

// Command run is the T01 host-side feasibility harness CLI. It boots a single
// Firecracker microVM per cycle via the shared spike package, executes the
// guest payload, streams output, and stops the VM.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"grillo.local/grillo/experiments/boot/spike"
)

func main() {
	var (
		fcPath    = flag.String("firecracker", "experiments/artifacts/dependencies-v1/bin/firecracker", "firecracker binary")
		kernel    = flag.String("kernel", "experiments/artifacts/t01/vmlinux", "uncompressed guest kernel")
		initramfs = flag.String("initramfs", "experiments/artifacts/t01/initramfs.cpio.gz", "gzip initramfs")
		cycles    = flag.Int("cycles", 1, "boot/stop cycles")
		keep      = flag.Bool("keep", false, "keep per-cycle logs")
	)
	flag.Parse()

	logDir := ""
	if *keep {
		dir, err := os.MkdirTemp("", "grillo-t01-logs-")
		if err != nil {
			fmt.Fprintln(os.Stderr, "run: create log dir:", err)
			os.Exit(1)
		}
		logDir = dir
		fmt.Println("keeping firecracker logs in", dir)
	}

	results := make([]spike.Result, 0, *cycles)
	for i := 0; i < *cycles; i++ {
		var log io.Writer = io.Discard
		if logDir != "" {
			f, err := os.Create(filepath.Join(logDir, fmt.Sprintf("cycle-%02d.log", i)))
			if err != nil {
				fmt.Fprintln(os.Stderr, "run: create log:", err)
				os.Exit(1)
			}
			defer f.Close()
			log = f
		} else if *cycles == 1 {
			log = os.Stderr
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		res, err := spike.RunOnce(ctx, spike.Options{
			Firecracker: *fcPath,
			Kernel:      *kernel,
			Initramfs:   *initramfs,
			Log:         log,
		})
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "cycle %d: FAIL: %v\n", i+1, err)
			os.Exit(1)
		}
		if !strings.Contains(res.Stdout, "grillo-testcmd stdout") || res.ExitCode != 0 {
			fmt.Fprintf(os.Stderr, "cycle %d: unexpected payload: exit=%d stdout=%q\n", i+1, res.ExitCode, res.Stdout)
			os.Exit(1)
		}
		results = append(results, res)
		fmt.Printf("cycle %d: boot=%s exec=%s stop=%s total=%s exit=%d\n",
			i+1, res.BootDuration.Round(time.Millisecond), res.ExecDuration.Round(time.Millisecond),
			res.StopDuration.Round(time.Millisecond), res.Total.Round(time.Millisecond), res.ExitCode)
	}
	summarize(results)
}

func summarize(results []spike.Result) {
	if len(results) < 2 {
		return
	}
	boots := make([]time.Duration, len(results))
	totals := make([]time.Duration, len(results))
	for i, r := range results {
		boots[i] = r.BootDuration
		totals[i] = r.Total
	}
	sort.Slice(boots, func(i, j int) bool { return boots[i] < boots[j] })
	sort.Slice(totals, func(i, j int) bool { return totals[i] < totals[j] })
	fmt.Printf("summary: cycles=%d boot_median=%s boot_p95=%s total_median=%s total_p95=%s\n",
		len(results),
		median(boots).Round(time.Millisecond), percentile(boots, 0.95).Round(time.Millisecond),
		median(totals).Round(time.Millisecond), percentile(totals, 0.95).Round(time.Millisecond))
}

func median(sorted []time.Duration) time.Duration { return percentile(sorted, 0.5) }

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[int(float64(len(sorted)-1)*p)]
}
