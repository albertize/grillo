//go:build kvm

// SPDX-License-Identifier: Apache-2.0

// T19 F2 gate: a complete Compose application on the real runtime.
// Run with: make test-f2

package executor

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"grillo.local/grillo/internal/backend/qemu"
	"grillo.local/grillo/internal/frontend/compose"
	"grillo.local/grillo/internal/reconcile"
	"grillo.local/grillo/internal/sandbox"
	"grillo.local/grillo/internal/storage"
)

const f2Compose = `name: f2
services:
  web:
    image: busybox:1.37
    command: ["/bin/httpd", "-f", "-p", "8080", "-h", "/www"]
    expose: ["8080"]
    networks: [app]
  worker:
    image: busybox:1.37
    command: ["/bin/sh", "-c", "until wget -q -O /data/result.html http://web:8080/; do sleep 1; done; while true; do sleep 5; done"]
    volumes:
      - data:/data
    depends_on: [web]
    networks: [app]
volumes:
  data:
networks:
  app:
`

func TestKVMComposeF2Application(t *testing.T) {
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
		WorkDir: filepath.Join(work, "backend"), CIDBase: 130, BootTimeout: 30 * time.Second,
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
		GuestKey:   key, VsockPort: 1024, VsockCIDBase: 130,
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

	composeDir := t.TempDir()
	composePath := filepath.Join(composeDir, "compose.yaml")
	if err := os.WriteFile(composePath, []byte(f2Compose), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compose.Compile(ctx, data, compose.Options{Path: composePath, ProjectName: "f2"})
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Diagnostics.HasErrors() {
		t.Fatalf("compose diagnostics: %+v", compiled.Diagnostics.Errors())
	}
	app := compiled.Application

	runtime.SetDesired(app)
	if _, err := reconciler.Apply(ctx, app); err != nil {
		t.Fatalf("apply: %v", err)
	}
	defer reconciler.Down(context.Background(), "f2", true)

	infos := runtime.Sandboxes("f2")
	if len(infos) != 2 {
		t.Fatalf("sandboxes = %+v", infos)
	}
	var webInfo, workerInfo SandboxInfo
	for _, info := range infos {
		if strings.Contains(info.Key, "web") && !strings.Contains(info.Key, "worker") {
			webInfo = info
		} else {
			workerInfo = info
		}
	}
	if webInfo.IP == "" || workerInfo.IP == "" || webInfo.IP == workerInfo.IP {
		t.Fatalf("unexpected addresses: web=%q worker=%q", webInfo.IP, workerInfo.IP)
	}

	// The worker resolves web over the in-guest DNS and fetches its page.
	volumeSource := waitForVolumeSource(t, volumes, "data")
	waitForFile(t, filepath.Join(volumeSource, "result.html"), 45*time.Second)
	result, err := os.ReadFile(filepath.Join(volumeSource, "result.html"))
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := os.ReadFile(filepath.Join(rootfs, "www", "index.html"))
	if !strings.Contains(string(result), strings.TrimSpace(string(expected))) {
		t.Fatalf("worker result = %q, want web index content", result)
	}

	// DNS resolves the service FQDN to the web sandbox address.
	var dnsOut, dnsErr bytes.Buffer
	_, _ = runtime.ExecRuntime(ctx, workerInfo.Key, "worker", []string{"/bin/nslookup", "web.default.svc.cluster.local"}, &dnsOut, &dnsErr)
	if !strings.Contains(dnsOut.String(), webInfo.IP) {
		t.Fatalf("DNS output %q missing %s (stderr %q)", dnsOut.String(), webInfo.IP, dnsErr.String())
	}

	// Re-applying an unchanged application is a no-op.
	before := sandboxKeys(runtime.Sandboxes("f2"))
	if _, err := reconciler.Apply(ctx, app); err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	after := sandboxKeys(runtime.Sandboxes("f2"))
	if strings.Join(before, ",") != strings.Join(after, ",") {
		t.Fatalf("re-apply replaced sandboxes: %v -> %v", before, after)
	}

	// down keeps the volume and removes the sandboxes, then recovery restores it.
	if _, err := reconciler.Down(ctx, "f2", false); err != nil {
		t.Fatalf("down: %v", err)
	}
	if infos := runtime.Sandboxes("f2"); len(infos) != 0 {
		t.Fatalf("sandboxes survived down: %+v", infos)
	}
	if _, err := os.Stat(filepath.Join(volumeSource, "result.html")); err != nil {
		t.Fatalf("down without --volumes removed data: %v", err)
	}
	if _, err := reconciler.Apply(ctx, app); err != nil {
		t.Fatalf("recovery apply: %v", err)
	}
	if got := waitForVolumeSource(t, volumes, "data"); got != volumeSource {
		t.Fatalf("volume source changed across recovery: %q -> %q", volumeSource, got)
	}

	// down --volumes removes owned volumes.
	if _, err := reconciler.Down(ctx, "f2", true); err != nil {
		t.Fatalf("down --volumes: %v", err)
	}
	if _, err := os.Stat(volumeSource); !os.IsNotExist(err) {
		t.Fatalf("volume directory still present after down --volumes: %v", err)
	}
}

func sandboxKeys(infos []SandboxInfo) []string {
	keys := make([]string, 0, len(infos))
	for _, info := range infos {
		keys = append(keys, info.Key)
	}
	sort.Strings(keys)
	return keys
}

func waitForVolumeSource(t *testing.T, volumes *storage.Manager, name string) string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		list, err := volumes.List()
		if err == nil {
			for _, volume := range list {
				if volume.Name == name && volume.Source != "" {
					return volume.Source
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("volume %q was not created", name)
	return ""
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("file %s was not written within %s", path, timeout)
}
