//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type failedLogWriter struct{}

func (failedLogWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestRuncStreamingReportsOutputFailure(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "fixture-runc")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf fixture-output\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	reaper := NewReaper()
	reaper.Run()
	defer reaper.Stop()
	rt := &Runc{Binary: binary, Root: dir, Reaper: reaper}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	status, err := rt.RunStreaming(ctx, "app", dir, failedLogWriter{}, io.Discard)
	if status.ExitCode != 0 || err == nil {
		t.Fatalf("expected successful process with output-copy failure: status=%+v error=%v", status, err)
	}
}

func TestRuncDetachedLogPipesAndFailureCleanup(t *testing.T) {
	for _, code := range []string{"0", "1"} {
		t.Run(code, func(t *testing.T) {
			dir := t.TempDir()
			binary := filepath.Join(dir, "fixture-runc")
			// A trusted test executable, never workload/source-provided host shell input.
			if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf fixture-out\nprintf fixture-err >&2\nexit "+code+"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			reaper := NewReaper()
			reaper.Run()
			defer reaper.Stop()
			logs := &ContainerLogs{}
			rt := &Runc{Binary: binary, Root: dir, Reaper: reaper, Logs: logs}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err := rt.StartDetached(ctx, "app", dir)
			if (err != nil) != (code == "1") {
				t.Fatalf("start: %v", err)
			}
			if code == "0" {
				found := map[string]string{}
				for ctx.Err() == nil {
					for _, chunk := range logs.Read(0).Chunks {
						found[chunk.Stream] = string(chunk.Data)
					}
					if strings.Contains(found["stdout"], "fixture-out") && strings.Contains(found["stderr"], "fixture-err") {
						break
					}
					time.Sleep(time.Millisecond)
				}
				if ctx.Err() != nil {
					t.Fatalf("logs missing: %v", found)
				}
			}
			rt.closeLogReaders("app")
			rt.logMu.Lock()
			count := len(rt.logReaders)
			rt.logMu.Unlock()
			if count != 0 {
				t.Fatal("retained pipe readers")
			}
		})
	}
}
