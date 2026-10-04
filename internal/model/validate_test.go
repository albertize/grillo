// SPDX-License-Identifier: Apache-2.0

package model

import (
	"testing"

	"github.com/albertize/grillo/internal/source"
)

func cloneForTest(t *testing.T, app Application) Application {
	t.Helper()
	clone, err := cloneApplication(app)
	if err != nil {
		t.Fatal(err)
	}
	return clone
}

func hasCode(diags source.List, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestValidateFixtureOK(t *testing.T) {
	app := loadFixture(t)
	diags := Validate(app, FullCapabilities())
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}
}

func TestValidateUnknownVersion(t *testing.T) {
	app := cloneForTest(t, loadFixture(t))
	app.APIVersion = "grillo.dev/v9"
	if diags := Validate(app, FullCapabilities()); !hasCode(diags, CodeVersionUnknown) {
		t.Fatalf("expected %s, got %v", CodeVersionUnknown, diags)
	}
}

func TestValidateMissingVolumeReference(t *testing.T) {
	app := cloneForTest(t, loadFixture(t))
	app.Workloads[0].Template.Containers[0].Mounts = append(app.Workloads[0].Template.Containers[0].Mounts, VolumeMount{Volume: "nope", MountPath: "/data"})
	if diags := Validate(app, FullCapabilities()); !hasCode(diags, CodeReferenceMissing) {
		t.Fatalf("expected %s, got %v", CodeReferenceMissing, diags)
	}
}

func TestValidateDuplicateWorkload(t *testing.T) {
	app := cloneForTest(t, loadFixture(t))
	app.Workloads = append(app.Workloads, app.Workloads[0])
	if diags := Validate(app, FullCapabilities()); !hasCode(diags, CodeDuplicateName) {
		t.Fatalf("expected %s, got %v", CodeDuplicateName, diags)
	}
}

func TestValidateDependencyCycle(t *testing.T) {
	app := cloneForTest(t, loadFixture(t))
	for i := range app.Workloads {
		switch app.Workloads[i].ID {
		case "api":
			app.Workloads[i].DependsOn = []string{"db"}
		case "db":
			app.Workloads[i].DependsOn = []string{"api"}
		}
	}
	if diags := Validate(app, FullCapabilities()); !hasCode(diags, CodeReferenceCycle) {
		t.Fatalf("expected %s, got %v", CodeReferenceCycle, diags)
	}
}

func TestValidateNegativeReplicas(t *testing.T) {
	app := cloneForTest(t, loadFixture(t))
	app.Workloads[0].Replicas = -1
	if diags := Validate(app, FullCapabilities()); !hasCode(diags, CodeQuantityNegative) {
		t.Fatalf("expected %s, got %v", CodeQuantityNegative, diags)
	}
}

func TestValidateGuestBudgetExceeded(t *testing.T) {
	app := cloneForTest(t, loadFixture(t))
	app.Workloads[0].Template.GuestResources.Requests.CPU = 100
	if diags := Validate(app, FullCapabilities()); !hasCode(diags, CodeRequestExceedsGuest) {
		t.Fatalf("expected %s, got %v", CodeRequestExceedsGuest, diags)
	}
}

func TestValidateCapabilitiesUnsupported(t *testing.T) {
	app := loadFixture(t)
	diags := Validate(app, NewCapabilities())
	if !hasCode(diags, CodeSupportUnsupported) {
		t.Fatalf("expected %s with empty capabilities, got %v", CodeSupportUnsupported, diags)
	}
	for _, d := range diags {
		if d.Code == CodeSupportUnsupported && d.Severity != source.SeverityError {
			t.Errorf("unsupported capability must be an error, got %s", d.Severity)
		}
	}
}

func TestValidateEnvConflict(t *testing.T) {
	app := cloneForTest(t, loadFixture(t))
	app.Workloads[0].Template.Containers[0].Env = []EnvVar{{
		Name:      "CONFLICT",
		Value:     "literal",
		ValueFrom: &EnvSource{ConfigRef: &ConfigKeyRef{Config: "app-config", Key: "LOG_LEVEL"}},
	}}
	if diags := Validate(app, FullCapabilities()); !hasCode(diags, CodeEnvConflict) {
		t.Fatalf("expected %s, got %v", CodeEnvConflict, diags)
	}
}

func TestValidateDuplicateContainerPort(t *testing.T) {
	app := cloneForTest(t, loadFixture(t))
	c := &app.Workloads[0].Template.Containers[0]
	c.Ports = append(c.Ports, ContainerPort{ContainerPort: 8080, Protocol: "TCP"})
	if diags := Validate(app, FullCapabilities()); !hasCode(diags, CodePortDuplicate) {
		t.Fatalf("expected %s, got %v", CodePortDuplicate, diags)
	}
}
