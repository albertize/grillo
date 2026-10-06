//go:build kvm

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/backend/qemu"
	"github.com/albertize/grillo/internal/frontend/helm"
	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/plan"
	"github.com/albertize/grillo/internal/reconcile"
	"github.com/albertize/grillo/internal/sandbox"
	"github.com/albertize/grillo/internal/secrets"
	"github.com/albertize/grillo/internal/state"
	"github.com/albertize/grillo/internal/storage"
)

// TestKVMHelmScenarioC deliberately asserts the full T22 gate. Missing production
// datapaths are failures, not permission to substitute a test-local proxy.
func TestKVMHelmScenarioC(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	// Keep namespace/virtiofs Unix socket paths below Linux's 108-byte limit.
	work, err := os.MkdirTemp("", "grillo-t22-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(work); err != nil {
			t.Errorf("remove owned workspace: %v", err)
		}
	})
	secretStore, err := secrets.Open(filepath.Join(work, "secrets"), state.Ops{})
	if err != nil {
		t.Fatal(err)
	}
	compile := func(t *testing.T, values ...string) model.Application {
		t.Helper()
		result, err := helm.Compile(ctx, helm.Options{
			Chart: filepath.Join(root, "examples/helm/scenario-c"), Release: "scenario-c", Values: values,
		})
		if err != nil || result.Diagnostics.HasErrors() {
			t.Fatalf("compile: err=%v diagnostics=%+v", err, result.Diagnostics)
		}
		for _, secret := range result.Secrets {
			ref, err := secretStore.Put(secret.Name, secret.Data)
			if err != nil {
				t.Fatal(err)
			}
			for i := range result.Application.Secrets {
				if result.Application.Secrets[i].Name == secret.Name {
					result.Application.Secrets[i] = ref
				}
			}
		}
		public, err := json.Marshal(result.Application)
		if err != nil || bytes.Contains(public, []byte("synthetic-t22-token")) {
			t.Fatal("private secret entered public application")
		}
		return result.Application
	}
	app := compile(t)
	if len(app.Workloads) != 2 || len(app.Services) != 1 || len(app.Routes) != 1 || len(app.Configs) != 1 || len(app.Secrets) != 1 {
		t.Fatal("scenario C topology is incomplete")
	}
	netnsBin := filepath.Join(work, "grillo-netns")
	build := exec.CommandContext(ctx, "go", "build", "-o", netnsBin, "./cmd/grillo-netns")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build grillo-netns: %v: %s", err, out)
	}
	runtimeDir := filepath.Join(work, "run")
	if err := os.MkdirAll(runtimeDir, 0700); err != nil {
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
		Kernel: kernel, Initramfs: initramfs, WorkDir: filepath.Join(work, "backend"), CIDBase: 180, BootTimeout: 30 * time.Second,
		Launch: func(ctx context.Context, spec sandbox.Spec, args []string, logPath string) (sandbox.VMM, error) {
			return runtime.LaunchSandbox(ctx, spec, args, logPath)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	volumes, err := storage.Open(filepath.Join(work, "volumes"))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err = New(Config{
		Backend: backend, Volumes: volumes, Images: directoryResolver{dir: rootfs}, Secrets: secretStore,
		Kernel: kernel, Initramfs: initramfs, KernelArgs: "console=ttyS0 reboot=k panic=1 rdinit=/init",
		GuestKey: key, VsockPort: 1024, VsockCIDBase: 180,
		EnableNetwork: true, QEMU: "qemu-system-x86_64", NetnsBinary: netnsBin, Pasta: "pasta", RuntimeDir: runtimeDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	reconciler := &reconcile.Reconciler{
		Store: reconcile.NewMemoryStore(), MaxAttempts: 1,
		Executor: &reconcile.NativeExecutor{Sandboxes: SandboxController{runtime}, Volumes: VolumeController{runtime}},
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		// Also clean attempted resources: failed CreateSandbox is not yet in
		// the reconciler's observations. These IDs belong to this workspace.
		descriptors, err := plan.DesiredSandboxes(app)
		if err != nil {
			t.Errorf("cleanup descriptors: %v", err)
		}
		for _, descriptor := range descriptors {
			if err := runtime.DeleteSandbox(cleanup, app.Identity.Name, descriptor); err != nil {
				t.Errorf("cleanup attempted sandbox: %v", err)
			}
		}
		if _, err := reconciler.Down(cleanup, app.Identity.Name, true); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()
	apply := func(t *testing.T, desired model.Application) {
		t.Helper()
		runtime.SetDesired(desired)
		if _, err := reconciler.Apply(ctx, desired); err != nil {
			t.Fatalf("apply: %v", err)
		}
	}
	run := func(t *testing.T, key, container string, args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code, err := runtime.ExecRuntime(ctx, key, container, args, &stdout, &stderr)
		if err != nil || code != 0 {
			t.Fatalf("guest exec %v: code=%d err=%v", args, code, err)
		}
		return strings.TrimSpace(stdout.String())
	}
	boots := func(t *testing.T) map[string]string {
		t.Helper()
		out := map[string]string{}
		for _, info := range runtime.Sandboxes(app.Identity.Name) {
			container := "api"
			if strings.Contains(info.Key, "storage") {
				container = "storage"
			}
			out[info.Key] = run(t, info.Key, container, "cat", "/proc/sys/kernel/random/boot_id")
		}
		return out
	}
	apply(t, app)
	infos := runtime.Sandboxes(app.Identity.Name)
	if len(infos) != 3 {
		t.Fatalf("want two API Pods plus one storage Pod, got %d VMs", len(infos))
	}
	var apis []SandboxInfo
	var storageKey string
	for _, info := range infos {
		if strings.Contains(info.Key, "storage") {
			storageKey = info.Key
		} else {
			apis = append(apis, info)
		}
	}
	if len(apis) != 2 || storageKey == "" {
		t.Fatal("incorrect replica topology")
	}
	t.Run("init-sidecar-config-secret", func(t *testing.T) {
		for _, info := range apis {
			if run(t, info.Key, "sidecar", "cat", "/shared/init") != "initialized" {
				t.Fatal("init output not visible to sidecar")
			}
			if run(t, info.Key, "sidecar", "wget", "-q", "-O", "-", "http://127.0.0.1:8080/") != "initial-config" {
				t.Fatal("sidecar localhost/config mismatch")
			}
			// Validate privately without returning or printing the fixture secret.
			run(t, info.Key, "api", "sh", "-c", "test \"$TOKEN\" = synthetic-t22-token")
		}
	})
	run(t, storageKey, "storage", "sh", "-c", "echo retained-row > /data/row")
	before := boots(t)
	apply(t, compile(t))
	for key, id := range boots(t) {
		if id != before[key] {
			t.Fatalf("identical apply rebooted %s", key)
		}
	}
	values := filepath.Join(work, "config.yaml")
	if err := os.WriteFile(values, []byte("message: updated-config\n"), 0600); err != nil {
		t.Fatal(err)
	}
	app = compile(t, values)
	apply(t, app)
	t.Run("config-only-consumers-recreated", func(t *testing.T) {
		for key, id := range boots(t) {
			if (key == storageKey) != (id == before[key]) {
				t.Errorf("wrong replacement scope: %s", key)
			}
		}
		for _, info := range apis {
			if run(t, info.Key, "api", "printenv", "MESSAGE") != "updated-config" {
				t.Error("config update did not reach consumer")
			}
		}
	})
	t.Run("secret-only-consumers-recreated", func(t *testing.T) {
		before := boots(t)
		values := filepath.Join(work, "secret.yaml")
		if err := os.WriteFile(values, []byte("message: updated-config\ntoken: synthetic-t22-token-updated\n"), 0600); err != nil {
			t.Fatal(err)
		}
		app = compile(t, values)
		apply(t, app)
		for key, id := range boots(t) {
			if (key == storageKey) != (id == before[key]) {
				t.Errorf("wrong secret replacement scope: %s", key)
			}
		}
		for _, info := range apis {
			run(t, info.Key, "api", "sh", "-c", "test \"$TOKEN\" = synthetic-t22-token-updated")
		}
	})
	t.Run("clusterIP-dns-not-pod-IPs", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code, err := runtime.ExecRuntime(ctx, storageKey, "storage", []string{"nslookup", "-type=A", "scenario-c-api.default.svc.cluster.local"}, &stdout, &stderr)
		if err != nil || code != 0 {
			t.Fatalf("Service A lookup failed: code=%d err=%v stdout=%q stderr=%q", code, err, stdout.String(), stderr.String())
		}
		resolved := stdout.String()
		for _, info := range runtime.Sandboxes(app.Identity.Name) {
			if strings.Contains(info.Key, "api") && strings.Contains(resolved, info.IP) {
				t.Fatal("non-headless ClusterIP Service resolves directly to Pod addresses, not a Service VIP")
			}
		}
	})
	t.Run("ingress-endpoint-publication", func(t *testing.T) {
		// Route.Endpoint is the model's published local endpoint. Do not
		// construct a separate proxy or guess a host port to make this pass.
		runtime.mu.Lock()
		route := runtime.desired[app.Identity.Name].Routes[0]
		runtime.mu.Unlock()
		if route.Endpoint == "" {
			t.Fatal("Ingress has no published local endpoint in the runtime application")
		}
		endpoint, err := url.Parse(route.Endpoint)
		if err != nil || endpoint.Scheme != "http" || endpoint.Hostname() != "127.0.0.1" {
			t.Fatal("Ingress must publish an explicit loopback HTTP endpoint for this fixture")
		}
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = route.Hostname
		client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		defer client.CloseIdleConnections()
		response, err := client.Do(req)
		if err != nil {
			t.Fatal("local Ingress request failed", err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(io.LimitReader(response.Body, 1024))
		if err != nil || response.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "updated-config" {
			t.Fatal("Ingress did not route to the ready Service")
		}
	})
	// Service port differs from the container port: raw Pod-IP DNS must not
	// accidentally count as a functional ClusterIP proxy.
	t.Run("service-targetPort", func(t *testing.T) {
		run(t, storageKey, "storage", "wget", "-T", "3", "-q", "-O", "-", "http://scenario-c-api.default.svc.cluster.local:80/")
	})
	t.Run("four-dns-names-and-replica-balancing", func(t *testing.T) {
		for _, name := range []string{"scenario-c-api", "scenario-c-api.default", "scenario-c-api.default.svc", "scenario-c-api.default.svc.cluster.local"} {
			// Query each alias absolutely: BusyBox otherwise also probes search
			// suffixes and can return failure despite a successful answer.
			output := run(t, storageKey, "storage", "nslookup", "-type=A", name+".")
			if !strings.Contains(output, "10.78.") {
				t.Fatalf("%s did not resolve to a Service VIP: %s", name, output)
			}
		}
		for i, info := range apis {
			run(t, info.Key, "api", "sh", "-c", fmt.Sprintf("printf replica-%d > /www/index.html", i))
			defer run(t, info.Key, "api", "sh", "-c", "printf updated-config > /www/index.html")
		}
		seen := map[string]bool{}
		for range 8 {
			seen[run(t, storageKey, "storage", "wget", "-T", "3", "-q", "-O", "-", "http://scenario-c-api:80/")] = true
		}
		if !seen["replica-0"] || !seen["replica-1"] {
			t.Fatalf("Service did not balance across real replicas: %v", seen)
		}
	})
	t.Run("headless-ready-addresses-and-named-srv", func(t *testing.T) {
		headless := app
		headless.Services = append([]model.Service(nil), app.Services...)
		headless.Services[0].Headless = true
		before := boots(t)
		apply(t, headless)
		defer apply(t, app)
		for key, id := range boots(t) {
			if before[key] != id {
				t.Fatal("Service-only change recreated a VM")
			}
		}
		output := run(t, storageKey, "storage", "nslookup", "-type=A", "scenario-c-api.default.svc.cluster.local")
		for _, info := range apis {
			if !strings.Contains(output, info.IP) {
				t.Fatalf("headless lookup missing %s: %s", info.IP, output)
			}
		}
		output = run(t, storageKey, "storage", "nslookup", "-type=SRV", "_http._tcp.scenario-c-api.default.svc.cluster.local")
		if !strings.Contains(output, "8080") || !strings.Contains(output, "pod-") {
			t.Fatalf("headless SRV lacks named target ports: %s", output)
		}
	})
	t.Run("readiness-removes-endpoints", func(t *testing.T) {
		// Establish a working ready Service first; otherwise a permanently
		// broken proxy could falsely pass the negative check below.
		run(t, storageKey, "storage", "wget", "-T", "3", "-q", "-O", "-", "http://scenario-c-api:80/")
		for _, info := range apis {
			run(t, info.Key, "api", "rm", "/tmp/ready")
		}
		defer func() {
			for _, info := range apis {
				run(t, info.Key, "api", "touch", "/tmp/ready")
			}
		}()
		// Probe period/threshold are one second/one failure. Query the actual
		// Service, not a separately constructed test registry or fake balancer.
		time.Sleep(3 * time.Second)
		var stdout, stderr bytes.Buffer
		code, err := runtime.ExecRuntime(ctx, storageKey, "storage", []string{"wget", "-T", "3", "-q", "-O", "-", "http://scenario-c-api:80/"}, &stdout, &stderr)
		if err != nil || code == 0 {
			t.Fatalf("unready Service still reachable or transport failed: code=%d err=%v", code, err)
		}
	})
	t.Run("readiness-restores-endpoints", func(t *testing.T) {
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			var stdout, stderr bytes.Buffer
			code, err := runtime.ExecRuntime(ctx, storageKey, "storage", []string{"wget", "-T", "2", "-q", "-O", "-", "http://scenario-c-api:80/"}, &stdout, &stderr)
			if err == nil && code == 0 && strings.TrimSpace(stdout.String()) == "updated-config" {
				return
			}
			time.Sleep(time.Second)
		}
		t.Fatal("ready Service endpoints were not restored")
	})
	t.Run("published-loopback-port-and-release", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		_ = listener.Close()
		published := app
		published.Workloads = append([]model.Workload(nil), app.Workloads...)
		for i := range published.Workloads {
			if strings.Contains(published.Workloads[i].ID, "storage") {
				published.Workloads[i].Template.Containers = append([]model.Container(nil), published.Workloads[i].Template.Containers...)
				published.Workloads[i].Template.Containers[0].Ports = []model.ContainerPort{{ContainerPort: 9090, HostPort: int32(port), Protocol: "TCP"}}
			}
		}
		apply(t, published)
		run(t, storageKey, "storage", "sh", "-c", "mkdir -p /public && printf published > /public/index.html && httpd -p 9090 -h /public")
		client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}}
		defer client.CloseIdleConnections()
		response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, 1024))
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || string(body) != "published" {
			t.Fatal("host port did not reach the real guest")
		}
		apply(t, app)
		listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Fatal("host port survived removal", err)
		}
		listener.Close()
	})
	t.Run("pvc-persistence", func(t *testing.T) {
		if _, err := reconciler.Down(ctx, app.Identity.Name, false); err != nil {
			t.Fatal(err)
		}
		apply(t, app)
		if run(t, storageKey, "storage", "cat", "/data/row") != "retained-row" {
			t.Fatal("PVC data lost across down/up")
		}
	})
	if _, err := reconciler.Down(ctx, app.Identity.Name, true); err != nil {
		t.Fatal(err)
	}
	if len(runtime.Sandboxes(app.Identity.Name)) != 0 {
		t.Fatal("sandboxes survived down")
	}
	list, err := volumes.List()
	if err != nil || len(list) != 0 {
		t.Fatalf("volumes survived explicit deletion: count=%d err=%v", len(list), err)
	}
}
