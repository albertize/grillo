//go:build linux

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/sandbox"
	"github.com/albertize/grillo/internal/source"
)

func TestDeclaredViewIsAllowlisted(t *testing.T) {
	private := "synthetic-hidden-value"
	app := model.Application{Identity: model.Identity{Name: "app"}, Source: &source.Source{Kind: source.KindHelm, Path: "chart"}, Workloads: []model.Workload{{ID: "api", Kind: model.WorkloadDeployment, Replicas: 2, Labels: map[string]string{"app": "api"}, Template: model.SandboxTemplate{Containers: []model.Container{{Name: "web", Image: model.ImageRef{Reference: "web:local"}, Env: []model.EnvVar{{Name: "TOKEN", Value: private}}, Command: &[]string{private}, Probes: model.Probes{Readiness: &model.Probe{Exec: &model.ExecAction{Command: []string{private}}}}, Mounts: []model.VolumeMount{{Volume: "data", MountPath: "/data", ReadOnly: true}}}}}}}, Configs: []model.Config{{Name: "settings", Entries: []model.ConfigEntry{{Text: private}, {Binary: []byte(private)}}}}, Secrets: []model.SecretRef{{Name: "token", Version: private}}, Volumes: []model.Volume{{Name: "data", Kind: model.VolumeBind, Source: private}}, Services: []model.Service{{Name: "api", Selector: map[string]string{"app": "api"}}, {Name: "other", Selector: map[string]string{"app": "other"}}}, Routes: []model.Route{{Hostname: "app.local", Path: "/", Service: "api", Endpoint: "http://127.0.0.1:1234"}}}
	view := declaredView(app)
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), private) {
		t.Fatal("private data leaked in resource projection")
	}
	if len(view.Workloads) != 1 || len(view.Workloads[0].Containers[0].Mounts) != 1 || !view.Workloads[0].Containers[0].ReadinessProbe || view.Configs[0].EntryCount != 2 || len(view.Services[0].Workloads) != 1 || len(view.Services[1].Workloads) != 0 || view.SourceKind != "helm" {
		t.Fatalf("incomplete metadata: %+v", view)
	}
}

type viewBackend struct {
	sandbox.Backend
	fail bool
}

func (b viewBackend) Inspect(ctx context.Context, _ sandbox.Handle) (sandbox.Observation, error) {
	if err := ctx.Err(); err != nil {
		return sandbox.Observation{}, err
	}
	if b.fail {
		return sandbox.Observation{}, errors.New("synthetic-private-error")
	}
	return sandbox.Observation{State: sandbox.StateRunning, PID: os.Getpid()}, nil
}
func TestViewObservationsAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		e := &Executor{cfg: Config{Backend: viewBackend{fail: fail}, MemoryMiB: 512}, desired: map[string]model.Application{"app": {Identity: model.Identity{Name: "app"}}}, runtimes: map[string]*sandboxRuntime{"app-api-0": {id: "app-api-0", app: "app", workload: "api", ready: true, ip: "10.0.0.2"}}}
		view, err := e.View(context.Background(), "app")
		if err != nil {
			t.Fatal(err)
		}
		vm := view.Sandboxes[0]
		if vm.ID != "app-api-0" || vm.GuestBudgetBytes != 512<<20 || vm.VCPU != 1 {
			t.Fatalf("bad snapshot %+v", vm)
		}
		if fail {
			if vm.VMM != nil || vm.State != "unavailable" {
				t.Fatal("invented metric/state")
			}
		} else if vm.VMM == nil || vm.VMM.RSSBytes <= 0 {
			t.Fatal("missing real process sample")
		}
		raw, _ := json.Marshal(view)
		if strings.Contains(string(raw), "synthetic-private-error") {
			t.Fatal("raw backend error leaked")
		}
		if _, err := e.View(context.Background(), "unknown"); err == nil {
			t.Fatal("unknown app accepted")
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := e.View(ctx, "app"); err == nil {
			t.Fatal("cancellation ignored")
		}
	}
}
