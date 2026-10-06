// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"github.com/albertize/grillo/internal/model"
	"testing"
)

func TestSummaryTopologyAndDiffActions(t *testing.T) {
	app := model.Application{Workloads: []model.Workload{
		{Replicas: 2, Template: model.SandboxTemplate{Containers: make([]model.Container, 2), InitContainers: make([]model.Container, 1)}},
		{Replicas: 1, Template: model.SandboxTemplate{Containers: make([]model.Container, 1)}},
		{Replicas: 0, Template: model.SandboxTemplate{Containers: make([]model.Container, 4)}},
	}}
	p := Plan{Actions: []Action{{Kind: ActionCreateSandbox}, {Kind: ActionCreateSandbox}, {Kind: ActionUpdateEndpoints}}}
	counts := Summarize(app, p)
	if counts.Workloads != 3 || counts.Sandboxes != 3 || counts.Containers != 5 || counts.InitContainers != 2 || counts.Actions[ActionCreateSandbox] != 2 {
		t.Fatalf("counts = %+v", counts)
	}
	if got := Summarize(app, Plan{}); len(got.Actions) != 0 || got.Sandboxes != 3 {
		t.Fatal("no-op lost desired topology")
	}
}
