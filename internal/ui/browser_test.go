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

func TestFirefoxConsole(t *testing.T) { browserConsole(t, "firefox", "firefox") }
func TestChromeConsole(t *testing.T)  { browserConsole(t, "google-chrome", "chrome") }

// Rendering/transport fixture evidence, not real microVM or PTY evidence.
func browserConsole(t *testing.T, tool, browser string) {
	t.Helper()
	for _, name := range []string{tool, "node"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("browser prerequisite missing: %s", name)
		}
	}
	core := &richCore{}
	bridge := New(core)
	server := httptest.NewServer(bridge.Handler())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "../../scripts/ui-browser-smoke.mjs")
	cmd.Env = append(os.Environ(), "GRILLO_UI_TEST_URL="+bridge.URL(server.Listener.Addr().String()), "GRILLO_UI_BROWSER="+browser)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s smoke failed: %v\n%s", browser, err, output)
	}
	t.Log(string(output))
	core.mu.Lock()
	defer core.mu.Unlock()
	if core.since == 0 {
		t.Fatal("browser reconnect did not resume from event ID")
	}
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	if len(bridge.terminals) != 0 {
		t.Fatal("browser terminal session leaked")
	}
}
