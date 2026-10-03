//go:build kvm

// SPDX-License-Identifier: Apache-2.0

// T20: a compiled Kubernetes multi-container Pod on the real runtime.
// Run with: make test-k8s

package executor

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grillo.local/grillo/internal/backend/qemu"
	"grillo.local/grillo/internal/frontend/kubernetes"
	"grillo.local/grillo/internal/reconcile"
	"grillo.local/grillo/internal/sandbox"
	"grillo.local/grillo/internal/storage"
)

const multiContainerDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: multi
spec:
  replicas: 1
  selector:
    matchLabels:
      app: multi
  strategy:
    type: Recreate
  template:
    metadata:
      labels:
        app: multi
    spec:
      initContainers:
        - name: init
          image: busybox:1.37
          command: ["/bin/sh", "-c", "echo init"]
      containers:
        - name: app
          image: busybox:1.37
          command: ["/bin/sh", "-c", "mkdir -p /www && echo multi-ok > /www/index.html && exec httpd -f -p 8080 -h /www"]
        - name: sidecar
          image: busybox:1.37
          command: ["/bin/sh", "-c", "until wget -q -O /shared/result.txt http://127.0.0.1:8080/; do sleep 1; done; while true; do sleep 5; done"]
          volumeMounts:
            - name: shared
              mountPath: /shared
      volumes:
        - name: shared
          emptyDir: {}
`

func TestKVMKubernetesMultiContainer(t *testing.T) {
	skipUnlessBridged(t)
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
	work := t.TempDir()
	netnsBin := filepath.Join(work, "grillo-netns")
	build := exec.Command("go", "build", "-o", netnsBin, "./cmd/grillo-netns")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build grillo-netns: %v: %s", err, out)
	}
	runtimeDir := filepath.Join(work, "run")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(keyData)))
	if err != nil {
		t.Fatal(err)
	}
	var runtime *Executor
	backend, err := qemu.Open(qemu.Config{
		Kernel: kernel, Initramfs: initramfs,
		WorkDir: filepath.Join(work, "backend"), CIDBase: 150, BootTimeout: 30 * time.Second,
		Launch: func(ctx context.Context, spec sandbox.Spec, args []string, logPath string) (sandbox.VMM, error) {
			return runtime.LaunchSandbox(ctx, spec, args, logPath)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	volumes, err := storage.Open(filepath.Join(work, "storage"))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err = New(Config{
		Backend: backend, Volumes: volumes, Kernel: kernel, Initramfs: initramfs,
		KernelArgs: "console=ttyS0 reboot=k panic=1 rdinit=/init",
		GuestKey:   key, VsockPort: 1024, VsockCIDBase: 150,
		Images:        directoryResolver{dir: rootfs},
		EnableNetwork: true, QEMU: "qemu-system-x86_64", NetnsBinary: netnsBin, Pasta: "pasta", RuntimeDir: runtimeDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	reconciler := &reconcile.Reconciler{
		Store:       reconcile.NewMemoryStore(),
		Executor:    &reconcile.NativeExecutor{Sandboxes: SandboxController{runtime}, Volumes: VolumeController{runtime}},
		MaxAttempts: 1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	compiled, err := kubernetes.Compile(ctx, []byte(multiContainerDeployment), kubernetes.Options{Path: "multi.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Diagnostics.HasErrors() {
		t.Fatalf("compile: %+v", compiled.Diagnostics.Errors())
	}
	app := compiled.Application
	if len(app.Workloads[0].Template.InitContainers) != 1 || len(app.Workloads[0].Template.Containers) != 2 {
		t.Fatalf("template = %+v", app.Workloads[0].Template)
	}

	runtime.SetDesired(app)
	if _, err := reconciler.Apply(ctx, app); err != nil {
		t.Fatalf("apply: %v", err)
	}
	defer reconciler.Down(context.Background(), app.Identity.Name, true)

	infos := runtime.Sandboxes(app.Identity.Name)
	if len(infos) != 1 {
		t.Fatalf("sandboxes = %+v", infos)
	}
	status, err := runtime.Status(ctx, app.Identity.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(status) != 3 {
		t.Fatalf("containers = %+v", status)
	}
	// The sidecar reaches the app over shared localhost and writes to emptyDir.
	source := waitForVolumeSource(t, volumes, "shared")
	waitForFile(t, filepath.Join(source, "result.txt"), 45*time.Second)
	data, err := os.ReadFile(filepath.Join(source, "result.txt"))
	if err != nil || !strings.Contains(string(data), "multi-ok") {
		t.Fatalf("sidecar result = %q err=%v", data, err)
	}
}
