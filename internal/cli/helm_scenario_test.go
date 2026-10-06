//go:build linux

// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/plan"
	"github.com/albertize/grillo/internal/source"
)

func scenarioChart(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "examples", "helm", "scenario-c")
}

func TestHelmScenarioCPlanCounts(t *testing.T) {
	requireCLIHelm(t)
	layout := isolateCLIState(t)
	app, stdout, stderr := newTestApp(t, &fakeClient{}, &fakeTerminal{})
	app.Ensure = func(context.Context, string, string, time.Duration) error { t.Fatal("plan started daemon"); return nil }
	for _, output := range []string{"text", "json"} {
		stdout.Reset()
		stderr.Reset()
		if code := app.Run(context.Background(), []string{"plan", scenarioChart(t), "--release", "scenario-c", "--output", output}); code != 0 {
			t.Fatal("expected supported offline preview", code, stderr.String())
		}
		if strings.Contains(stdout.String()+stderr.String(), "synthetic-t22-token") {
			t.Fatal("secret leaked")
		}
		if output == "text" {
			if strings.Contains(stdout.String(), "BLOCKED") {
				t.Fatal("supported plan marked blocked")
			}
			if !strings.Contains(stdout.String(), "2 workloads, 3 sandboxes, 5 containers, 2 init containers, 1 services, 1 routes, 2 volumes, 1 configs, 1 secrets") {
				t.Fatal("incorrect text counts", stdout.String())
			}
		} else {
			var result struct {
				plan.Plan
				Counts     plan.Counts `json:"counts"`
				Applicable bool        `json:"applicable"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if !result.Applicable {
				t.Fatal("supported implementation profile marked non-applicable")
			}
			c := result.Counts
			if c.Workloads != 2 || c.Sandboxes != 3 || c.Containers != 5 || c.InitContainers != 2 || c.Services != 1 || c.Routes != 1 || c.Volumes != 2 || c.Configs != 1 || c.Secrets != 1 || c.Actions[plan.ActionCreateSandbox] != 3 {
				t.Fatalf("wrong JSON counts: %+v", c)
			}
		}
		assertNoCLIState(t, layout)
	}
}

func TestHelmScenarioCCompatibilityConsentAndRejection(t *testing.T) {
	requireCLIHelm(t)
	layout := isolateCLIState(t)
	data, err := os.ReadFile(filepath.Join(scenarioChart(t), "templates", "application.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, template, diagnostic string
		consent                    bool
		want                       int
	}{
		{"rolling-blocked", strings.ReplaceAll(string(data), "type: Recreate", "type: RollingUpdate"), "kubernetes.rollout_strategy", false, 1},
		{"rolling-consented", strings.ReplaceAll(string(data), "type: Recreate", "type: RollingUpdate"), "kubernetes.rollout_strategy", true, 0},
		{"metadata-only", string(data) + "\n---\napiVersion: v1\nkind: ServiceAccount\nmetadata: {name: metadata-only}\n", "kubernetes.validate_only_kind", false, 0},
		{"unsupported", string(data) + "\n---\napiVersion: v1\nkind: Pod\nmetadata: {name: forbidden}\nspec:\n  hostNetwork: true\n  containers: [{name: app, image: busybox:1.37}]\n", "kubernetes.host_network", false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chart := cliChart(t, tc.template)
			writeHelmFile(t, filepath.Join(chart, "values.yaml"), "image: busybox:1.37\nmessage: initial-config\ntoken: synthetic-t22-token\n")
			client := &fakeClient{}
			app, stdout, stderr := newTestApp(t, client, &fakeTerminal{})
			app.Ensure = func(context.Context, string, string, time.Duration) error {
				t.Fatal("rejected input started daemon")
				return nil
			}
			args := []string{"plan", chart}
			if tc.name == "rolling-blocked" || tc.name == "unsupported" {
				args[0] = "up"
			}
			if tc.consent {
				args = append(args, "--allow-degraded=kubernetes.rollout_strategy")
			}
			if code := app.Run(context.Background(), args); code != tc.want {
				t.Fatal("wrong exit", code, stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.diagnostic) {
				t.Fatal("missing support diagnostic", stderr.String())
			}
			if client.applied.Identity.Name != "" {
				t.Fatal("rejected input provisioned application")
			}
			if strings.Contains(stdout.String()+stderr.String(), "synthetic-t22-token") {
				t.Fatal("secret leaked")
			}
			assertNoCLIState(t, layout)
			stdout.Reset()
			stderr.Reset()
			jsonArgs := []string{"plan", chart, "--output=json"}
			if tc.consent {
				jsonArgs = append(jsonArgs, "--allow-degraded=kubernetes.rollout_strategy")
			}
			if code := app.Run(context.Background(), jsonArgs); code != tc.want {
				t.Fatal("JSON plan lost compatibility status", code)
			}
			var report struct {
				Applicable  bool        `json:"applicable"`
				Diagnostics source.List `json:"diagnostics"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if report.Applicable != (tc.want == 0) {
				t.Fatal("incorrect plan applicability")
			}
			found := false
			for _, diagnostic := range report.Diagnostics {
				if diagnostic.Code == tc.diagnostic && diagnostic.Compatibility != "" && diagnostic.Consequence != "" {
					found = true
				}
			}
			if !found {
				t.Fatal("JSON discarded structured support/consequence diagnostic")
			}
			if strings.Contains(stdout.String()+stderr.String(), "synthetic-t22-token") {
				t.Fatal("JSON leaked secret")
			}
			assertNoCLIState(t, layout)
		})
	}
}
