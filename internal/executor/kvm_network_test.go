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

	"grillo.local/grillo/internal/backend/qemu"
	"grillo.local/grillo/internal/guestproto"
	"grillo.local/grillo/internal/model"
	"grillo.local/grillo/internal/reconcile"
	"grillo.local/grillo/internal/sandbox"
	"grillo.local/grillo/internal/storage"
)

// TestKVMBridgedApplicationNetwork applies an application with the production
// rootless network topology and verifies cross-VM reachability, DNS records with
// real addresses, guest egress, and application isolation.
//
// Missing /dev/kvm, pasta, a built netns binary, or SELinux Enforcing is a SKIP.
func TestKVMBridgedApplicationNetwork(t *testing.T) {
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
	keyData, _ := os.ReadFile(keyPath)
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(keyData)))
	if err != nil {
		t.Fatal(err)
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

	var exec *Executor
	backend, err := qemu.Open(qemu.Config{
		Kernel: kernel, Initramfs: initramfs,
		WorkDir: filepath.Join(work, "backend"), CIDBase: 110, BootTimeout: 30 * time.Second,
		Launch: func(ctx context.Context, spec sandbox.Spec, args []string, logPath string) (int, error) {
			return exec.LaunchSandbox(ctx, spec, args, logPath)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	volumes, err := storage.Open(filepath.Join(work, "storage"))
	if err != nil {
		t.Fatal(err)
	}
	exec, err = New(Config{
		Backend: backend, Volumes: volumes, Kernel: kernel, Initramfs: initramfs,
		KernelArgs: "console=ttyS0 reboot=k panic=1 rdinit=/init",
		GuestKey:   key, VsockPort: 1024, VsockCIDBase: 110,
		Images:        directoryResolver{dir: rootfs},
		EnableNetwork: true, QEMU: "qemu-system-x86_64", NetnsBinary: netnsBin, Pasta: "pasta", RuntimeDir: runtimeDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer exec.Close()
	reconciler := &reconcile.Reconciler{Store: reconcile.NewMemoryStore(), Executor: &reconcile.NativeExecutor{Sandboxes: SandboxController{exec}, Volumes: VolumeController{exec}}, MaxAttempts: 1}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	app := bridgedApp("bridged", 2, "/bin/httpd", "-f", "-p", "8080", "-h", "/www")
	exec.SetDesired(app)
	if _, err := reconciler.Apply(ctx, app); err != nil {
		t.Fatalf("apply: %v", err)
	}
	defer reconciler.Down(ctx, "bridged", false)

	infos := exec.Sandboxes("bridged")
	if len(infos) != 2 {
		t.Fatalf("sandboxes = %+v", infos)
	}
	if infos[0].IP == "" || infos[0].IP == infos[1].IP {
		t.Fatalf("sandbox addresses are not unique: %+v", infos)
	}

	// Cross-VM reachability: one sandbox fetches the other's page.
	expected, _ := os.ReadFile(filepath.Join(rootfs, "www", "index.html"))
	want := strings.TrimSpace(string(expected))
	runInEventually(t, ctx, exec, infos[0].Key, want, "/bin/wget", "-qO-", "http://"+infos[1].IP+":8080/")

	// DNS records carry the real sandbox addresses. nslookup exits non-zero from
	// search-domain NXDOMAIN noise, so only the answer is asserted.
	var dnsOut, dnsErr bytes.Buffer
	_, _ = exec.ExecRuntime(ctx, infos[0].Key, "web", []string{"/bin/nslookup", "web.default.svc.cluster.local"}, &dnsOut, &dnsErr)
	for _, info := range infos {
		if !strings.Contains(dnsOut.String(), info.IP) {
			t.Fatalf("DNS output %q missing %s", dnsOut.String(), info.IP)
		}
	}

	// Guest egress through the bridge and pasta NAT.
	if err := guestEgress(ctx, infos[0], key); err != nil {
		t.Fatalf("guest egress: %v", err)
	}

	// Isolation: a second application cannot reach the first, even at the same
	// address, because it is on its own bridge.
	other := bridgedApp("other", 1, "/bin/sh", "-c", "echo B > /srv/index.html && exec httpd -f -p 8080 -h /srv")
	other.Volumes = []model.Volume{{Name: "content", Kind: model.VolumeManaged}}
	other.Workloads[0].Template.Containers[0].Mounts = []model.VolumeMount{{Volume: "content", MountPath: "/srv"}}
	exec.SetDesired(other)
	if _, err := reconciler.Apply(ctx, other); err != nil {
		t.Fatalf("apply other: %v", err)
	}
	defer reconciler.Down(ctx, "other", false)
	otherInfos := exec.Sandboxes("other")
	if len(otherInfos) != 1 {
		t.Fatalf("other sandboxes = %+v", otherInfos)
	}
	otherOut := runInEventually(t, ctx, exec, otherInfos[0].Key, "B", "/bin/wget", "-qO-", "http://"+infos[0].IP+":8080/")
	if strings.Contains(otherOut, want) {
		t.Fatalf("application isolation failed: other reached bridged (%q)", otherOut)
	}
}

func skipUnlessBridged(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skip("SKIP: /dev/kvm is not available")
	}
	if enforce, err := os.ReadFile("/sys/fs/selinux/enforce"); err == nil && strings.TrimSpace(string(enforce)) == "1" {
		t.Skip("SKIP: SELinux Enforcing kills pasta helpers on the tested policy")
	}
	if _, err := exec.LookPath("pasta"); err != nil {
		t.Skip("SKIP: pasta is not installed")
	}
}

func bridgedApp(name string, replicas int, command ...string) model.Application {
	return model.Application{
		APIVersion: model.APIVersion, Kind: model.KindApplication,
		Identity: model.Identity{Name: name, Namespace: "default"},
		Workloads: []model.Workload{{
			ID: "web", Kind: model.WorkloadDeployment, Replicas: int32(replicas),
			Labels: map[string]string{"app": name},
			Template: model.SandboxTemplate{Containers: []model.Container{{
				Name:    "web",
				Image:   model.ImageRef{Reference: "busybox:1.37"},
				Command: &command,
			}}},
		}},
		Services: []model.Service{{Name: "web", Selector: map[string]string{"app": name}, Ports: []model.ServicePort{{Port: 8080}}}},
	}
}

// runInEventually retries a command until its output contains want, so a
// transient bridge/ARP delay does not fail the test.
func runInEventually(t *testing.T, ctx context.Context, exec *Executor, key, want string, args ...string) string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	last := ""
	for time.Now().Before(deadline) {
		var stdout, stderr bytes.Buffer
		code, err := exec.ExecRuntime(ctx, key, "web", args, &stdout, &stderr)
		last = stdout.String() + stderr.String()
		if err == nil && code == 0 && strings.Contains(last, want) {
			return stdout.String()
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("exec %v did not produce %q: %q", args, want, last)
	return ""
}

func runIn(t *testing.T, ctx context.Context, exec *Executor, key string, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code, err := exec.ExecRuntime(ctx, key, "web", args, &stdout, &stderr)
	if err != nil || code != 0 {
		t.Fatalf("exec %v: code=%d err=%v stdout=%q stderr=%q", args, code, err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func guestEgress(ctx context.Context, info SandboxInfo, key []byte) error {
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := guestproto.DialVsock(dialCtx, info.CID, 1024)
	if err != nil {
		return err
	}
	client, err := guestproto.NewClient(dialCtx, conn, guestproto.HandshakeConfig{Key: key, Agent: "egress"})
	if err != nil {
		return err
	}
	defer client.Close()
	result, err := client.Probe(dialCtx, guestproto.ProbeRequest{Kind: guestproto.ProbeTCP, Host: "1.1.1.1", Port: 443, TimeoutMS: 5000})
	if err != nil {
		return err
	}
	if !result.Healthy {
		return errString("guest egress denied: " + result.Detail)
	}
	return nil
}

type errString string

func (e errString) Error() string { return string(e) }
