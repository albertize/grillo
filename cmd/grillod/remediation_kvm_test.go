//go:build linux && kvm

// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grillo.local/grillo/internal/api"
	"grillo.local/grillo/internal/build"
	"grillo.local/grillo/internal/frontend/kubernetes"
)

// This gate deliberately starts the actual daemon, not a differently wired
// executor fixture. All data, sockets, images and process logs are test-owned.
func TestKVMDaemonRemediation(t *testing.T) {
	for _, device := range []string{"/dev/kvm", "/dev/vhost-vsock"} {
		f, err := os.OpenFile(device, os.O_RDWR, 0)
		if err != nil {
			t.Skipf("hardware unavailable: %v", err)
		}
		f.Close()
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	kernel := filepath.Join(root, "experiments/artifacts/qemu/bzImage")
	guest := filepath.Join(root, "experiments/artifacts/t07/initramfs-agent.cpio.gz")
	key := filepath.Join(root, "experiments/artifacts/t07/key")
	busybox := filepath.Join(root, "experiments/artifacts/t02/rootfs/bin/busybox")
	for _, p := range []string{kernel, guest, key, busybox} {
		if _, err := os.Stat(p); err != nil {
			t.Skipf("missing artifact: %s", p)
		}
	}
	for _, tool := range []string{"pasta", "qemu-system-x86_64", "ip", "nft"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("missing %s", tool)
		}
	}
	work, err := os.MkdirTemp("", "grillo-review-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(work) })
	daemon, helper := filepath.Join(work, "grillod"), filepath.Join(work, "grillo-netns")
	for _, b := range []struct{ path, pkg string }{{daemon, "./cmd/grillod"}, {helper, "./cmd/grillo-netns"}} {
		cmd := exec.Command("go", "build", "-o", b.path, b.pkg)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build: %v %s", err, out)
		}
	}
	env := append([]string{}, os.Environ()...)
	for _, v := range []struct{ name, dir string }{{"XDG_RUNTIME_DIR", "run"}, {"XDG_STATE_HOME", "state"}, {"XDG_DATA_HOME", "data"}, {"XDG_CACHE_HOME", "cache"}} {
		dir := filepath.Join(work, v.dir)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		env = append(env, v.name+"="+dir)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	socket := filepath.Join(work, "run/grillo/grillod.sock")
	client := api.NewClient(socket)
	var proc *exec.Cmd
	var exited chan error
	logPath := filepath.Join(work, "daemon.log")
	start := func() {
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		proc = exec.Command(daemon, "-kernel", kernel, "-initramfs", guest, "-key-file", key, "-netns-binary", helper)
		proc.Env = env
		proc.Dir = root
		proc.Stdout = f
		proc.Stderr = f
		if err := proc.Start(); err != nil {
			f.Close()
			t.Fatal(err)
		}
		exited = make(chan error, 1)
		cmd := proc
		done := exited
		go func() { done <- cmd.Wait(); f.Close() }()
		for {
			if _, err := client.Health(ctx); err == nil {
				return
			}
			select {
			case err := <-exited:
				data, _ := os.ReadFile(logPath)
				t.Fatalf("daemon exited: %v\n%s", err, data)
			case <-ctx.Done():
				data, _ := os.ReadFile(logPath)
				t.Fatalf("daemon timeout: %s", data)
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	waitOp := func(id string, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		for {
			op, err := client.Operation(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if op.State == "succeeded" {
				return
			}
			if op.State != "running" {
				t.Fatalf("operation failed: %+v", op.Error)
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	start()
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if id, err := client.Down(cleanup, "review", true); err == nil {
			for {
				op, err := client.Operation(cleanup, id)
				if err != nil || op.State != "running" {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
		}
		_ = client.Shutdown(cleanup)
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			_ = proc.Process.Kill()
			<-exited
		}
		if t.Failed() {
			data, _ := os.ReadFile(logPath)
			t.Logf("daemon log:\n%s", data)
		}
	})
	contextDir := filepath.Join(work, "context")
	for _, dir := range []string{"bin", "etc", "proc", "sys", "dev", "tmp", "www"} {
		if err := os.MkdirAll(filepath.Join(contextDir, "root", dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	// The pinned fixture is dynamically linked: include its loader, libraries
	// and applet symlinks, not just the busybox executable.
	copyFixture := exec.Command("cp", "-a", filepath.Join(root, "experiments/artifacts/t02/rootfs")+"/.", filepath.Join(contextDir, "root"))
	if out, err := copyFixture.CombinedOutput(); err != nil {
		t.Fatalf("copy fixture: %v %s", err, out)
	}
	for path, data := range map[string]string{"Dockerfile": "FROM scratch\nCOPY root/ /\n", "root/etc/resolv.conf": "", "root/www/index.html": "daemon-network-ok\n"} {
		if err := os.WriteFile(filepath.Join(contextDir, path), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	waitOp(client.Build(ctx, build.Request{ContextDir: contextDir, Reference: "review:local"}))
	manifest := `apiVersion: v1
kind: PersistentVolumeClaim
metadata: {name: claim}
spec: {accessModes: [ReadWriteOnce], resources: {requests: {storage: 1Mi}}}
---
apiVersion: v1
kind: Pod
metadata: {name: web, labels: {app: web}}
spec:
  containers:
  - name: web
    image: review:local
    command: [/bin/busybox, httpd, -f, -p, "8080", -h, /www]
---
apiVersion: v1
kind: Service
metadata: {name: web}
spec: {selector: {app: web}, ports: [{port: 8080}]}
---
apiVersion: v1
kind: Pod
metadata: {name: worker}
spec:
  volumes:
  - name: local-alias
    persistentVolumeClaim: {claimName: claim}
  containers:
  - name: worker
    image: review:local
    command: [/bin/busybox, sleep, "300"]
    volumeMounts: [{name: local-alias, mountPath: /data}]
---
apiVersion: v1
kind: Pod
metadata: {name: secure}
spec:
  securityContext: {runAsUser: 1000, runAsGroup: 1000}
  containers:
  - name: secure
    image: review:local
    command: [/bin/busybox, sleep, "300"]
    securityContext: {readOnlyRootFilesystem: true}
    resources: {limits: {cpu: 100m}}
`
	compiled, err := kubernetes.Compile(ctx, []byte(manifest), kubernetes.Options{Path: "review.yaml"})
	if err != nil || compiled.Diagnostics.HasErrors() {
		t.Fatalf("%v %+v", err, compiled.Diagnostics)
	}
	waitOp(client.Apply(ctx, compiled.Application))
	run := func(container, script string) string {
		t.Helper()
		code, out, stderr, err := client.Exec(ctx, "review", container, []string{"/bin/busybox", "sh", "-c", script})
		if err != nil || code != 0 {
			t.Fatalf("exec %s: exit=%d err=%v stdout=%q stderr=%q", container, code, err, out, stderr)
		}
		return out
	}
	if out := run("worker", "/bin/busybox wget -q -O - http://web:8080/"); !strings.Contains(out, "daemon-network-ok") {
		t.Fatalf("network: %q", out)
	}
	run("web", "echo private > /private-marker")
	run("worker", "test ! -e /private-marker; echo persistent > /data/marker")
	for _, base := range []string{"cache/grillo/rootfs"} {
		err := filepath.WalkDir(filepath.Join(work, base), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Name() == "private-marker" {
				return fmt.Errorf("image cache was modified")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if out := run("secure", "/bin/busybox id -u"); strings.TrimSpace(out) != "1000" {
		t.Fatalf("UID: %q", out)
	}
	code, _, _, err := client.Exec(ctx, "review", "secure", []string{"/bin/busybox", "touch", "/www/index.html"})
	if err != nil || code == 0 {
		t.Fatalf("read-only root accepted write: %d %v", code, err)
	}
	if out := run("secure", "/bin/busybox cat /sys/fs/cgroup/cpu.max"); strings.TrimSpace(out) != "100000 1000000" {
		t.Fatalf("CPU quota: %q", out)
	}
	// Crash the actual daemon, preserving its private on-disk state.
	if err := proc.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	<-exited
	start()
	names, err := client.Applications(ctx)
	if err != nil || len(names) != 1 || names[0] != "review" {
		t.Fatalf("recovery inventory: %v %v", names, err)
	}
	run("worker", "test ! -e /private-marker; /bin/busybox grep persistent /data/marker")
	run("web", "test ! -e /private-marker")
	if out := run("worker", "/bin/busybox wget -q -O - http://web:8080/"); !strings.Contains(out, "daemon-network-ok") {
		t.Fatal("network did not recover")
	}
	waitOp(client.Down(ctx, "review", false))
	if err := proc.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	<-exited
	start()
	states, err := client.Status(ctx, "review")
	if err != nil || len(states) != 0 {
		t.Fatalf("stopped application restarted: %+v %v", states, err)
	}
}
