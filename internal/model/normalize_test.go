// SPDX-License-Identifier: Apache-2.0

package model

import (
	"bytes"
	"testing"
)

func TestNormalizeIsOrderInsensitive(t *testing.T) {
	base := loadFixture(t)
	shuffled := cloneForTest(t, base)

	appStrings(&shuffled)

	Normalize(&shuffled)
	want, err := CanonicalJSON(base)
	if err != nil {
		t.Fatal(err)
	}
	got, err := CanonicalJSON(shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("normalized order-dependent output:\n want %s\n  got %s", want, got)
	}
}

// appStrings reverses the semantically unordered collections in place.
func appStrings(app *Application) {
	for i, j := 0, len(app.Workloads)-1; i < j; i, j = i+1, j-1 {
		app.Workloads[i], app.Workloads[j] = app.Workloads[j], app.Workloads[i]
	}
	for w := range app.Workloads {
		reverseStrings(app.Workloads[w].DependsOn)
		reverseStrings(app.Workloads[w].Template.Volumes)
		reverseStrings(app.Workloads[w].Template.Networks)
		for c := range app.Workloads[w].Template.Containers {
			container := &app.Workloads[w].Template.Containers[c]
			reverse(container.Env)
			reverse(container.Mounts)
			reverse(container.Ports)
		}
	}
	for i, j := 0, len(app.Services)-1; i < j; i, j = i+1, j-1 {
		app.Services[i], app.Services[j] = app.Services[j], app.Services[i]
	}
	for s := range app.Services {
		reverse(app.Services[s].Ports)
	}
	reverse(app.Volumes)
	for c := range app.Configs {
		reverse(app.Configs[c].Entries)
	}
	reverse(app.Configs)
	reverse(app.Secrets)
	reverse(app.Networks)
}

func reverseStrings(s []string) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// reverse is generic over any slice.
func reverse[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func TestNormalizeAppliesDefaults(t *testing.T) {
	app := Application{
		APIVersion: APIVersion,
		Kind:       KindApplication,
		Identity:   Identity{Name: "x"},
		Workloads: []Workload{{
			ID: "w",
			Template: SandboxTemplate{Containers: []Container{{
				Name:  "c",
				Image: ImageRef{Reference: "img"},
				Ports: []ContainerPort{{ContainerPort: 80}},
			}}},
		}},
		Services: []Service{{Name: "s", Ports: []ServicePort{{Port: 80}}}},
		Routes:   []Route{{Service: "s"}},
	}
	Normalize(&app)
	if app.Identity.Namespace != DefaultNamespace {
		t.Errorf("namespace = %q, want %q", app.Identity.Namespace, DefaultNamespace)
	}
	if app.Workloads[0].RestartPolicy != RestartAlways {
		t.Errorf("restartPolicy = %q, want %q", app.Workloads[0].RestartPolicy, RestartAlways)
	}
	if app.Workloads[0].Template.Containers[0].Ports[0].Protocol != "TCP" {
		t.Errorf("container protocol = %q, want TCP", app.Workloads[0].Template.Containers[0].Ports[0].Protocol)
	}
	if app.Services[0].Ports[0].Protocol != "TCP" {
		t.Errorf("service protocol = %q, want TCP", app.Services[0].Ports[0].Protocol)
	}
	if app.Routes[0].PathType != "Prefix" {
		t.Errorf("pathType = %q, want Prefix", app.Routes[0].PathType)
	}
}
