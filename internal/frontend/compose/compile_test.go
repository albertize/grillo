// SPDX-License-Identifier: Apache-2.0

package compose

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/source"
)

func yamlScalarNode(t *testing.T, document string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(document), &doc); err != nil {
		t.Fatal(err)
	}
	value, ok := mapGet(doc.Content[0], "command")
	if !ok {
		t.Fatalf("no command in %q", document)
	}
	return value
}

var update = flag.Bool("update", false, "update golden files")

func compileFixture(t *testing.T, name string, env map[string]string) Result {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Compile(context.Background(), data, Options{Path: path, Environment: env})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func hasError(diagnostics source.List) bool { return diagnostics.HasErrors() }

func TestCompileFullFixture(t *testing.T) {
	result := compileFixture(t, "full.yaml", nil)
	if hasError(result.Diagnostics) {
		t.Fatalf("unexpected errors: %+v", result.Diagnostics.Errors())
	}
	app := result.Application
	if app.Identity.Name != "demo" || app.Source == nil || app.Source.Kind != source.KindCompose {
		t.Fatalf("identity/source = %+v %+v", app.Identity, app.Source)
	}
	if len(app.Workloads) != 2 {
		t.Fatalf("workloads = %d", len(app.Workloads))
	}
	web := workloadByName(app, "web")
	if web == nil {
		t.Fatal("web workload missing")
	}
	if web.Replicas != 2 || web.RestartPolicy != model.RestartOnFailure {
		t.Fatalf("web = %+v", web)
	}
	if len(web.DependsOn) != 1 || web.DependsOn[0] != "db" {
		t.Fatalf("depends_on = %v", web.DependsOn)
	}
	container := web.Template.Containers[0]
	if container.Image.Reference != "busybox:1.37" || container.User != "1000:1000" || container.WorkingDir != "/srv" {
		t.Fatalf("container = %+v", container)
	}
	if container.Resources.Limits.CPU != 500 || container.Resources.Limits.Memory != 128<<20 {
		t.Fatalf("limits = %+v", container.Resources.Limits)
	}
	if container.Probes.Liveness == nil || len(container.Probes.Liveness.Exec.Command) != 4 || container.Probes.Liveness.Exec.Command[0] != "wget" {
		t.Fatalf("healthcheck = %+v", container.Probes.Liveness)
	}
	if len(container.Ports) == 0 || container.Ports[0].ContainerPort != 80 || container.Ports[0].HostPort != 8080 {
		t.Fatalf("ports = %+v", container.Ports)
	}
	if !hasVolume(app.Volumes, "data") || !hasVolume(app.Volumes, "dbdata") {
		t.Fatalf("volumes = %+v", app.Volumes)
	}
	// The project-relative bind source is resolved against the Compose file
	// directory (kept relative so goldens are reproducible across machines).
	bind := volumeByKind(app.Volumes, model.VolumeBind)
	if bind == nil || bind.Source != filepath.Join("testdata", "static") {
		t.Fatalf("bind volume = %+v", bind)
	}
	if len(app.Services) != 2 || app.Services[0].Name != "web" || app.Services[1].Name != "db" {
		t.Fatalf("services = %+v", app.Services)
	}
	if len(app.Services[0].Ports) != 1 || app.Services[0].Ports[0].Port != 8080 {
		t.Fatalf("web service ports = %+v", app.Services[0].Ports)
	}
	if len(app.Networks) != 1 || app.Networks[0].Name != "front" {
		t.Fatalf("networks = %+v", app.Networks)
	}
	// The empty environment value is preserved as an empty string, distinct from
	// an absent variable.
	if value, ok := envValue(container, "EMPTY"); !ok || value != "" {
		t.Fatalf("EMPTY env = %q ok=%v", value, ok)
	}
	if _, ok := envValue(container, "MISSING"); ok {
		t.Fatal("MISSING should be absent")
	}
	// Source map records the workload location.
	if span, ok := result.SourceMap.Get("workload/web"); !ok || span.Start.Line == 0 {
		t.Fatalf("source map = %+v", span)
	}
}

func TestFullFixtureGolden(t *testing.T) {
	result := compileFixture(t, "full.yaml", nil)
	canonical, err := model.CanonicalJSON(result.Application)
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "full.golden.json")
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

func TestUnknownFieldIsRejected(t *testing.T) {
	result := compileFixture(t, "unknown-field.yaml", nil)
	if !hasError(result.Diagnostics) {
		t.Fatal("unknown field did not produce an error")
	}
	found := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "compose.unknown_field" && strings.Contains(diagnostic.Message, "frobnicate") {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics = %+v", result.Diagnostics)
	}
}

func TestUnsupportedFieldsAreRejected(t *testing.T) {
	result := compileFixture(t, "unsupported.yaml", nil)
	if !hasError(result.Diagnostics) {
		t.Fatal("unsupported fields did not produce errors")
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Compatibility != source.Unsupported {
			t.Fatalf("diagnostic compatibility = %+v", diagnostic)
		}
	}
}

func TestInterpolation(t *testing.T) {
	if value, _, err := Interpolate("${TAG:-1.37}", nil); err != nil || value != "1.37" {
		t.Fatalf("default = %q err=%v", value, err)
	}
	if value, _, err := Interpolate("${TAG-1.37}", map[string]string{"TAG": ""}); err != nil || value != "" {
		t.Fatalf("unset-only default = %q err=%v", value, err)
	}
	if _, _, err := Interpolate("${TAG:?tag is required}", nil); err == nil {
		t.Fatal("required variable did not error")
	}
	if value, _, err := Interpolate("${A:+alt}", map[string]string{"A": "1"}); err != nil || value != "alt" {
		t.Fatalf("alternative = %q err=%v", value, err)
	}
	if value, _, err := Interpolate("$$HOME", nil); err != nil || value != "$HOME" {
		t.Fatalf("escape = %q err=%v", value, err)
	}
	if value, warnings, err := Interpolate("$UNSET", nil); err != nil || value != "" || len(warnings) != 1 {
		t.Fatalf("unset = %q warnings=%v err=%v", value, warnings, err)
	}
}

func TestInterpolationCompilesAndFailsOnRequired(t *testing.T) {
	result := compileFixture(t, "interpolation.yaml", map[string]string{"TAG": "1.38", "REQUIRED": "yes"})
	if hasError(result.Diagnostics) {
		t.Fatalf("unexpected errors: %+v", result.Diagnostics.Errors())
	}
	if got := result.Application.Workloads[0].Template.Containers[0].Image.Reference; got != "busybox:1.38" {
		t.Fatalf("image = %q", got)
	}
	missing := compileFixture(t, "interpolation.yaml", nil)
	if !hasError(missing.Diagnostics) {
		t.Fatal("missing required variable did not error")
	}
}

func TestEmptyDocumentIsEmptyApplication(t *testing.T) {
	result := compileFixture(t, "empty.yaml", nil)
	if hasError(result.Diagnostics) {
		t.Fatalf("empty document produced errors: %+v", result.Diagnostics)
	}
	if len(result.Application.Workloads) != 0 || result.Application.Identity.Name == "" {
		t.Fatalf("application = %+v", result.Application)
	}
}

func TestAbsentVsEmptyCommand(t *testing.T) {
	absent := commandList(nil, false)
	if absent != nil {
		t.Fatalf("absent command = %v", absent)
	}
	node := yamlScalarNode(t, "command: ''")
	empty := commandList(node, false)
	if empty == nil || len(*empty) != 0 {
		t.Fatalf("empty command = %v", empty)
	}
	node = yamlScalarNode(t, `command: echo "hello world"`)
	split := commandList(node, false)
	if split == nil || len(*split) != 2 || (*split)[1] != "hello world" {
		t.Fatalf("split command = %v", split)
	}
}

func TestSecretIsNotEchoedInDiagnostics(t *testing.T) {
	document := `services:
  web:
    image: busybox:1.37
    unknown_thing: true
    environment:
      PASSWORD: s3cr3t-value
`
	result, err := Compile(context.Background(), []byte(document), Options{Path: "compose.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range result.Diagnostics {
		if strings.Contains(diagnostic.Message, "s3cr3t-value") || strings.Contains(diagnostic.Consequence, "s3cr3t-value") {
			t.Fatalf("diagnostic leaked a secret: %+v", diagnostic)
		}
	}
}

func TestDockerComposeComparison(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("SKIP: docker is not installed")
	}
	path := filepath.Join("testdata", "full.yaml")
	out, err := exec.Command("docker", "compose", "-f", path, "config").CombinedOutput()
	if err != nil {
		t.Skipf("SKIP: docker compose unavailable: %v: %s", err, out)
	}
	// Structural comparison: both parse the same services. Docker output is not a
	// runtime dependency.
	if !strings.Contains(string(out), "web:") || !strings.Contains(string(out), "db:") {
		t.Fatalf("unexpected docker compose config: %s", out)
	}
}

func TestFrontendDoesNotImportRuntime(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("SKIP: go is not on PATH")
	}
	out, err := exec.Command("go", "list", "-f", "{{join .Imports \"\\n\"}}", ".").CombinedOutput()
	if err != nil {
		t.Skipf("SKIP: go list failed: %v: %s", err, out)
	}
	for _, forbidden := range []string{
		"internal/backend", "internal/sandbox", "internal/state", "internal/api",
		"internal/executor", "internal/network", "internal/netns", "internal/reconcile",
	} {
		if strings.Contains(string(out), forbidden) {
			t.Fatalf("compose frontend imports %s:\n%s", forbidden, out)
		}
	}
	_ = json.Marshal
}

