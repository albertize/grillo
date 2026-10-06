//go:build linux

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/source"
	"testing"
)

func TestNativeValidationRejectsUnimplementedWorkloadsAndSemantics(t *testing.T) {
	app := model.Application{Identity: model.Identity{Name: "policy"}, Workloads: []model.Workload{{ID: "web", Kind: model.WorkloadDeployment, Replicas: 1, Template: model.SandboxTemplate{Containers: []model.Container{{Name: "web", Image: model.ImageRef{Reference: "busybox:1.37"}}}}}}}
	if diagnostics := ValidateApplication(app, true); diagnostics.HasErrors() {
		t.Fatal(diagnostics)
	}
	for _, kind := range []model.WorkloadKind{model.WorkloadJob, model.WorkloadStatefulSet, model.WorkloadCronJob} {
		candidate := app
		candidate.Workloads = append([]model.Workload(nil), app.Workloads...)
		candidate.Workloads[0].Kind = kind
		if !ValidateApplication(candidate, true).HasErrors() {
			t.Fatalf("unsupported workload accepted: %s", kind)
		}
	}
	candidate := app
	candidate.Services = []model.Service{{Name: "web", Ports: []model.ServicePort{{Port: 80, Protocol: "UDP"}}}}
	diagnostics := ValidateApplication(candidate, true)
	if !diagnostics.HasErrors() {
		t.Fatal("UDP semantics accepted")
	}
}

func TestNativeCapabilitiesAreExplicitAndConfigurationBound(t *testing.T) {
	for _, bridged := range []bool{false, true} {
		caps := RuntimeCapabilities(bridged)
		for _, feature := range []model.Feature{model.FeatureManagedVolume, model.FeaturePVCMount, model.FeatureBindMount, model.FeatureInitContainers, model.FeatureMultipleContainers, model.FeatureExecProbe, model.FeatureHTTPProbe, model.FeatureTCPProbe, model.FeatureReadOnlyRootFilesystem} {
			state, ok := caps.State(feature)
			if !ok || state != source.Supported {
				t.Fatalf("missing implemented feature %s", feature)
			}
		}
		for _, feature := range []model.Feature{model.FeatureHostPort, model.FeatureRoute} {
			_, ok := caps.State(feature)
			if ok != bridged {
				t.Fatalf("%s ignored network configuration", feature)
			}
		}
		for _, feature := range []model.Feature{model.FeaturePrivileged, model.FeatureJob, model.FeatureStatefulSet, "future.unknown"} {
			if _, ok := caps.State(feature); ok {
				t.Fatalf("unsupported feature enabled: %s", feature)
			}
		}
	}
}
