//go:build kvm

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"grillo.local/grillo/internal/guestproto"
)

// TestKVMAgentScenarioA boots the real grillo-agent guest with QEMU microvm and
// drives OCI scenario A over vsock with the production protocol client: an init
// container writes a shared file, the app serves over localhost, the sidecar
// reaches it, roots stay separate, PIDs differ, and no zombie remains.
//
// Missing /dev/kvm, qemu, or the built image is a documented SKIP, never a pass.
func TestKVMAgentScenarioA(t *testing.T) {
	client := bootAgentVM(t, 41)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	spec := scenarioASpec()
	start, err := client.Start(ctx, guestproto.StartRequest{Sandbox: &spec})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if start.State != "running" || len(start.Containers) != 3 {
		t.Fatalf("start result = %+v", start)
	}

	status := waitStatus(t, ctx, client, 3)
	if status.State != "running" {
		t.Fatalf("state = %q", status.State)
	}
	pids := map[string]int{}
	for _, c := range status.Containers {
		pids[c.Name] = c.PID
	}
	if pids["app"] == 0 || pids["sidecar"] == 0 || pids["app"] == pids["sidecar"] {
		t.Fatalf("containers must have distinct PIDs: %v", pids)
	}
	requireNoZombies(t, ctx, client)

	// 1. The init container wrote a file the app container can read.
	checkOutput(t, client, ctx, "app", []string{"/bin/cat", "/shared/ready"}, "ready", true)

	// 2. The sidecar reaches the app over shared localhost.
	checkOutput(t, client, ctx, "sidecar", []string{"/bin/wget", "-qO-", "http://127.0.0.1:8080/"}, "grillo-agent-localhost-ok", true)

	// 3. Roots are separate: a file created in the app is absent in the sidecar.
	if _, code, err := execIn(ctx, client, "app", "/bin/sh", "-c", "echo app-marker > /app-marker"); err != nil || code != 0 {
		t.Fatalf("write app marker: code=%d err=%v", code, err)
	}
	out, code, err := execIn(ctx, client, "sidecar", "/bin/cat", "/app-marker")
	if err == nil && code == 0 && strings.Contains(out, "app-marker") {
		t.Fatalf("containers share a root filesystem: sidecar saw %q", out)
	}

	// 4. Stop and confirm the containers are gone.
	if stop, err := client.Stop(ctx, guestproto.StopRequest{}); err != nil || !stop.Stopped {
		t.Fatalf("stop = %+v err=%v", stop, err)
	}
	status, err = client.Status(ctx)
	if err != nil {
		t.Fatalf("status after stop: %v", err)
	}
	for _, c := range status.Containers {
		if c.State == "running" {
			t.Fatalf("container %s still running after stop", c.Name)
		}
	}
	requireNoZombies(t, ctx, client)
}

// TestKVMAgentInitFailure proves a failing init container blocks application
// containers from starting.
func TestKVMAgentInitFailure(t *testing.T) {
	client := bootAgentVM(t, 42)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	spec := scenarioASpec()
	spec.Containers[0].Args = []string{"/bin/sh", "-c", "exit 1"}
	if _, err := client.Start(ctx, guestproto.StartRequest{Sandbox: &spec}); err == nil {
		t.Fatal("expected start to fail when the init container fails")
	}
	status, err := client.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, c := range status.Containers {
		if c.State == "running" {
			t.Fatalf("container %s started despite init failure", c.Name)
		}
	}
	requireNoZombies(t, ctx, client)
}

