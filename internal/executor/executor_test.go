//go:build linux

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"testing"

	"grillo.local/grillo/internal/model"
)

func TestParseUser(t *testing.T) {
	if user, err := parseUser("1000:2000"); err != nil || user.UID != 1000 || user.GID != 2000 {
		t.Fatalf("user = %+v err=%v", user, err)
	}
	if user, err := parseUser("1000"); err != nil || user.UID != 1000 || user.GID != 1000 {
		t.Fatalf("user = %+v err=%v", user, err)
	}
	if _, err := parseUser("abc"); err == nil {
		t.Fatal("invalid user accepted")
	}
}

func TestResourcesMapping(t *testing.T) {
	resources := resources(model.Resources{Limits: model.ResourceList{CPU: 500, Memory: 64 << 20}})
	if resources.CPUQuotaMicros != 500_000 || resources.CPUPeriodMicros != 100_000 {
		t.Fatalf("cpu = %+v", resources)
	}
	if resources.MemoryBytes != 64<<20 {
		t.Fatalf("memory = %d", resources.MemoryBytes)
	}
}

func TestContainerSpecMapsVolumesAndEnv(t *testing.T) {
	app := model.Application{
		Configs: []model.Config{{Name: "settings", Entries: []model.ConfigEntry{{Key: "greeting", Text: "hello"}}}},
	}
	container := model.Container{
		Name:    "app",
		Command: &[]string{"/bin/app"},
		Mounts: []model.VolumeMount{
			{Volume: "data", MountPath: "/data", ReadOnly: true},
		},
		Env: []model.EnvVar{
			{Name: "LITERAL", Value: "x"},
			{Name: "FROM_CONFIG", ValueFrom: &model.EnvSource{ConfigRef: &model.ConfigKeyRef{Config: "settings", Key: "greeting"}}},
		},
	}
	spec, err := (&Executor{}).containerSpec(app, container, "/run/grillo/rootfs/app", map[string]string{"data": "/run/grillo/volumes/data"})
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Mounts) != 1 || spec.Mounts[0].Source != "/run/grillo/volumes/data" || !spec.Mounts[0].ReadOnly {
		t.Fatalf("mounts = %+v", spec.Mounts)
	}
	if len(spec.Env) != 2 || spec.Env[1] != "FROM_CONFIG=hello" {
		t.Fatalf("env = %+v", spec.Env)
	}
}

func TestContainerSpecRejectsUnknownVolume(t *testing.T) {
	container := model.Container{Name: "app", Mounts: []model.VolumeMount{{Volume: "missing", MountPath: "/x"}}}
	if _, err := (&Executor{}).containerSpec(model.Application{}, container, "/root", map[string]string{}); err == nil {
		t.Fatal("unknown volume accepted")
	}
}

func TestReferencedVolumes(t *testing.T) {
	workload := model.Workload{Template: model.SandboxTemplate{
		Volumes: []string{"shared"},
		Containers: []model.Container{{
			Name:   "app",
			Mounts: []model.VolumeMount{{Volume: "data"}},
		}},
	}}
	names := referencedVolumes(workload)
	if len(names) != 2 {
		t.Fatalf("referenced volumes = %v", names)
	}
}

func TestContainerArgsMerge(t *testing.T) {
	command := []string{"/entry"}
	args := []string{"--flag"}
	got := containerArgs(model.Container{Command: &command, Args: &args})
	if len(got) != 2 || got[0] != "/entry" || got[1] != "--flag" {
		t.Fatalf("args = %v", got)
	}
}
