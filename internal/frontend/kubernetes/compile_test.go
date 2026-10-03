// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grillo.local/grillo/internal/model"
	"grillo.local/grillo/internal/source"
)

var update = flag.Bool("update", false, "update golden files")

func compileFixture(t *testing.T, name string, opts Options) Result {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Path == "" {
		opts.Path = path
	}
	result, err := Compile(context.Background(), data, opts)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func hasCode(list source.List, code string) bool {
	for _, diagnostic := range list {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func TestCompileDeployment(t *testing.T) {
	result := compileFixture(t, "deployment.yaml", Options{})
	if result.Diagnostics.HasErrors() {
		t.Fatalf("errors: %+v", result.Diagnostics.Errors())
	}
	app := result.Application
	if len(app.Workloads) != 1 {
		t.Fatalf("workloads = %d", len(app.Workloads))
	}
	workload := app.Workloads[0]
	if workload.ID != "web" || workload.Replicas != 2 || workload.RestartPolicy != model.RestartAlways {
		t.Fatalf("workload = %+v", workload)
	}
	if len(workload.Template.InitContainers) != 1 || len(workload.Template.Containers) != 1 {
		t.Fatalf("containers = %+v", workload.Template)
	}
	container := workload.Template.Containers[0]
	if container.Image.Reference != "busybox:1.37" || len(container.Ports) != 1 || container.Ports[0].Name != "http" {
		t.Fatalf("container = %+v", container)
	}
	if container.Resources.Limits.CPU != 500 || container.Resources.Limits.Memory != 128<<20 {
		t.Fatalf("limits = %+v", container.Resources.Limits)
	}
	if container.Probes.Readiness == nil || container.Probes.Readiness.HTTP == nil || container.Probes.Readiness.HTTP.Port.Name != "http" {
		t.Fatalf("readiness = %+v", container.Probes.Readiness)
	}
	if !workload.Template.SecurityProfile.ReadOnlyRootFilesystem {
		t.Fatalf("security = %+v", workload.Template.SecurityProfile)
	}
	if container.User != "1000" {
		t.Fatalf("user = %q", container.User)
	}
	if len(app.Configs) != 1 || app.Configs[0].Name != "app-config" {
		t.Fatalf("configs = %+v", app.Configs)
	}
	if len(app.Secrets) != 1 || app.Secrets[0].Name != "app-secret" || app.Secrets[0].Version == "" {
		t.Fatalf("secrets = %+v", app.Secrets)
	}
	if len(result.Secrets) != 1 || string(result.Secrets[0].Data["PASSWORD"]) != "s3cr3t-value" {
		t.Fatalf("secret data = %+v", result.Secrets)
	}
	if len(app.Volumes) != 2 {
		t.Fatalf("volumes = %+v", app.Volumes)
	}
	if len(app.Services) != 1 || app.Services[0].Name != "web" || len(app.Services[0].Ports) != 1 {
		t.Fatalf("services = %+v", app.Services)
	}
	if app.Services[0].Ports[0].TargetPort == nil || app.Services[0].Ports[0].TargetPort.Name != "http" {
		t.Fatalf("targetPort = %+v", app.Services[0].Ports[0])
	}
	if len(app.Routes) != 1 || app.Routes[0].Hostname != "web.local" || app.Routes[0].PathType != "Prefix" {
		t.Fatalf("routes = %+v", app.Routes)
	}
}

func TestSecretValuesNeverEnterPublicIR(t *testing.T) {
	result := compileFixture(t, "deployment.yaml", Options{})
	public, err := model.CanonicalJSON(result.Application)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(public), "s3cr3t-value") {
		t.Fatalf("secret value leaked into the public IR:\n%s", public)
	}
	// The redacted public view must also not contain it.
	publicView, err := model.Public(result.Application)
	if err != nil {
		t.Fatal(err)
	}
	redacted, err := json.Marshal(publicView)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(redacted), "s3cr3t-value") {
		t.Fatalf("secret value leaked into the redacted view:\n%s", redacted)
	}
}

func TestDeploymentGolden(t *testing.T) {
	result := compileFixture(t, "deployment.yaml", Options{})
	canonical, err := model.CanonicalJSON(result.Application)
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "deployment.golden.json")
	if *update {
		if err := os.WriteFile(golden, canonical, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	expected, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update): %v", err)
	}
	if strings.TrimSpace(string(expected)) != strings.TrimSpace(string(canonical)) {
		t.Fatalf("IR diverged from golden:\n%s", canonical)
	}
}

