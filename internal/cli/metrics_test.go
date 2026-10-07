//go:build linux

// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/albertize/grillo/internal/observe"
)

type metricClient struct{ fakeClient }

func (*metricClient) View(context.Context, string) (observe.ApplicationView, error) {
	value := uint64(4096)
	return observe.ApplicationView{Application: "demo", Sandboxes: []observe.SandboxView{{ID: "demo-app-0", Guest: &observe.GuestUsageView{MemoryTotalBytes: &value}, Containers: []observe.ContainerObservation{{Name: "app"}}}}}, nil
}
func TestMetricsJSONSuccessAndMissingValues(t *testing.T) {
	for _, format := range []string{"json", "text"} {
		app, out, stderr := newTestApp(t, &metricClient{}, &fakeTerminal{})
		if code := app.Run(context.Background(), []string{"metrics", "demo", "--output=" + format}); code != 0 {
			t.Fatalf("code=%d stderr=%s", code, stderr)
		}
		if format == "json" && !strings.Contains(out.String(), "memoryTotalBytes") {
			t.Fatal("JSON counters missing")
		}
		if format == "text" && !strings.Contains(out.String(), "unavailable") {
			t.Fatal("absent values fabricated")
		}
	}
}
func TestExecWithoutTTYDoesNotMakeRaw(t *testing.T) {
	terminal := &fakeTerminal{tty: true}
	app, _, _ := newTestApp(t, &fakeClient{}, terminal)
	_ = app.Run(context.Background(), []string{"exec", "demo", "app", "--", "true"})
	if terminal.raw != 0 {
		t.Fatal("non-TTY exec changed terminal")
	}
}
