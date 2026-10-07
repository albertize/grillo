//go:build linux

// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"github.com/albertize/grillo/internal/observe"
)

func (a *App) cmdMetrics(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("metrics", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	output := fs.String("output", "text", "text or json")
	flags, pos, _, err := SplitForFlagSet(args, fs)
	if err != nil {
		return usageError(a.Stderr, "metrics", err)
	}
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if len(pos) != 1 || *output != "text" && *output != "json" {
		return usageError(a.Stderr, "metrics", fmt.Errorf("requires one application and text/json output"))
	}
	if code := a.ensureDaemon(ctx); code != 0 {
		return code
	}
	client, ok := a.ClientFactory(a.SocketPath).(interface {
		View(context.Context, string) (observe.ApplicationView, error)
	})
	if !ok {
		return fail(a.Stderr, fmt.Errorf("metrics API unavailable"))
	}
	view, err := client.View(ctx, pos[0])
	if err != nil {
		return fail(a.Stderr, err)
	}
	if *output == "json" {
		if err := json.NewEncoder(a.Stdout).Encode(view); err != nil {
			return fail(a.Stderr, err)
		}
		return 0
	}
	value := func(p *uint64) string {
		if p == nil {
			return "unavailable"
		}
		return fmt.Sprint(*p)
	}
	for _, s := range view.Sandboxes {
		fmt.Fprintf(a.Stdout, "%s guest-budget=%d bytes", s.ID, s.GuestBudgetBytes)
		if s.VMM != nil {
			fmt.Fprintf(a.Stdout, " vmm-rss=%d bytes vmm-cpu=%d ms", s.VMM.RSSBytes, s.VMM.CPUTimeMS)
		} else {
			fmt.Fprint(a.Stdout, " vmm-usage=unavailable")
		}
		if s.Guest != nil {
			fmt.Fprintf(a.Stdout, " guest-total=%s bytes guest-available=%s bytes guest-busy=%s ticks guest-idle=%s ticks", value(s.Guest.MemoryTotalBytes), value(s.Guest.MemoryAvailableBytes), value(s.Guest.CPUBusyTicks), value(s.Guest.CPUIdleTicks))
		} else {
			fmt.Fprint(a.Stdout, " guest-usage=unavailable")
		}
		fmt.Fprintln(a.Stdout)
		for _, c := range s.Containers {
			fmt.Fprintf(a.Stdout, "  %s", c.Name)
			if c.Usage != nil {
				fmt.Fprintf(a.Stdout, " cgroup-memory=%s bytes cgroup-cpu=%s us", value(c.Usage.MemoryBytes), value(c.Usage.CPUUsec))
			} else {
				fmt.Fprint(a.Stdout, " cgroup-usage=unavailable")
			}
			fmt.Fprintln(a.Stdout)
		}
	}
	return 0
}