func TestRollingUpdateRequiresConsent(t *testing.T) {
	blocked := compileFixture(t, "rollingupdate.yaml", Options{})
	if !blocked.Diagnostics.HasErrors() {
		t.Fatal("default RollingUpdate was accepted without consent")
	}
	if !hasCode(blocked.Diagnostics, "kubernetes.rollout_strategy") {
		t.Fatalf("diagnostics = %+v", blocked.Diagnostics)
	}
	allowed := compileFixture(t, "rollingupdate.yaml", Options{AllowDegraded: []string{"kubernetes.rollout_strategy"}})
	if allowed.Diagnostics.HasErrors() {
		t.Fatalf("consented RollingUpdate rejected: %+v", allowed.Diagnostics.Errors())
	}
	if allowed.Application.Workloads[0].UpdatePolicy == nil || allowed.Application.Workloads[0].UpdatePolicy.Strategy != "Recreate" {
		t.Fatalf("update policy = %+v", allowed.Application.Workloads[0].UpdatePolicy)
	}
}

func TestRecreateIsFaithful(t *testing.T) {
	result := compileFixture(t, "deployment.yaml", Options{})
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "kubernetes.rollout_strategy" {
			t.Fatalf("explicit Recreate was flagged: %+v", diagnostic)
		}
	}
}

func TestRejections(t *testing.T) {
	cases := []struct {
		fixture string
		code    string
	}{
		{"privileged.yaml", "kubernetes.privileged"},
		{"hostnetwork.yaml", "kubernetes.host_network"},
		{"tier3.yaml", "kubernetes.unsupported_kind"},
		{"service-nodeport.yaml", "kubernetes.service_type"},
	}
	for _, tc := range cases {
		result := compileFixture(t, tc.fixture, Options{})
		if !result.Diagnostics.HasErrors() {
			t.Errorf("%s: expected an error", tc.fixture)
			continue
		}
		if !hasCode(result.Diagnostics, tc.code) {
			t.Errorf("%s: missing %s in %+v", tc.fixture, tc.code, result.Diagnostics)
		}
	}
}

func TestMultiContainerPod(t *testing.T) {
	result := compileFixture(t, "pod-multi.yaml", Options{})
	if result.Diagnostics.HasErrors() {
		t.Fatalf("errors: %+v", result.Diagnostics.Errors())
	}
	template := result.Application.Workloads[0].Template
	if len(template.InitContainers) != 1 || len(template.Containers) != 2 {
		t.Fatalf("template = %+v", template)
	}
	if template.Containers[1].Name != "sidecar" {
		t.Fatalf("sidecar = %+v", template.Containers[1])
	}
}

func TestListAndMultiDocument(t *testing.T) {
	input := `apiVersion: v1
kind: List
items:
  - apiVersion: v1
    kind: ConfigMap
    metadata: {name: a}
    data: {k: v}
  - apiVersion: v1
    kind: ConfigMap
    metadata: {name: b}
    data: {k: v}
`
	result, err := Compile(context.Background(), []byte(input), Options{Path: "list.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Diagnostics.HasErrors() {
		t.Fatalf("errors: %+v", result.Diagnostics.Errors())
	}
	if len(result.Application.Configs) != 2 {
		t.Fatalf("configs = %+v", result.Application.Configs)
	}
}

func TestNamespaceRejected(t *testing.T) {
	input := `apiVersion: v1
kind: ConfigMap
metadata: {name: a, namespace: other}
data: {k: v}
`
	result, err := Compile(context.Background(), []byte(input), Options{Path: "ns.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if !hasCode(result.Diagnostics, "kubernetes.namespace") {
		t.Fatalf("diagnostics = %+v", result.Diagnostics)
	}
}

func TestUnknownFieldRejected(t *testing.T) {
	input := `apiVersion: v1
kind: Pod
metadata: {name: p}
spec:
  containers:
    - name: app
      image: busybox:1.37
      frobnicate: true
`
	result, err := Compile(context.Background(), []byte(input), Options{Path: "unknown.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if !hasCode(result.Diagnostics, "kubernetes.unknown_field") {
		t.Fatalf("diagnostics = %+v", result.Diagnostics)
	}
}

func TestSelectorMismatchRejected(t *testing.T) {
	input := `apiVersion: apps/v1
kind: Deployment
metadata: {name: web}
spec:
  selector:
    matchLabels: {app: web}
  template:
    metadata:
      labels: {app: other}
    spec:
      containers:
        - name: web
          image: busybox:1.37
`
	result, err := Compile(context.Background(), []byte(input), Options{Path: "mismatch.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if !hasCode(result.Diagnostics, "kubernetes.selector_mismatch") {
		t.Fatalf("diagnostics = %+v", result.Diagnostics)
	}
}
