//go:build linux && amd64

// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A local fixture only: these tests prove harness failure handling, not KVM.
func TestNetProcessOutcome(t *testing.T) {
	for _, tc := range []struct {
		name, tail string
		fail       bool
	}{
		{"success", "exit 0", false},
		{"crash-after-pass", "exit 7", true},
		{"hang-after-pass", "exec sleep 30", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			script := filepath.Join(dir, "qemu-fixture")
			body := "#!/bin/sh\nwhile [ $# -gt 0 ]; do\n if [ \"$1\" = -serial ]; then shift; log=${1#file:}; fi\n shift\ndone\nprintf 'NET RESULT: PASS\\n' > \"$log\"\n" + tc.tail + "\n"
			if err := os.WriteFile(script, []byte(body), 0700); err != nil {
				t.Fatal(err)
			}
			serial, err := runNet(context.Background(), options{QEMU: script, WorkDir: dir, BootTimeout: 200 * time.Millisecond})
			if (err != nil) != tc.fail {
				t.Fatalf("err=%v, want failure=%v", err, tc.fail)
			}
			if !strings.Contains(serial, "NET RESULT: PASS") {
				t.Fatalf("fixture never reached PASS: %s", serial)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 1 {
				t.Fatalf("work directory leaked: %v", entries)
			}
		})
	}
}

func TestProcessCancellation(t *testing.T) {
	m, err := startProcess("sleep", "30")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.wait(ctx, time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestHostUpdateHandshake(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, sentinelName)
	if err := os.WriteFile(path, []byte(sentinelData), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- updateHostSentinel(ctx, dir) }()
	time.Sleep(30 * time.Millisecond)
	data, _ := os.ReadFile(path)
	if string(data) != sentinelData {
		t.Fatal("host modified sentinel before guest readiness")
	}
	if err := os.WriteFile(filepath.Join(dir, "guest-ready"), []byte("ready\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "host-sentinel-v2\n" {
		t.Fatalf("got %q", data)
	}
}

func TestHostUpdateCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := updateHostSentinel(ctx, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