func workloadByName(app model.Application, name string) *model.Workload {
	for i := range app.Workloads {
		if app.Workloads[i].ID == name {
			return &app.Workloads[i]
		}
	}
	return nil
}

func hasVolume(volumes []model.Volume, name string) bool {
	for _, volume := range volumes {
		if volume.Name == name {
			return true
		}
	}
	return false
}

func volumeByKind(volumes []model.Volume, kind model.VolumeKind) *model.Volume {
	for i := range volumes {
		if volumes[i].Kind == kind {
			return &volumes[i]
		}
	}
	return nil
}

func envValue(container model.Container, name string) (string, bool) {
	for _, variable := range container.Env {
		if variable.Name == name {
			return variable.Value, true
		}
	}
	return "", false
}

func TestSingleSharedNetworkSupported(t *testing.T) {
	result := compileFixture(t, "network-single.yaml", nil)
	if result.Diagnostics.HasErrors() {
		t.Fatalf("shared single network rejected: %+v", result.Diagnostics.Errors())
	}
}

func TestMultipleNetworksRejected(t *testing.T) {
	result := compileFixture(t, "network-multi.yaml", nil)
	if !result.Diagnostics.HasErrors() {
		t.Fatal("multi-network topology was accepted")
	}
	found := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "compose.multi_network" && diagnostic.Compatibility == source.Unsupported {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics = %+v", result.Diagnostics)
	}
}
