// SPDX-License-Identifier: Apache-2.0

package plan

import "github.com/albertize/grillo/internal/model"

// Counts distinguishes desired resource topology from the actions a diff will
// execute. Container counts include replicas, but exclude init containers.
type Counts struct {
	Workloads      int                `json:"workloads"`
	Sandboxes      int64              `json:"sandboxes"`
	Containers     int64              `json:"containers"`
	InitContainers int64              `json:"initContainers"`
	Services       int                `json:"services"`
	Routes         int                `json:"routes"`
	Volumes        int                `json:"volumes"`
	Configs        int                `json:"configs"`
	Secrets        int                `json:"secrets"`
	Actions        map[ActionKind]int `json:"actions"`
}

// Summarize returns counts only, never configuration or secret values. Callers
// validate the application before producing a plan.
func Summarize(app model.Application, p Plan) Counts {
	counts := Counts{
		Workloads: len(app.Workloads), Services: len(app.Services), Routes: len(app.Routes),
		Volumes: len(app.Volumes), Configs: len(app.Configs), Secrets: len(app.Secrets),
		Actions: map[ActionKind]int{},
	}
	for _, workload := range app.Workloads {
		if workload.Replicas <= 0 {
			continue
		}
		replicas := int64(workload.Replicas)
		counts.Sandboxes += replicas
		counts.Containers += replicas * int64(len(workload.Template.Containers))
		counts.InitContainers += replicas * int64(len(workload.Template.InitContainers))
	}
	for _, action := range p.Actions {
		counts.Actions[action.Kind]++
	}
	return counts
}