func bootAgentVM(t *testing.T, cid uint32) *guestproto.Client {
	t.Helper()
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skip("SKIP: /dev/kvm is not available")
	}
	root := repoRoot(t)
	initramfs := filepath.Join(root, "experiments/artifacts/t07/initramfs-agent.cpio.gz")
	keyPath := filepath.Join(root, "experiments/artifacts/t07/key")
	kernel := filepath.Join(root, "experiments/artifacts/qemu/bzImage")
	for _, path := range []string{initramfs, keyPath, kernel} {
		if _, err := os.Stat(path); err != nil {
			t.Skipf("SKIP: missing %s (run 'make t07-guest')", path)
		}
	}
	qemu, err := exec.LookPath("qemu-system-x86_64")
	if err != nil {
		t.Skip("SKIP: qemu-system-x86_64 is not installed")
	}
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(keyData)))
	if err != nil {
		t.Fatalf("decode key: %v", err)
	}

	logPath := filepath.Join(t.TempDir(), "qemu.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{
		"-machine", "microvm,accel=kvm,pcie=off,rtc=on,memory-backend=mem",
		"-cpu", "host", "-smp", "1", "-m", "512",
		"-object", "memory-backend-memfd,id=mem,size=512M,share=on",
		"-no-user-config",
		"-kernel", kernel,
		"-initrd", initramfs,
		"-append", "console=ttyS0 reboot=k panic=1 rdinit=/init",
		"-device", fmt.Sprintf("vhost-vsock-device,guest-cid=%d", cid),
		"-display", "none",
		"-serial", "file:" + logPath,
		"-no-reboot",
	}
	cmd := exec.Command(qemu, args...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
		_ = logFile.Close()
		if t.Failed() {
			dumpTail(t, logPath)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	conn, err := dialAgent(ctx, cid)
	if err != nil {
		t.Fatalf("dial agent: %v", err)
	}
	client, err := guestproto.NewClient(ctx, conn, guestproto.HandshakeConfig{Key: key, Agent: "t07-test"})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func waitStatus(t *testing.T, ctx context.Context, client *guestproto.Client, want int) guestproto.StatusResult {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		status, err := client.Status(ctx)
		if err == nil && len(status.Containers) == want {
			return status
		}
		if time.Now().After(deadline) {
			t.Fatalf("status did not report %d containers: %+v err=%v", want, status, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func requireNoZombies(t *testing.T, ctx context.Context, client *guestproto.Client) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	last := -1
	for {
		status, err := client.Status(ctx)
		if err == nil {
			last = status.Zombies
			if status.Zombies == 0 {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("guest reports %d zombie children", last)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func scenarioASpec() guestproto.SandboxSpec {
	return guestproto.SandboxSpec{
		ID:       "t07",
		Hostname: "pod-t07",
		Containers: []guestproto.ContainerSpec{
			{
				Name:   "setup",
				Init:   true,
				Rootfs: "/rootfs-setup",
				Args:   []string{"/bin/sh", "-c", "echo ready > /shared/ready"},
				Mounts: []guestproto.MountSpec{{Source: "/shared", Target: "/shared"}},
			},
			{
				Name:   "app",
				Rootfs: "/rootfs-app",
				Args:   []string{"/bin/httpd", "-f", "-p", "8080", "-h", "/www"},
				Mounts: []guestproto.MountSpec{{Source: "/shared", Target: "/shared", ReadOnly: true}},
			},
			{
				Name:   "sidecar",
				Rootfs: "/rootfs-sidecar",
				Args:   []string{"/bin/sleep", "300"},
			},
		},
	}
}

func dialAgent(ctx context.Context, cid uint32) (guestproto.Conn, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		conn, err := guestproto.DialVsock(ctx, cid, 1024)
		if err == nil {
			return conn, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func execIn(ctx context.Context, client *guestproto.Client, container string, args ...string) (string, int, error) {
	var stdout bytes.Buffer
	result, err := client.Exec(ctx, guestproto.ExecRequest{Container: container, Args: args}, &stdout, io.Discard)
	if err != nil {
		return "", 0, err
	}
	return stdout.String(), result.ExitCode, nil
}

func checkOutput(t *testing.T, client *guestproto.Client, ctx context.Context, container string, args []string, want string, wantExitZero bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var lastOut string
	var lastCode int
	var lastErr error
	for {
		out, code, err := execIn(ctx, client, container, args...)
		lastOut, lastCode, lastErr = out, code, err
		if err == nil && strings.Contains(out, want) && (!wantExitZero || code == 0) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("exec %s %v: want %q, got out=%q code=%d err=%v", container, args, want, lastOut, lastCode, lastErr)
		}
		time.Sleep(300 * time.Millisecond)
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

func dumpTail(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	const limit = 8192
	if len(data) > limit {
		data = data[len(data)-limit:]
	}
	t.Logf("--- qemu serial tail ---\n%s", data)
}
