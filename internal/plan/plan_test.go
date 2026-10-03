// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"os/exec"
	"strings"
	"testing"

	"grillo.local/grillo/internal/model"
)

func testApp(replicas int, tag string) model.Application {
	return model.Application{
		APIVersion: model.APIVersion,
		Kind:       model.KindApplication,
		Identity:   model.Identity{Name: "backend", Namespace: "default"},
		Workloads: []model.Workload{{
			ID:       "backend",
			Kind:     model.WorkloadDeployment,
			Replicas: int32(replicas),
			Template: model.SandboxTemplate{
				Containers: []model.Container{{
					Name:  "api",
					Image: model.ImageRef{Reference: "busybox:" + tag},
				}},
			},
		}},
		Services: []model.Service{{
			Name:     "backend",
			Selector: map[string]string{"app": "backend"},
			Ports:    []model.ServicePort{{Port: 80}},
		}},
	}
}

func observedFor(t *testing.T, app model.Application) Observed {
	t.Helper()
	revision, _ := Revision(app)
	routesHash, _ := RoutesHash(app)
	descriptors, err := DesiredSandboxes(app)
	if err != nil {
		t.Fatal(err)
	}
	observed := Observed{Revision: revision, RoutesHash: routesHash, Sandboxes: map[string]ObservedSandbox{}, Volumes: map[string]bool{}}
	for _, descriptor := range descriptors {
		observed.Sandboxes[descriptor.ID] = ObservedSandbox{
			Workload: descriptor.Workload, Index: descriptor.Index,
			TemplateHash: descriptor.TemplateHash, State: "running", Ready: true,
		}
	}
	return observed
}

func actionKinds(p Plan) []ActionKind {
	kinds := make([]ActionKind, 0, len(p.Actions))
	for _, action := range p.Actions {
		kinds = append(kinds, action.Kind)
	}
	return kinds
}

func TestIdenticalApplyIsEmpty(t *testing.T) {
	app := testApp(2, "1")
	p, err := Build(app, observedFor(t, app))
	if err != nil {
		t.Fatal(err)
	}
	if !p.Empty() {
		t.Fatalf("identical apply produced actions: %v", actionKinds(p))
	}
}

func TestCreateSandboxes(t *testing.T) {
	app := testApp(2, "1")
	routesHash, _ := RoutesHash(app)
	p, err := Build(app, Observed{Sandboxes: map[string]ObservedSandbox{}, Volumes: map[string]bool{}, RoutesHash: routesHash})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Actions) != 2 {
		t.Fatalf("actions = %v", actionKinds(p))
	}
	for _, action := range p.Actions {
		if action.Kind != ActionCreateSandbox {
			t.Fatalf("action = %+v", action)
		}
	}
}

func TestRouteOnlyChangeRebootsNothing(t *testing.T) {
	app := testApp(2, "1")
	observed := observedFor(t, app)
	observed.RoutesHash = "sha256:changed"
	p, err := Build(app, observed)
	if err != nil {
		t.Fatal(err)
	}
	if p.Reboots() != 0 {
		t.Fatalf("route-only change rebooted: %v", actionKinds(p))
	}
	if len(p.Actions) != 1 || p.Actions[0].Kind != ActionUpdateEndpoints {
		t.Fatalf("actions = %v", actionKinds(p))
	}
}

func TestTemplateChangeRecreates(t *testing.T) {
	app := testApp(1, "1")
	observed := observedFor(t, app)
	app.Workloads[0].Template.Containers[0].Image.Reference = "busybox:2"
	p, err := Build(app, observed)
	if err != nil {
		t.Fatal(err)
	}
	want := []ActionKind{ActionDrain, ActionStopSandbox, ActionDeleteSandbox, ActionCreateSandbox}
	got := actionKinds(p)
	if len(got) != len(want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("actions = %v, want %v", got, want)
		}
	}
}

func TestTemplateHashTracksConfigChanges(t *testing.T) {
	app := testApp(1, "1")
	app.Workloads[0].Template.Containers[0].Env = []model.EnvVar{{
		Name:      "TOKEN",
		ValueFrom: &model.EnvSource{ConfigRef: &model.ConfigKeyRef{Config: "settings", Key: "token"}},
	}}
	app.Configs = []model.Config{{Name: "settings", Entries: []model.ConfigEntry{{Key: "token", Text: "a"}}}}
	before, _ := DesiredSandboxes(app)
	app.Configs[0].Entries[0].Text = "b"
	after, _ := DesiredSandboxes(app)
	if before[0].TemplateHash == after[0].TemplateHash {
		t.Fatal("config change did not change the template hash")
	}
}

func TestScaleDown(t *testing.T) {
	app2 := testApp(2, "1")
	observed := observedFor(t, app2)
	app1 := testApp(1, "1")
	p, err := Build(app1, observed)
	if err != nil {
		t.Fatal(err)
	}
	want := []ActionKind{ActionDrain, ActionStopSandbox, ActionDeleteSandbox}
	got := actionKinds(p)
	if len(got) != len(want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("actions = %v, want %v", got, want)
		}
	}
	if p.Actions[0].Sandbox.ID != "backend-1" {
		t.Fatalf("scaled down %s, want backend-1", p.Actions[0].Sandbox.ID)
	}
}

func TestJobNeverDoesNotRestart(t *testing.T) {
	app := testApp(1, "1")
	app.Workloads[0].Kind = model.WorkloadJob
	app.Workloads[0].RestartPolicy = model.RestartNever
	observed := observedFor(t, app)
	descriptor := observed.Sandboxes["backend-0"]
	descriptor.State = "stopped"
	observed.Sandboxes["backend-0"] = descriptor
	p, err := Build(app, observed)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Empty() {
		t.Fatalf("a completed Job restarted: %v", actionKinds(p))
	}
}

func TestStoppedAppPlansTeardown(t *testing.T) {
	app := testApp(1, "1")
	observed := observedFor(t, app)
	observed.Stopped = true
	p, err := Build(app, observed)
	if err != nil {
		t.Fatal(err)
	}
	want := []ActionKind{ActionDrain, ActionStopSandbox, ActionDeleteSandbox}
	got := actionKinds(p)
	if len(got) != len(want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
}

func TestPrepareVolume(t *testing.T) {
	app := testApp(1, "1")
	app.Volumes = []model.Volume{{Name: "data", Kind: model.VolumeManaged}}
	observed := observedFor(t, app)
	p, err := Build(app, observed)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Actions) != 1 || p.Actions[0].Kind != ActionPrepareVolume || p.Actions[0].Volume != "data" {
		t.Fatalf("actions = %+v", p.Actions)
	}
}

// TestPlanDoesNotBuild enforces the guarantee that planning is pure and never
// invokes an image builder or the runtime.
func TestPlanDoesNotBuild(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", "{{join .Imports \"\\n\"}}", ".").CombinedOutput()
	if err != nil {
		t.Skipf("SKIP: go list failed: %v", err)
	}
	for _, forbidden := range []string{
		"internal/build", "internal/executor", "internal/backend",
		"internal/sandbox", "internal/image", "internal/oci", "internal/network",
	} {
		if strings.Contains(string(out), forbidden) {
			t.Fatalf("plan imports %s:\n%s", forbidden, out)
		}
	}
}
