//go:build linux && kvm

// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/api"
	"github.com/albertize/grillo/internal/build"
	"github.com/albertize/grillo/internal/frontend/kubernetes"
	"github.com/albertize/grillo/internal/guestproto"
	"github.com/albertize/grillo/internal/ui"
)

// This gate deliberately starts the actual daemon, not a differently wired
// executor fixture. All data, sockets, images and process logs are test-owned.
func TestKVMDaemonRemediation(t *testing.T) { runDaemonGate(t, false) }

// TestKVMDaemonUI adds a real Firefox surface to the actual native daemon gate.
func TestKVMDaemonUI(t *testing.T) {
	for _, tool := range []string{"firefox", "node"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("browser prerequisite missing: %s", tool)
		}
	}
	runDaemonGate(t, true)
}

func runDaemonGate(t *testing.T, browser bool) {
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
	for _, tool := range []string{"pasta", "qemu-system-x86_64", "ip", "nft", "helm"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("missing %s", tool)
		}
	}
	work, err := os.MkdirTemp("", "g16-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(work) })
	daemon, helper := filepath.Join(work, "grillod"), filepath.Join(work, "grillo-netns")
	cli := filepath.Join(work, "grillo")
	for _, b := range []struct{ path, pkg string }{{daemon, "./cmd/grillod"}, {helper, "./cmd/grillo-netns"}, {cli, "./cmd/grillo"}} {
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
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	// Leave the ordinary daemon/demo's default CID range untouched. These
	// explicit test ranges also separate the native builder from workloads.
	cidBase := 32768 + (os.Getpid()%4096)*128
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
		proc = exec.Command(daemon, "-kernel", kernel, "-initramfs", guest, "-key-file", key, "-netns-binary", helper, "-vsock-cid-base", fmt.Sprint(cidBase), "-build-vsock-cid-base", fmt.Sprint(cidBase+64))
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
		go func() { done <- cmd.Wait(); close(done); f.Close() }()
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
	t.Run("helm-cli-plan-up-inspect-down", func(t *testing.T) {
		values := filepath.Join(work, "helm-values.yaml")
		if err := os.WriteFile(values, []byte("image: review:local\nmessage: cli-config\ntoken: synthetic-cli-private-token\n"), 0600); err != nil {
			t.Fatal(err)
		}
		chart := filepath.Join(root, "examples/helm/scenario-c")
		runCLI := func(args ...string) string {
			t.Helper()
			cmd := exec.CommandContext(ctx, cli, args...)
			cmd.Env = env
			cmd.Dir = root
			out, err := cmd.CombinedOutput()
			if strings.Contains(string(out), "synthetic-cli-private-token") {
				t.Fatal("CLI leaked private secret")
			}
			if err != nil {
				t.Fatalf("CLI %s failed: %v %s", args[0], err, out)
			}
			return string(out)
		}
		planOutput := runCLI("plan", chart, "--release", "scenario-c", "-f", values, "--output=json")
		if !strings.Contains(planOutput, "\"applicable\": true") {
			t.Fatal("supported chart plan not applicable")
		}
		defer func() { waitOp(client.Down(ctx, "scenario-c", true)) }()
		runCLI("up", chart, "--release", "scenario-c", "-f", values)
		states, err := client.Status(ctx, "scenario-c")
		if err != nil || len(states) != 7 {
			t.Fatalf("wrong real chart container inventory: %d %v", len(states), err)
		}
		counts := map[string]int{}
		for _, state := range states {
			counts[state.Container]++
		}
		if counts["api"] != 2 || counts["sidecar"] != 2 || counts["init"] != 2 || counts["storage"] != 1 {
			t.Fatalf("wrong Pod/init topology: %v", counts)
		}
		inspection := runCLI("inspect", "scenario-c")
		if !strings.Contains(inspection, "http://127.0.0.1:") {
			t.Fatal("inspect omitted actual Ingress fallback")
		}
		code, out, _, err := client.Exec(ctx, "scenario-c", "storage", []string{"/bin/busybox", "wget", "-T", "3", "-q", "-O", "-", "http://scenario-c-api:80/"})
		if err != nil || code != 0 || strings.TrimSpace(out) != "cli-config" {
			t.Fatalf("CLI-applied Service failed: code=%d err=%v", code, err)
		}
		t.Run("guest-stdout-stderr-to-api-cli", func(t *testing.T) {
			// Write to the running container's own stdio, not the exec session
			// capture. PID 1 here is the container process in its PID namespace.
			code, _, _, err := client.Exec(ctx, "scenario-c", "storage", []string{"/bin/sh", "-c", "echo 'live-storage-stdout <img src=x onerror=window.pwned=true>' > /proc/1/fd/1; echo live-storage-stderr > /proc/1/fd/2"})
			if err != nil || code != 0 {
				t.Fatalf("write workload stdio: code=%d err=%v", code, err)
			}
			found := map[string]bool{}
			resource := ""
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				records, err := client.ListLogs(ctx, 0, "", "storage")
				if err != nil {
					t.Fatal(err)
				}
				for _, record := range records {
					if strings.Contains(record.Line, "live-storage-"+record.Stream) {
						found[record.Stream] = true
						resource = record.Resource
					}
				}
				if found["stdout"] && found["stderr"] {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if !found["stdout"] || !found["stderr"] {
				t.Fatalf("missing persisted workload logs: %v", found)
			}
			output := runCLI("logs", resource, "--container", "storage")
			if !strings.Contains(output, "live-storage-stdout") || !strings.Contains(output, "live-storage-stderr") {
				t.Fatal("CLI did not consume the real log spool")
			}
		})
		t.Run("interactive-cli-metrics-artifacts", func(t *testing.T) {
			input := make(chan guestproto.Frame, 4)
			input <- guestproto.SizeFrame(37, 99)
			input <- guestproto.Frame{Stream: guestproto.StreamStdin, Data: []byte("api-tty-marker\n")}
			var output, stderr bytes.Buffer
			code, err := client.ExecAttached(ctx, api.AttachRequest{Application: "scenario-c", Container: "storage", TTY: true, Args: []string{"/bin/sh", "-c", "test -t 0 || exit 9; read line; echo $line; stty size; exit 7"}}, input, &output, &stderr)
			if err != nil || code != 7 || !strings.Contains(output.String(), "api-tty-marker") || !strings.Contains(output.String(), "37 99") || stderr.Len() != 0 {
				t.Fatalf("duplex API: code=%d err=%v output=%q stderr=%q", code, err, output.String(), stderr.String())
			}
			command := exec.CommandContext(ctx, cli, "exec", "-i", "-t", "scenario-c", "storage", "--", "/bin/sh", "-c", "test -t 0 || exit 9; read line; echo CLI:$line; exit 7")
			command.Env = env
			command.Dir = root
			command.Stdin = strings.NewReader("cli-tty-marker\n")
			out, err := command.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 7 || !strings.Contains(string(out), "CLI:cli-tty-marker") {
				t.Fatalf("real CLI TTY: %v %q", err, out)
			}
			view, err := client.View(ctx, "scenario-c")
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range view.Sandboxes {
				if s.Guest == nil || s.Guest.MemoryTotalBytes == nil || s.Guest.CPUBusyTicks == nil {
					t.Fatal("actual guest metrics absent")
				}
				for _, c := range s.Containers {
					if c.State == "running" && (c.Usage == nil || c.Usage.MemoryBytes == nil || c.Usage.CPUUsec == nil) {
						t.Fatalf("actual container metrics absent: %s", c.Name)
					}
				}
			}
			exerciseHostTerminal(t, ctx, cli, root, env)
			if out := runCLI("metrics", "scenario-c", "--output=json"); !strings.Contains(out, "memoryTotalBytes") || !strings.Contains(out, "cpuUsec") {
				t.Fatal("metrics CLI missing actual counters")
			}
			attachCtx, abort := context.WithCancel(ctx)
			done := make(chan error, 1)
			go func() {
				_, err := client.ExecAttached(attachCtx, api.AttachRequest{Application: "scenario-c", Container: "storage", TTY: true, Args: []string{"/bin/sh", "-c", "echo $$ > /tmp/attach.pid; exec /bin/sleep 300"}}, make(chan guestproto.Frame), io.Discard, io.Discard)
				done <- err
			}()
			deadline := time.Now().Add(5 * time.Second)
			pid := ""
			for time.Now().Before(deadline) {
				code, out, _, err := client.Exec(ctx, "scenario-c", "storage", []string{"/bin/cat", "/tmp/attach.pid"})
				if err == nil && code == 0 {
					pid = strings.TrimSpace(out)
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			abort()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("abort reported success")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("interactive cancellation hung")
			}
			if _, err := strconv.Atoi(pid); err != nil {
				t.Fatal("exec PID not observed before abort")
			}
			deadline = time.Now().Add(5 * time.Second)
			gone := false
			for time.Now().Before(deadline) {
				code, _, _, err := client.Exec(ctx, "scenario-c", "storage", []string{"/bin/kill", "-0", pid})
				if err == nil && code != 0 {
					gone = true
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if !gone {
				t.Fatal("cancelled TTY exec process survived")
			}
		})
		t.Run("ui-lifetime-and-exec", func(t *testing.T) {
			before, err := client.View(ctx, "scenario-c")
			if err != nil || len(before.Sandboxes) != 3 {
				t.Fatalf("UI snapshot: %v %+v", err, before)
			}
			bridge := ui.New(client)
			server := httptest.NewServer(bridge.Handler())
			defer server.Close()
			if browser {
				// Exercise the actual CLI bridge and its reported ephemeral port.
				uiProcess := exec.CommandContext(ctx, cli, "ui", "--port", "0")
				uiProcess.Env = env
				stdout, err := uiProcess.StdoutPipe()
				if err != nil {
					t.Fatal(err)
				}
				if err := uiProcess.Start(); err != nil {
					t.Fatal(err)
				}
				uiDone := make(chan error, 1)
				go func() { uiDone <- uiProcess.Wait() }()
				stopped := false
				stopUI := func() {
					if stopped {
						return
					}
					stopped = true
					_ = uiProcess.Process.Signal(os.Interrupt)
					select {
					case err := <-uiDone:
						if err != nil {
							t.Errorf("CLI UI exit: %v", err)
						}
					case <-time.After(5 * time.Second):
						_ = uiProcess.Process.Kill()
						<-uiDone
						t.Error("CLI UI failed graceful shutdown")
					}
				}
				defer stopUI()
				urls := make(chan string, 1)
				go func() {
					scanner := bufio.NewScanner(stdout)
					if scanner.Scan() {
						urls <- strings.TrimPrefix(scanner.Text(), "Grillo UI: ")
					} else {
						urls <- ""
					}
				}()
				var uiURL string
				select {
				case uiURL = <-urls:
				case <-ctx.Done():
					t.Fatal("UI startup timeout")
				}
				if !strings.HasPrefix(uiURL, "http://127.0.0.1:") || strings.Contains(uiURL, ":0/") {
					t.Fatal("CLI did not report assigned UI port")
				}
				cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "scripts/ui-browser-smoke.mjs"))
				cmd.Env = append(os.Environ(), "GRILLO_UI_TEST_URL="+uiURL, "GRILLO_UI_TEST_LIVE=1")
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("real browser/daemon gate: %v %s", err, out)
				}
				t.Log(string(out))
				stopUI()
			} else {
				resp, err := server.Client().Post(server.URL+"/session", "application/json", strings.NewReader(`{"token":"`+bridge.BootstrapToken()+`"}`))
				if err != nil {
					t.Fatal(err)
				}
				cookies := resp.Cookies()
				resp.Body.Close()
				if len(cookies) != 1 {
					t.Fatal("UI authentication failed")
				}
				req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/v1/applications/scenario-c/view", nil)
				req.AddCookie(cookies[0])
				resp, err = server.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				var view any
				err = json.NewDecoder(resp.Body).Decode(&view)
				resp.Body.Close()
				if err != nil || resp.StatusCode != 200 {
					t.Fatalf("UI view: %v", err)
				}
				data, _ := json.Marshal(view)
				if strings.Contains(string(data), "synthetic-cli-private-token") {
					t.Fatal("secret leaked to UI")
				}
			}
			// Closing only the bridge must not replace or stop any microVM.
			server.Close()
			after, err := client.View(ctx, "scenario-c")
			if err != nil || len(after.Sandboxes) != len(before.Sandboxes) {
				t.Fatal("UI closure changed workload inventory", err)
			}
			for i, vm := range after.Sandboxes {
				if vm.State != "running" || vm.VMM == nil || before.Sandboxes[i].VMM == nil || vm.VMM.PID != before.Sandboxes[i].VMM.PID {
					t.Fatal("UI closure stopped/replaced VMM")
				}
			}
			code, _, _, err := client.Exec(ctx, "scenario-c", "storage", []string{"/bin/busybox", "true"})
			if err != nil || code != 0 {
				t.Fatal("workload unavailable after UI closure", err)
			}
		})
		runCLI("up", chart, "--release", "scenario-c", "-f", values)
		runCLI("down", "scenario-c", "--volumes")
		states, err = client.Status(ctx, "scenario-c")
		if err != nil || len(states) != 0 {
			t.Fatal("CLI down left containers", err)
		}
	})
	if t.Failed() {
		return
	}
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
	if err != nil || len(names) != 2 || names[0] != "review" || names[1] != "scenario-c" {
		t.Fatalf("recovery inventory: %v %v", names, err)
	}
	stopped, err := client.Status(ctx, "scenario-c")
	if err != nil || len(stopped) != 0 {
		t.Fatal("stopped CLI chart restarted", err)
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
