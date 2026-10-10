// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"reflect"
	"testing"

	"github.com/albertize/grillo/internal/model"
)

func TestStartupDependencyOrdering(t *testing.T) {
	app := model.Application{Workloads: []model.Workload{{ID: "aconsumer", Replicas: 1, DependsOn: []string{"zdb"}}, {ID: "zdb", Replicas: 2}}}
	descriptors, err := DesiredSandboxes(app)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, d := range descriptors {
		ids = append(ids, d.ID)
	}
	if !reflect.DeepEqual(ids, []string{"zdb-0", "zdb-1", "aconsumer-0"}) {
		t.Fatalf("not ordered: %v", ids)
	}
	app.Workloads[0], app.Workloads[1] = app.Workloads[1], app.Workloads[0]
	reordered, err := DesiredSandboxes(app)
	if err != nil || !reflect.DeepEqual(reordered, descriptors) {
		t.Fatal("input order changed plan")
	}
	app.Workloads[0].DependsOn = []string{"aconsumer"}
	if _, err := DesiredSandboxes(app); err == nil {
		t.Fatal("cycle accepted")
	}
	app.Workloads[0].DependsOn = []string{"missing"}
	if _, err := DesiredSandboxes(app); err == nil {
		t.Fatal("missing dependency accepted")
	}
}
