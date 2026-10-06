//go:build kvm

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/backend/qemu"
	"github.com/albertize/grillo/internal/frontend/helm"
	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/reconcile"
	"github.com/albertize/grillo/internal/sandbox"
	"github.com/albertize/grillo/internal/storage"
)

// TestKVMHelmApplication is the first T22 hardware check, not the full scenario C
// gate. It uses the official renderer and a preexisting OCI rootfs fixture; it
// does not test registry pulls, Service proxies, Ingress or PVC persistence.
func TestKVMHelmApplication(t *testing.T) {
	skipUnlessBridged(t)
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("SKIP: provision official Helm " + helm.Version)
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
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	chart := filepath.Join(root, "examples/helm/demo")
	compile := func(values ...string) model.Application {
		t.Helper()
		result, err := helm.Compile(ctx, helm.Options{Chart: chart, Release: "helm-gate", Values: values})
		if err != nil {
			t.Fatal(err)
		}
		if result.Diagnostics.HasErrors() {
			t.Fatalf("Helm diagnostics: %+v", result.Diagnostics.Errors())
		}
		if len(result.Application.Workloads) != 1 || result.Application.Workloads[0].Replicas != 1 {
			t.Fatal("unexpected chart topology")
		}
		return result.Application
	}
	app := compile()
	work := t.TempDir()
	netnsBin := filepath.Join(work, "grillo-netns")
	build := exec.CommandContext(ctx, "go", "build", "-o", netnsBin, "./cmd/grillo-netns")
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
		WorkDir: filepath.Join(work, "backend"), CIDBase: 170, BootTimeout: 30 * time.Second,
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
		GuestKey:   key, VsockPort: 1024, VsockCIDBase: 170,
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
	// Register cleanup before the first effect, including partial apply failure.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if _, err := reconciler.Down(cleanup, app.Identity.Name, true); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()
	apply := func(desired model.Application) {
		t.Helper()
		runtime.SetDesired(desired)
		if _, err := reconciler.Apply(ctx, desired); err != nil {
			t.Fatalf("apply: %v", err)
		}
	}
	run := func(args ...string) string {
		t.Helper()
		infos := runtime.Sandboxes(app.Identity.Name)
		if len(infos) != 1 {
			t.Fatalf("expected one Pod microVM, got %d", len(infos))
		}
		var stdout, stderr bytes.Buffer
		code, err := runtime.ExecRuntime(ctx, infos[0].Key, "app", args, &stdout, &stderr)
		if err != nil || code != 0 {
			t.Fatalf("guest exec: code=%d err=%v", code, err)
		}
		return strings.TrimSpace(stdout.String())
	}
	bootID := func() string {
		t.Helper()
		id := run("cat", "/proc/sys/kernel/random/boot_id")
		if id == "" {
			t.Fatal("empty guest boot ID")
		}
		return id
	}
	apply(app)
	if got := run("printenv", "MESSAGE"); got != "hello-from-helm" {
		t.Fatal("rendered default value did not reach the container")
	}
	before := bootID()
	apply(compile())
	if bootID() != before {
		t.Fatal("identical Helm apply rebooted the microVM")
	}
	values := filepath.Join(work, "override.yaml")
	if err := os.WriteFile(values, []byte("message: updated-from-helm\n"), 0600); err != nil {
		t.Fatal(err)
	}
	apply(compile(values))
	if got := run("printenv", "MESSAGE"); got != "updated-from-helm" {
		t.Fatal("updated value did not reach the container")
	}
	if bootID() == before {
		t.Fatal("changed Helm template did not recreate the microVM")
	}
	if _, err := reconciler.Down(ctx, app.Identity.Name, true); err != nil {
		t.Fatalf("down: %v", err)
	}
	if len(runtime.Sandboxes(app.Identity.Name)) != 0 {
		t.Fatal("sandbox survived down")
	}
	entries, err := os.ReadDir(filepath.Join(work, "backend"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("backend resources survived down: entries=%d err=%v", len(entries), err)
	}
}
