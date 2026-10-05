// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/frontend/kubernetes"
	"github.com/albertize/grillo/internal/source"
)

const pod = `apiVersion: v1
kind: Pod
metadata:
  name: demo
spec:
  containers:
    - name: app
      image: busybox:1.37
      args: ["{{ .Values.message }}"]
`

func fixture(t *testing.T, template string) string {
	t.Helper()
	root := t.TempDir()
	put(t, filepath.Join(root, "Chart.yaml"), "apiVersion: v2\nname: demo\nversion: 0.1.0\n")
	put(t, filepath.Join(root, "values.yaml"), "message: initial\n")
	put(t, filepath.Join(root, "templates", "pod.yaml"), template)
	return root
}
func put(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func realHelm(t *testing.T) string {
	t.Helper()
	binary, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("pinned Helm not installed")
	}
	b, err := exec.Command(binary, "version", "--template", "{{.Version}}").Output()
	if err != nil || string(b) != Version {
		t.Skip("installed Helm does not match pin")
	}
	return binary
}
func TestRealHelmEquivalentStableOrderedValues(t *testing.T) {
	binary := realHelm(t)
	chart := fixture(t, pod)
	first, second := filepath.Join(t.TempDir(), "first.yaml"), filepath.Join(t.TempDir(), "second.yaml")
	put(t, first, "message: first\n")
	put(t, second, "message: final\n")
	opts := Options{Binary: binary, Chart: chart, Release: "demo", Values: []string{first, second}}
	result, err := Compile(context.Background(), opts)
	if err != nil || result.Diagnostics.HasErrors() {
		t.Fatalf("compile: %v %v", err, result.Diagnostics)
	}
	expected, err := kubernetes.Compile(context.Background(), []byte(strings.ReplaceAll(pod, "{{ .Values.message }}", "final")), kubernetes.Options{Path: filepath.Join(chart, "rendered.yaml")})
	if err != nil || expected.Diagnostics.HasErrors() {
		t.Fatal(err, expected.Diagnostics)
	}
	expected.Application.Identity.Name = "demo"
	expected.Application.Source = &source.Source{Kind: source.KindHelm, Path: chart}
	if !reflect.DeepEqual(result.Application, expected.Application) {
		t.Fatalf("chart and manifest IR differ: %#v / %#v", result.Application, expected.Application)
	}
	again, err := Compile(context.Background(), opts)
	if err != nil || !reflect.DeepEqual(result, again) {
		t.Fatalf("unstable compilation: %v", err)
	}
	opts.Values = []string{second, first}
	reversed, err := Compile(context.Background(), opts)
	if err != nil || (*reversed.Application.Workloads[0].Template.Containers[0].Args)[0] != "first" {
		t.Fatal("values order not preserved", err)
	}
}
func TestRealHelmErrorsRedactedAndHooksRejected(t *testing.T) {
	binary := realHelm(t)
	for _, tc := range []struct{ name, template, code string }{
		{"secret-error", `{{ fail "synthetic-sensitive-value" }}`, "HELM_RENDER_FAILED"},
		{"hooks", strings.Replace(pod, "  name: demo", "  name: demo\n  annotations:\n    helm.sh/hook: pre-install", 1), "HELM_HOOK_UNSUPPORTED"},
		{"invalid-yaml", "apiVersion: [synthetic-sensitive-value\n", "HELM_RENDER_FAILED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Compile(context.Background(), Options{Binary: binary, Chart: fixture(t, tc.template), Release: "demo"})
			if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != tc.code {
				t.Fatal(err, result.Diagnostics)
			}
			if strings.Contains(result.Diagnostics[0].Error(), "synthetic-sensitive-value") {
				t.Fatal("tool output leaked")
			}
		})
	}
}
func TestPreflight(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*testing.T, string)
		code   string
	}{
		{"lookup", func(t *testing.T, p string) {
			put(t, filepath.Join(p, "templates", "pod.yaml"), `{{ lookup "v1" "Pod" "default" "x" }}`)
		}, "HELM_TEMPLATE_UNSUPPORTED"},
		{"random", func(t *testing.T, p string) {
			put(t, filepath.Join(p, "templates", "pod.yaml"), `{{ randAlphaNum 8 }}`)
		}, "HELM_TEMPLATE_UNSUPPORTED"},
		{"time", func(t *testing.T, p string) {
			put(t, filepath.Join(p, "templates", "pod.yaml"), `{{ ago (toDate "2006-01-02" "2020-01-01") }}`)
		}, "HELM_TEMPLATE_UNSUPPORTED"},
		{"randInt", func(t *testing.T, p string) { put(t, filepath.Join(p, "templates", "pod.yaml"), `{{ randInt 0 10 }}`) }, "HELM_TEMPLATE_UNSUPPORTED"},
		{"bcrypt", func(t *testing.T, p string) {
			put(t, filepath.Join(p, "templates", "pod.yaml"), `{{ htpasswd "user" "value" }}`)
		}, "HELM_TEMPLATE_UNSUPPORTED"},
		{"dns", func(t *testing.T, p string) {
			put(t, filepath.Join(p, "templates", "pod.yaml"), `{{ getHostByName "example.test" }}`)
		}, "HELM_TEMPLATE_UNSUPPORTED"},
		{"tpl", func(t *testing.T, p string) {
			put(t, filepath.Join(p, "templates", "pod.yaml"), `{{ tpl .Values.payload . }}`)
		}, "HELM_TEMPLATE_UNSUPPORTED"},
		{"dependencies", func(t *testing.T, p string) {
			put(t, filepath.Join(p, "Chart.yaml"), "apiVersion: v2\nname: demo\ndependencies: []\n")
		}, "HELM_DEPENDENCIES_UNSUPPORTED"},
		{"crds", func(t *testing.T, p string) { put(t, filepath.Join(p, "crds", "x.yaml"), "x") }, "HELM_FEATURE_UNSUPPORTED"},
		{"schema", func(t *testing.T, p string) {
			put(t, filepath.Join(p, "values.schema.json"), `{"$ref":"https://example.test/schema"}`)
		}, "HELM_FEATURE_UNSUPPORTED"},
		{"symlink", func(t *testing.T, p string) {
			if err := os.Symlink("values.yaml", filepath.Join(p, "linked.yaml")); err != nil {
				t.Fatal(err)
			}
		}, "HELM_CHART_INVALID"},
		{"large", func(t *testing.T, p string) { put(t, filepath.Join(p, "values.yaml"), strings.Repeat("x", maxFile+1)) }, "HELM_CHART_INVALID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chart := fixture(t, pod)
			tc.change(t, chart)
			result, err := Compile(context.Background(), Options{Binary: "/not/executed", Chart: chart, Release: "demo"})
			if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != tc.code {
				t.Fatal(err, result.Diagnostics)
			}
		})
	}
	result, err := Compile(context.Background(), Options{Chart: "oci://example.test/chart", Release: "demo"})
	if err != nil || result.Diagnostics[0].Code != "HELM_OCI_REFERENCE" {
		t.Fatal(err, result.Diagnostics)
	}
	result, err = Compile(context.Background(), Options{Chart: fixture(t, pod), Release: "--bad"})
	if err != nil || result.Diagnostics[0].Code != "HELM_RELEASE_INVALID" {
		t.Fatal(err, result.Diagnostics)
	}
}
func TestRealHelmOfflineCapabilitiesAndRenderedCRD(t *testing.T) {
	binary := realHelm(t)
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "must-not-be-read"))
	chart := fixture(t, strings.ReplaceAll(pod, `"{{ .Values.message }}"`, `"{{ .Capabilities.KubeVersion.Version }}", "{{ .Release.Namespace }}", "{{ .Capabilities.APIVersions.Has "apps/v1" }}"`))
	result, err := Compile(context.Background(), Options{Binary: binary, Chart: chart, Release: "demo"})
	if err != nil || result.Diagnostics.HasErrors() {
		t.Fatal(err, result.Diagnostics)
	}
	args := *result.Application.Workloads[0].Template.Containers[0].Args
	if !reflect.DeepEqual(args, []string{"v" + KubernetesVersion, "default", "true"}) {
		t.Fatal("capabilities not fixed offline", args)
	}
	chart = fixture(t, "apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata: {name: unsupported.example.test}\n")
	result, err = Compile(context.Background(), Options{Binary: binary, Chart: chart, Release: "demo"})
	if err != nil || !result.Diagnostics.HasErrors() {
		t.Fatal("rendered CRD accepted", err, result.Diagnostics)
	}
}

