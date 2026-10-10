//go:build linux && kvm

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/backend/qemu"
	"github.com/albertize/grillo/internal/frontend/compose"
	"github.com/albertize/grillo/internal/reconcile"
	"github.com/albertize/grillo/internal/storage"
)

// Deliberately names the consumer alphabetically before its dependency, proves
// absence before real guest health, then confirms unhealthy never restarted it.
func TestKVMComposeDependencyHealth(t *testing.T) {
	for _, device := range []string{"/dev/kvm", "/dev/vhost-vsock"} {
		f, err := os.OpenFile(device, os.O_RDWR, 0)
		if err != nil {
			t.Skipf("hardware prerequisite: %v", err)
		}
		f.Close()
	}
	root := repoRoot(t)
	kernel := filepath.Join(root, "experiments/artifacts/qemu/bzImage")
	initramfs := filepath.Join(root, "experiments/artifacts/t07/initramfs-agent.cpio.gz")
	keyPath := filepath.Join(root, "experiments/artifacts/t07/key")
	rootfs := filepath.Join(root, "experiments/artifacts/t02/rootfs")
	for _, path := range []string{kernel, initramfs, keyPath, rootfs} {
		if _, err := os.Stat(path); err != nil {
			t.Skipf("missing guest fixture: %s", path)
		}
	}
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(keyData)))
	if err != nil {
		t.Fatal(err)
	}
	// Unix helper socket paths must fit sockaddr_un; the long test name must
	// not become the runtime directory prefix.
	directory, err := os.MkdirTemp("", "gr-dep-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Errorf("owned fixture cleanup: %v", err)
		}
	})
	backend, err := qemu.Open(qemu.Config{Kernel: kernel, Initramfs: initramfs, WorkDir: filepath.Join(directory, "backend"), CIDBase: 18000, BootTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	volumes, err := storage.Open(filepath.Join(t.TempDir(), "storage"))
	if err != nil {
		t.Fatal(err)
	}
	executor, err := New(Config{Backend: backend, Volumes: volumes, Kernel: kernel, Initramfs: initramfs, KernelArgs: "console=ttyS0 reboot=k panic=1 rdinit=/init", GuestKey: key, VsockPort: 1024, VsockCIDBase: 18000, Images: directoryResolver{dir: rootfs}})
	if err != nil {
		t.Fatal(err)
	}
	reconciler := &reconcile.Reconciler{Store: reconcile.NewMemoryStore(), Executor: &reconcile.NativeExecutor{Sandboxes: SandboxController{executor}, Volumes: VolumeController{executor}}}
	manifest := `name: dependency-gate
services:
  aconsumer:
    image: busybox:1.37
    command: [/bin/sh, -c, "sleep 300"]
    depends_on:
      zdependency:
        condition: service_healthy
  zdependency:
    image: busybox:1.37
    command: [/bin/sh, -c, "sleep 300"]
    healthcheck:
      test: [CMD, /bin/test, -f, /tmp/healthy]
      interval: 1s
      timeout: 1s
      retries: 1
`
	compiled, err := compose.Compile(context.Background(), []byte(manifest), compose.Options{Path: "dependency-gate.yaml"})
	if err != nil || compiled.Diagnostics.HasErrors() {
		t.Fatalf("compile: %v %+v", err, compiled.Diagnostics)
	}
	app := compiled.Application
	executor.SetDesired(app)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	done := make(chan struct{})
	defer func() {
		cancel()
		<-done
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if _, err := reconciler.Down(cleanup, app.Identity.Name, false); err != nil {
			t.Errorf("cleanup: %v", err)
		}
		executor.Close()
	}()
	result := make(chan error, 1)
	go func() { defer close(done); _, err := reconciler.Apply(ctx, app); result <- err }()
	deadline := time.Now().Add(30 * time.Second)
	for {
		executor.mu.Lock()
		dependency := executor.runtimes["dependency-gate-zdependency-0"]
		consumer := executor.runtimes["dependency-gate-aconsumer-0"]
		executor.mu.Unlock()
		if consumer != nil {
			t.Fatal("consumer started before dependency health")
		}
		if dependency != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("dependency did not start")
		}
		select {
		case err := <-result:
			t.Fatalf("apply completed before health: %v", err)
		case <-time.After(50 * time.Millisecond):
		}
	}
	pid := func() int {
		t.Helper()
		states, err := executor.Status(ctx, app.Identity.Name)
		if err != nil {
			t.Fatal(err)
		}
		for _, state := range states {
			if state.Container == "zdependency" {
				return state.PID
			}
		}
		t.Fatal("missing dependency status")
		return 0
	}
	before := pid()
	time.Sleep(1500 * time.Millisecond) // more than one failed readiness period
	if after := pid(); before == 0 || after != before {
		t.Fatalf("unhealthy dependency restarted: %d -> %d", before, after)
	}
	executor.mu.Lock()
	consumer := executor.runtimes["dependency-gate-aconsumer-0"]
	executor.mu.Unlock()
	if consumer != nil {
		t.Fatal("consumer ignored failed health")
	}
	var output bytes.Buffer
	if code, err := executor.Exec(ctx, app.Identity.Name, "dependency-gate-zdependency-0/zdependency", []string{"/bin/touch", "/tmp/healthy"}, &output, nil); err != nil || code != 0 {
		t.Fatalf("health transition: %d %v", code, err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("consumer never started after dependency became healthy")
	}
	executor.mu.Lock()
	consumer = executor.runtimes["dependency-gate-aconsumer-0"]
	executor.mu.Unlock()
	if consumer == nil {
		t.Fatal("consumer missing after successful health gate")
	}
	if after := pid(); after != before {
		t.Fatal("health transition restarted dependency")
	}
}
