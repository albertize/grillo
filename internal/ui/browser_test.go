//go:build linux && browser

// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

// This proves rendering in a real browser, not real microVM execution. The
// separate daemon KVM gate proves the bridge shares workload lifetime/exec.
func TestFirefoxConsole(t *testing.T) {
	for _, tool := range []string{"firefox", "node"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("browser prerequisite missing: %s", tool)
		}
	}
	core := &richCore{}
	bridge := New(core)
	server := httptest.NewServer(bridge.Handler())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "../../scripts/ui-browser-smoke.mjs")
	cmd.Env = append(os.Environ(), "GRILLO_UI_TEST_URL="+bridge.URL(server.Listener.Addr().String()))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Firefox smoke failed: %v\n%s", err, output)
	}
	t.Log(string(output))
	core.mu.Lock()
	defer core.mu.Unlock()
	if core.since == 0 {
		t.Fatal("browser reconnect did not resume from event ID")
	}
}
