// SPDX-License-Identifier: Apache-2.0

package compose

import (
	"context"
	"strings"
	"testing"

	"github.com/albertize/grillo/internal/model"
)

func TestDependencyConditions(t *testing.T) {
	prefix := `name: dependencies
services:
  zdb:
    image: busybox
    healthcheck:
      test: [CMD, /bin/true]
  aweb:
    image: busybox
    depends_on:
      zdb:
`
	for _, tc := range []struct {
		body string
		fail bool
	}{
		{"        condition: service_started\n", false},
		{"        condition: service_healthy\n        required: true\n        restart: false\n", false},
		{"        condition: service_completed_successfully\n", true},
		{"        condition: invented\n", true},
		{"        condition: service_healthy\n        restart: true\n", true},
		{"        required: false\n", true},
		{"        restart: invalid\n", true},
		{"        unknown: yes\n", true},
	} {
		result, err := Compile(context.Background(), []byte(prefix+tc.body), Options{Path: "dependencies.yaml"})
		if err != nil {
			t.Fatal(err)
		}
		if result.Diagnostics.HasErrors() != tc.fail {
			t.Fatalf("%s: %+v", tc.body, result.Diagnostics)
		}
		if !tc.fail {
			web := workloadByName(result.Application, "aweb")
			if web == nil || len(web.DependsOn) != 1 {
				t.Fatal("dependency lost")
			}
			if strings.Contains(tc.body, "service_healthy") && web.DependencyConditions["zdb"] != model.DependencyHealthy {
				t.Fatal("health gate lost")
			}
			db := workloadByName(result.Application, "zdb")
			if db.Template.Containers[0].Probes.Readiness == nil || db.Template.Containers[0].Probes.Liveness != nil {
				t.Fatal("healthcheck became liveness")
			}
		}
	}
	for _, body := range []string{
		strings.Replace(prefix+"        condition: service_healthy\n", "test: [CMD, /bin/true]", "disable: true", 1),
		"services:\n  app:\n    image: busybox\n    depends_on: [absent]\n",
		"services:\n  app:\n    image: busybox\n    depends_on: [app]\n",
		"services:\n  app:\n    image: busybox\n    depends_on: invalid\n",
		"services:\n  zdb:\n    image: busybox\n    deploy:\n      replicas: 0\n  app:\n    image: busybox\n    depends_on: [zdb]\n",
	} {
		result, err := Compile(context.Background(), []byte(body), Options{Path: "dependencies.yaml"})
		if err != nil {
			t.Fatal(err)
		}
		if !result.Diagnostics.HasErrors() {
			t.Fatalf("accepted invalid dependency: %s", body)
		}
	}
}

func TestHealthcheckIsHealthNotLiveness(t *testing.T) {
	for _, tc := range []struct {
		body           string
		fail, disabled bool
	}{
		{"test: 'test -f /tmp/healthy'", false, false},
		{"test: [NONE]", false, true},
		{"disable: true", false, true},
		{"test: [CMD, /bin/true]\n      interval: 500ms", true, false},
		{"test: [CMD, /bin/true]\n      start_interval: 1s", true, false},
		{"test: [UNKNOWN, /bin/true]", true, false},
		{"interval: 1s", true, false},
	} {
		result, err := Compile(context.Background(), []byte("services:\n  app:\n    image: busybox\n    healthcheck:\n      "+tc.body+"\n"), Options{Path: "health.yaml"})
		if err != nil {
			t.Fatal(err)
		}
		if result.Diagnostics.HasErrors() != tc.fail {
			t.Fatalf("%s: %+v", tc.body, result.Diagnostics)
		}
		container := result.Application.Workloads[0].Template.Containers[0]
		if container.Probes.Liveness != nil {
			t.Fatal("Compose healthcheck became liveness")
		}
		if !tc.fail && (container.Probes.Readiness == nil) != tc.disabled {
			t.Fatal("wrong healthcheck enablement")
		}
	}
}
