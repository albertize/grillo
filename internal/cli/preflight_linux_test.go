//go:build linux

// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadOnlyPreflightBoundsAndRedactsTools(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nprintf 'synthetic-private-output'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRILLO_HELM_BINARY", tool)
	checks := toolCapabilityChecks(context.Background())
	found := false
	for _, check := range checks {
		if strings.Contains(check.Detail, "synthetic-private-output") {
			t.Fatal("raw tool output exposed")
		}
		if check.Name == "helm-version" {
			found = true
			if check.Status != "fail" {
				t.Fatal("wrong renderer accepted")
			}
		}
	}
	if !found {
		t.Fatal("missing renderer version check")
	}
	if err := os.WriteFile(tool, []byte("#!/bin/sh\ni=0; while [ $i -lt 40000 ]; do printf 'xxxxxxxxxxxxxxxx'; i=$((i+1)); done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := readOnlyTool(context.Background(), tool); err == nil {
		t.Fatal("unbounded preflight output accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readOnlyTool(ctx, tool); err == nil {
		t.Fatal("canceled preflight executed")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("read-only helper probe created files")
	}
}
