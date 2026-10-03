//go:build kvm

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"grillo.local/grillo/internal/backend/qemu"
	"grillo.local/grillo/internal/model"
	"grillo.local/grillo/internal/reconcile"
	"grillo.local/grillo/internal/storage"
)

// directoryResolver serves a fixed host rootfs directory for every image.
type directoryResolver struct{ dir string }

func (d directoryResolver) Resolve(context.Context, model.ImageRef) (Image, error) {
	return Image{HostPath: d.dir, Version: "test"}, nil
}

// TestKVMEndToEndApply applies a native manifest end to end: the executor boots a
// sandbox with a virtiofs rootfs, starts the container, reports status, runs a
// command, and tears everything down.
func TestKVMEndToEndApply(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skip("SKIP: /dev/kvm is not available")
	}
	root := repoRoot(t)
	kernel := filepath.Join(root, "experiments/artifacts/qemu/bzImage")
	initramfs := filepath.Join(root, "experiments/artifacts/t07/initramfs-agent.cpio.gz")
	keyPath := filepath.Join(root, "experiments/artifacts/t07/key")
	rootfs := filepath.Join(root, "experiments/artifacts/t02/rootfs")
	for _, path := range []string{kernel, initramfs, keyPath, rootfs} {
		if _, err := os.Stat(path); err != nil {
			t.Skipf("SKIP: missing %s (run 'make t07-guest' and 'make oci-guest')", path)
		}
	}
	keyData, _ := os.ReadFile(keyPath)
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(keyData)))
	if err != nil {
		t.Fatal(err)
	}
	backend, err := qemu.Open(qemu.Config{
		Kernel:      kernel,
		Initramfs:   initramfs,
		WorkDir:     filepath.Join(t.TempDir(), "backend"),
		CIDBase:     85,
		BootTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	volumes, err := storage.Open(filepath.Join(t.TempDir(), "storage"))
	if err != nil {
		t.Fatal(err)
	}
	exec, err := New(Config{
		Backend:      backend,
		Volumes:      volumes,
		Kernel:       kernel,
		Initramfs:    initramfs,
		KernelArgs:   "console=ttyS0 reboot=k panic=1 rdinit=/init",
		GuestKey:     key,
		VsockPort:    1024,
		VsockCIDBase: 85,
		Images:       directoryResolver{dir: rootfs},
	})
	if err != nil {
		t.Fatal(err)
	}
	reconciler := &reconcile.Reconciler{
		Store:    reconcile.NewMemoryStore(),
		Executor: &reconcile.NativeExecutor{Sandboxes: SandboxController{exec}, Volumes: VolumeController{exec}},
	}

	app := model.Application{
		APIVersion: model.APIVersion,
		Kind:       model.KindApplication,
		Identity:   model.Identity{Name: "e2e", Namespace: "default"},
		Workloads: []model.Workload{{
			ID:       "web",
			Kind:     model.WorkloadDeployment,
			Replicas: 1,
			Template: model.SandboxTemplate{
				Containers: []model.Container{{
					Name:  "app",
					Image: model.ImageRef{Reference: "busybox:1.37"},
					Command: &[]string{
						"/bin/sh", "-c", "sleep 300",
					},
				}},
			},
		}},
	}
	exec.SetDesired(app)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	result, err := reconciler.Apply(ctx, app)
	if err != nil {
		t.Fatalf("apply: %v (failed action %+v)", err, result.Failed)
	}

	// The container should be running.
	deadline := time.Now().Add(20 * time.Second)
	var states []ContainerState
	for time.Now().Before(deadline) {
		states, _ = exec.Status(ctx, "e2e")
		for _, state := range states {
			if state.Container == "app" && state.State == "running" {
				goto running
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
running:
	found := false
	for _, state := range states {
		if state.Container == "app" && state.State == "running" {
			found = true
		}
	}
	if !found {
		t.Fatalf("container did not reach running: %+v", states)
	}

	// Exec a command in the container.
	var stdout bytes.Buffer
	code, err := exec.Exec(ctx, "e2e", "app", []string{"/bin/echo", "end-to-end"}, &stdout, nil)
	if err != nil || code != 0 {
		t.Fatalf("exec code=%d err=%v", code, err)
	}
	if !strings.Contains(stdout.String(), "end-to-end") {
		t.Fatalf("exec output = %q", stdout.String())
	}

	// Down tears the sandbox down.
	if _, err := reconciler.Down(ctx, "e2e", false); err != nil {
		t.Fatalf("down: %v", err)
	}
	if states, _ := exec.Status(ctx, "e2e"); len(states) != 0 {
		t.Fatalf("containers remained after down: %+v", states)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate source file")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