func TestRealHelmSecretsStayPrivate(t *testing.T) {
	binary := realHelm(t)
	chart := fixture(t, `apiVersion: v1
kind: Secret
metadata: {name: private}
stringData:
  token: "{{ .Values.message }}"
`)
	values := filepath.Join(t.TempDir(), "private.yaml")
	put(t, values, "message: synthetic-sensitive-value\n")
	result, err := Compile(context.Background(), Options{Binary: binary, Chart: chart, Release: "demo", Values: []string{values}})
	if err != nil || result.Diagnostics.HasErrors() || len(result.Secrets) != 1 {
		t.Fatal(err, result.Diagnostics)
	}
	if string(result.Secrets[0].Data["token"]) != "synthetic-sensitive-value" {
		t.Fatal("private secret not preserved")
	}
	if result.Application.Secrets[0].Version != "unresolved-offline" {
		t.Fatal("public IR contains a content-derived secret fingerprint")
	}
	public, err := json.Marshal(result.Application)
	if err != nil || bytes.Contains(public, []byte("synthetic-sensitive-value")) {
		t.Fatal("public IR leaked secret", err)
	}
}

func TestValuesSymlinkAndCanceledContext(t *testing.T) {
	values := filepath.Join(t.TempDir(), "values.yaml")
	put(t, values, "message: value\n")
	link := filepath.Join(t.TempDir(), "linked.yaml")
	if err := os.Symlink(values, link); err != nil {
		t.Fatal(err)
	}
	result, err := Compile(context.Background(), Options{Chart: fixture(t, pod), Release: "demo", Values: []string{link}, Binary: "/not/executed"})
	if err != nil || result.Diagnostics[0].Code != "HELM_VALUES_INVALID" {
		t.Fatal(err, result.Diagnostics)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Compile(ctx, Options{}); err != context.Canceled {
		t.Fatal("canceled context ignored", err)
	}
}

func TestBoundedBuffer(t *testing.T) {
	b := &boundedBuffer{limit: 4}
	if _, err := b.Write([]byte("abcd")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("sensitive")); err == nil {
		t.Fatal("limit ignored")
	}
	if !bytes.Equal(b.Bytes(), []byte("abcd")) {
		t.Fatal("overflow retained")
	}
}
func TestControlledEnvironmentAndCancellation(t *testing.T) {
	// Trusted fixture code, never chart input. No host shell is used by Compile.
	binary := filepath.Join(t.TempDir(), "renderer")
	put(t, binary, "#!/bin/sh\nif [ \"$1\" = version ]; then printf '"+Version+"'; exit 0; fi\nif [ -n \"$GRILLO_TEST_SECRET\" ]; then exit 1; fi\nif [ \"$HELM_PLUGINS\" != \"$HOME/plugins\" ]; then exit 1; fi\nprintf 'apiVersion: v1\\nkind: Pod\\nmetadata: {name: demo}\\nspec:\\n  containers: [{name: app, image: busybox:1.37}]\\n'\n")
	if err := os.Chmod(binary, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRILLO_TEST_SECRET", "sensitive")
	result, err := Compile(context.Background(), Options{Binary: binary, Chart: fixture(t, pod), Release: "demo"})
	if err != nil || result.Diagnostics.HasErrors() {
		t.Fatal(err, result.Diagnostics)
	}
	put(t, binary, "#!/bin/sh\nexec /bin/sleep 10\n")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := run(ctx, binary, nil, nil, t.TempDir(), 10); err == nil {
		t.Fatal("cancellation ignored")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("cancellation not bounded")
	}
}
func TestToolOutputLimitAndVersion(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "renderer")
	put(t, binary, "#!/bin/sh\nprintf 'unexpected-version'\n")
	if err := os.Chmod(binary, 0700); err != nil {
		t.Fatal(err)
	}
	result, err := Compile(context.Background(), Options{Binary: binary, Chart: fixture(t, pod), Release: "demo"})
	if err != nil || result.Diagnostics[0].Code != "HELM_VERSION" {
		t.Fatal(err, result.Diagnostics)
	}
	if _, err := run(context.Background(), binary, nil, nil, t.TempDir(), 4); err == nil {
		t.Fatal("output overflow accepted")
	}
}
