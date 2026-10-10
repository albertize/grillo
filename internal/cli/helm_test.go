//go:build linux

// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/api"
	"github.com/albertize/grillo/internal/frontend/helm"
	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/oci"
	"github.com/albertize/grillo/internal/secrets"
	"github.com/albertize/grillo/internal/state"
)

const helmPod = `apiVersion: v1
kind: Pod
metadata: {name: "{{ .Release.Name }}"}
spec:
  containers:
    - name: app
      image: busybox:1.37
      args: ["{{ .Values.message }}"]
      env:
        - name: TOKEN
          valueFrom:
            secretKeyRef: {name: private, key: token}
---
apiVersion: v1
kind: Secret
metadata: {name: private}
stringData:
  token: "{{ .Values.token }}"
`

func requireCLIHelm(t *testing.T) {
	t.Helper()
	binary, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("pinned Helm not installed")
	}
	version, err := exec.Command(binary, "version", "--template", "{{.Version}}").Output()
	if err != nil || string(version) != helm.Version {
		t.Skip("installed Helm does not match pin")
	}
	t.Setenv("GRILLO_HELM_BINARY", binary)
}
func writeHelmFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func cliChart(t *testing.T, template string) string {
	t.Helper()
	chart := filepath.Join(t.TempDir(), "demo")
	writeHelmFile(t, filepath.Join(chart, "Chart.yaml"), "apiVersion: v2\nname: demo\nversion: 0.1.0\n")
	writeHelmFile(t, filepath.Join(chart, "values.yaml"), "message: default\ntoken: synthetic-private-token\n")
	writeHelmFile(t, filepath.Join(chart, "templates", "app.yaml"), template)
	return chart
}
func isolateCLIState(t *testing.T) state.Layout {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"XDG_RUNTIME_DIR", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(name, filepath.Join(root, name))
	}
	layout, err := state.NewLayout(state.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	return layout
}
func assertNoCLIState(t *testing.T, layout state.Layout) {
	t.Helper()
	for _, path := range []string{layout.Runtime, layout.Data, layout.State, layout.Cache} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("preview/rejection mutated %s: %v", path, err)
		}
	}
}
func TestHelmCLIPlanOfflineStableAndOrdered(t *testing.T) {
	requireCLIHelm(t)
	layout := isolateCLIState(t)
	chart := cliChart(t, helmPod)
	first, second := filepath.Join(t.TempDir(), "first.yaml"), filepath.Join(t.TempDir(), "second.yaml")
	writeHelmFile(t, first, "message: first\n")
	writeHelmFile(t, second, "message: final\n")
	app, stdout, stderr := newTestApp(t, &fakeClient{}, &fakeTerminal{})
	app.Ensure = func(context.Context, string, string, time.Duration) error { t.Fatal("plan started daemon"); return nil }
	args := []string{"plan", "-f", first, chart, "--release", "stable", "-f", second, "--output", "json"}
	if code := app.Run(context.Background(), args); code != 0 {
		t.Fatal(code, stderr.String())
	}
	output := stdout.String()
	if strings.Contains(output, "synthetic-private-token") || strings.Contains(stderr.String(), "synthetic-private-token") {
		t.Fatal("secret leaked")
	}
	assertNoCLIState(t, layout)
	stdout.Reset()
	stderr.Reset()
	if code := app.Run(context.Background(), args); code != 0 || output != stdout.String() {
		t.Fatal("unstable plan", code, stderr.String())
	}
	loaded, code := app.loadApplication(context.Background(), inputOptions{Release: "stable", Namespace: "default"}, []string{first, second}, []string{chart}, nil)
	if code != 0 || loaded.Identity.Name != "stable" || (*loaded.Workloads[0].Template.Containers[0].Args)[0] != "final" {
		t.Fatal("identity/values mismatch", code, stderr.String())
	}
	if loaded.Secrets[0].Version != "unresolved-offline" {
		t.Fatal("offline secret version disclosed")
	}
	assertNoCLIState(t, layout)
}
func TestHelmCLIUpPersistsOpaqueSecrets(t *testing.T) {
	requireCLIHelm(t)
	layout := isolateCLIState(t)
	chart := cliChart(t, helmPod)
	client := &fakeClient{operation: api.Operation{Kind: "apply", State: "succeeded"}}
	app, _, stderr := newTestApp(t, client, &fakeTerminal{})
	for _, path := range []string{chart, filepath.Join(chart, "Chart.yaml")} {
		if code := app.Run(context.Background(), []string{"up", path}); code != 0 {
			t.Fatal(code, stderr.String())
		}
		if client.applied.Identity.Name != "demo" || len(client.applied.Secrets) != 1 {
			t.Fatal("wrong application")
		}
	}
	ref := client.applied.Secrets[0]
	if ref.ID == "" || ref.Version == "" || ref.Version == "unresolved-offline" {
		t.Fatal("not a private opaque reference")
	}
	store, err := secrets.Open(filepath.Join(layout.Data, "secrets"), state.Ops{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := store.Get(ref)
	if err != nil || string(data["token"]) != "synthetic-private-token" {
		t.Fatal("secret not persisted", err)
	}
	if len(store.Refs()) != 1 {
		t.Fatal("identical reapply changed secret version")
	}
	public, _ := json.Marshal(client.applied)
	if strings.Contains(string(public), "synthetic-private-token") {
		t.Fatal("Apply received secret value")
	}
}
func TestHelmCLIRejectedInputDoesNotProvision(t *testing.T) {
	layout := isolateCLIState(t)
	chart := cliChart(t, `{{ lookup "v1" "Pod" "default" "demo" }}`)
	for _, args := range [][]string{
		{"up", chart},
		{"up", chart, "--namespace", "other"},
		{"up", chart, "--release", "--invalid"},
		{"up", "oci://example.test/chart"},
		{"plan", nativeManifest(t), "-f", "unused-values.yaml"},
		{"plan", nativeManifest(t), "--release", "demo"},
	} {
		client := &fakeClient{}
		app, _, stderr := newTestApp(t, client, &fakeTerminal{})
		app.Ensure = func(context.Context, string, string, time.Duration) error {
			t.Fatal("rejected input started daemon")
			return nil
		}
		if code := app.Run(context.Background(), args); code == 0 {
			t.Fatal("input accepted", args, stderr.String())
		}
		if client.applied.APIVersion != "" {
			t.Fatal("invalid input applied")
		}
		assertNoCLIState(t, layout)
	}
}
func TestHelmCLIDegradedConsentAndRedactedFailure(t *testing.T) {
	requireCLIHelm(t)
	layout := isolateCLIState(t)
	chart := cliChart(t, strings.Replace(k8sDeployment, "  strategy: {type: Recreate}\n", "", 1))
	app, _, stderr := newTestApp(t, &fakeClient{}, &fakeTerminal{})
	if code := app.Run(context.Background(), []string{"plan", chart}); code == 0 {
		t.Fatal("RollingUpdate accepted without consent")
	}
	if !strings.Contains(stderr.String(), "kubernetes.rollout_strategy") {
		t.Fatal(stderr.String())
	}
	if code := app.Run(context.Background(), []string{"plan", chart, "--allow-degraded=kubernetes.rollout_strategy"}); code != 0 {
		t.Fatal(code, stderr.String())
	}
	chart = cliChart(t, `{{ fail .Values.token }}`)
	stderr.Reset()
	if code := app.Run(context.Background(), []string{"plan", chart}); code == 0 {
		t.Fatal("failed renderer accepted")
	}
	if !strings.Contains(stderr.String(), "HELM_RENDER_FAILED") || strings.Contains(stderr.String(), "synthetic-private-token") {
		t.Fatal("unredacted tool error", stderr.String())
	}
	assertNoCLIState(t, layout)
}
func TestKubernetesPlanAndRejectedUpDoNotStoreSecrets(t *testing.T) {
	layout := isolateCLIState(t)
	data := strings.ReplaceAll(helmPod, "{{ .Release.Name }}", "demo")
	data = strings.ReplaceAll(data, "{{ .Values.message }}", "default")
	data = strings.ReplaceAll(data, "{{ .Values.token }}", "synthetic-private-token")
	path := filepath.Join(t.TempDir(), "pod.yaml")
	writeHelmFile(t, path, data)
	app, _, stderr := newTestApp(t, &fakeClient{}, &fakeTerminal{})
	if code := app.Run(context.Background(), []string{"plan", path, "--output", "json"}); code != 0 {
		t.Fatal(code, stderr.String())
	}
	assertNoCLIState(t, layout)
	writeHelmFile(t, path, strings.Replace(data, "      image: busybox:1.37", "      image: busybox:1.37\n      securityContext: {privileged: true}", 1))
	if code := app.Run(context.Background(), []string{"up", path}); code == 0 {
		t.Fatal("privileged input accepted")
	}
	assertNoCLIState(t, layout)
}
func TestHelmCLIOCIExplicitFetchAndCachedOfflinePlan(t *testing.T) {
	requireCLIHelm(t)
	layout := isolateCLIState(t)
	chart := cliChart(t, helmPod)
	packedDir := t.TempDir()
	// Exercise official Helm packaging, not only synthetic archive fixtures.
	if output, err := exec.Command("helm", "package", chart, "--destination", packedDir).CombinedOutput(); err != nil {
		t.Fatalf("package fixture: %v %s", err, output)
	}
	archive, err := os.ReadFile(filepath.Join(packedDir, "demo-0.1.0.tgz"))
	if err != nil {
		t.Fatal(err)
	}
	config := []byte(`{"name":"demo","version":"0.1.0"}`)
	sha := func(b []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(b)) }
	manifest, err := json.Marshal(oci.Manifest{SchemaVersion: 2, MediaType: oci.MediaTypeOCIManifest, Config: oci.Descriptor{MediaType: "application/vnd.cncf.helm.config.v1+json", Digest: sha(config), Size: int64(len(config))}, Layers: []oci.Descriptor{{MediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip", Digest: sha(archive), Size: int64(len(archive))}}})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/v2/charts/demo/manifests/0.1.0":
			w.Header().Set("Content-Type", oci.MediaTypeOCIManifest)
			w.Write(manifest)
		case "/v2/charts/demo/blobs/" + sha(config):
			w.Write(config)
		case "/v2/charts/demo/blobs/" + sha(archive):
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	registry := oci.NewRegistryClient()
	registry.Transport = server.Client().Transport
	client := &fakeClient{operation: api.Operation{Kind: "apply", State: "succeeded"}}
	app, stdout, stderr := newTestApp(t, client, &fakeTerminal{})
	app.HelmRegistry = registry
	app.Ensure = func(context.Context, string, string, time.Duration) error {
		t.Fatal("OCI plan started daemon")
		return nil
	}
	reference := "oci://" + strings.TrimPrefix(server.URL, "https://") + "/charts/demo"
	args := []string{"plan", reference, "--version", "0.1.0", "--chart-digest", sha(manifest), "--output", "json"}
	if code := app.Run(context.Background(), args); code == 0 || !strings.Contains(stderr.String(), "HELM_OCI_FETCH_REQUIRED") {
		t.Fatal(code, stderr.String())
	}
	if calls.Load() != 0 {
		t.Fatal("OCI CLI fetched without consent")
	}
	assertNoCLIState(t, layout)
	stdout.Reset()
	stderr.Reset()
	if code := app.Run(context.Background(), append(args, "--fetch-chart")); code != 0 {
		t.Fatal(code, stderr.String())
	}
	first := stdout.String()
	if calls.Load() != 3 {
		t.Fatal("unexpected OCI requests", calls.Load())
	}
	if strings.Contains(first, "synthetic-private-token") {
		t.Fatal("secret leaked from OCI chart plan")
	}
	for _, p := range []string{layout.Data, layout.State, layout.Runtime} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatal("OCI plan wrote runtime state", p, err)
		}
	}
	server.Close()
	stdout.Reset()
	stderr.Reset()
	if code := app.Run(context.Background(), args); code != 0 || stdout.String() != first || calls.Load() != 3 {
		t.Fatal("offline OCI plan unstable", code, stderr.String())
	}
	app.Ensure = func(context.Context, string, string, time.Duration) error { return nil }
	if code := app.Run(context.Background(), []string{"up", reference, "--version", "0.1.0", "--chart-digest", sha(manifest)}); code != 0 {
		t.Fatal("cached OCI apply failed", code, stderr.String())
	}
	if client.applied.Identity.Name != "demo" || len(client.applied.Secrets) != 1 || client.applied.Secrets[0].ID == "" || calls.Load() != 3 {
		t.Fatal("OCI apply did not use private cached input")
	}
}

func TestHelmCLIRejectsRemoteFlagsOnLocalInput(t *testing.T) {
	layout := isolateCLIState(t)
	for _, flags := range [][]string{{"--version", "0.1.0"}, {"--fetch-chart"}, {"--chart-digest", "invalid"}} {
		app, _, stderr := newTestApp(t, &fakeClient{}, &fakeTerminal{})
		if code := app.Run(context.Background(), append([]string{"plan", cliChart(t, helmPod)}, flags...)); code != 2 {
			t.Fatal("ignored remote flags", code, stderr.String())
		}
		assertNoCLIState(t, layout)
	}
}

func TestHelmCLIMissingToolAndUnsafeFiles(t *testing.T) {
	layout := isolateCLIState(t)
	chart := cliChart(t, helmPod)
	app, _, stderr := newTestApp(t, &fakeClient{}, &fakeTerminal{})
	t.Setenv("PATH", t.TempDir())
	if code := app.Run(context.Background(), []string{"plan", chart}); code == 0 || !strings.Contains(stderr.String(), "HELM_UNAVAILABLE") {
		t.Fatal(code, stderr.String())
	}
	pipe := filepath.Join(t.TempDir(), "pipe.yaml")
	if err := syscall.Mkfifo(pipe, 0600); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if code := app.Run(context.Background(), []string{"plan", chart, "-f", pipe}); code == 0 || !strings.Contains(stderr.String(), "HELM_VALUES_INVALID") {
		t.Fatal(code, stderr.String())
	}
	if err := syscall.Mkfifo(filepath.Join(chart, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if code := app.Run(context.Background(), []string{"plan", chart}); code == 0 || !strings.Contains(stderr.String(), "HELM_CHART_INVALID") {
		t.Fatal(code, stderr.String())
	}
	assertNoCLIState(t, layout)
}

func TestManifestReadLimit(t *testing.T) {
	layout := isolateCLIState(t)
	path := filepath.Join(t.TempDir(), "large.yaml")
	writeHelmFile(t, path, strings.Repeat("x", (8<<20)+1))
	app, _, stderr := newTestApp(t, &fakeClient{}, &fakeTerminal{})
	if code := app.Run(context.Background(), []string{"plan", path}); code == 0 || !strings.Contains(stderr.String(), "size limit") {
		t.Fatal(code, stderr.String())
	}
	assertNoCLIState(t, layout)
}

func TestHelmCLIPlanReleaseMatchesRenderedPod(t *testing.T) {
	requireCLIHelm(t)
	isolateCLIState(t)
	chart := cliChart(t, helmPod)
	app, _, stderr := newTestApp(t, &fakeClient{}, &fakeTerminal{})
	loaded, code := app.loadApplication(context.Background(), inputOptions{Format: "helm", Release: "chosen", Namespace: model.DefaultNamespace}, nil, []string{chart}, nil)
	if code != 0 || loaded.Identity.Name != "chosen" || loaded.Workloads[0].ID != "chosen" {
		t.Fatal("release identity mismatch", code, stderr.String(), loaded.Identity)
	}
}
