//go:build linux && kvm

// SPDX-License-Identifier: Apache-2.0

package distribution

import (
	"bufio"
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
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/api"
	"github.com/albertize/grillo/internal/guest"
	linux "github.com/albertize/grillo/internal/platform/linux"
	"golang.org/x/sys/unix"
)

func TestKVMInstalledRuntime(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("SKIP: runtime gate requires an unprivileged user")
	}
	for _, device := range []string{"/dev/kvm", "/dev/vhost-vsock", "/dev/net/tun"} {
		file, err := os.OpenFile(device, os.O_RDWR, 0)
		if err != nil {
			t.Skipf("SKIP: unavailable device %s", device)
		}
		file.Close()
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	selection := os.Getenv("GRILLO_TEST_GUEST_SELECTION")
	if !filepath.IsAbs(selection) {
		selection = filepath.Join(root, selection)
	}
	data, err := os.ReadFile(selection)
	if err != nil || len(data) > 4096 {
		t.Fatal("run make runtime-guest first")
	}
	guestDir := strings.TrimSpace(string(data))
	helmBinary, helmDigest := os.Getenv("GRILLO_TEST_HELM_BINARY"), os.Getenv("GRILLO_TEST_HELM_SHA256")
	if helmBinary == "" || helmDigest == "" {
		t.Fatal("explicit trusted Helm helper/pin required")
	}
	// Short private state paths are required by Unix sockets; prefix may have spaces.
	work, err := os.MkdirTemp("", "gd0-")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()
	original := filepath.Join(work, "original prefix")
	cfg := StageConfig{Output: original, Binaries: filepath.Join(root, "bin"), Guest: guestDir, Helm: helmBinary, HelmSHA256: helmDigest, Examples: filepath.Join(root, "examples/distribution"), Documents: root}
	if err := Stage(ctx, cfg); err != nil {
		os.RemoveAll(work)
		t.Fatal(err)
	}
	inventoryData, err := os.ReadFile(filepath.Join(original, "share/doc/grillo/VERSION.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stagedInventory Inventory
	if err := json.Unmarshal(inventoryData, &stagedInventory); err != nil {
		t.Fatal(err)
	}
	if len(stagedInventory.BinaryProvenance) != 4 || !strings.HasPrefix(stagedInventory.NoticeCoverage, "partial:") {
		t.Fatal("missing binary/notice provenance")
	}
	if _, err := os.Stat(filepath.Join(original, "share/doc/grillo/third-party/font-awesome-LICENSE.txt")); err != nil {
		t.Fatal("missing third-party notice", err)
	}
	prefix := filepath.Join(work, "moved prefix with spaces")
	if err := os.Rename(original, prefix); err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(prefix, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.Chmod(path, info.Mode().Perm()&^0222)
	}); err != nil {
		t.Fatal(err)
	}
	// Negative secret scan uses the development fixture credential, never logs it.
	fixtureKey, err := os.ReadFile(filepath.Join(root, "experiments/artifacts/t07/key"))
	if err != nil {
		t.Fatal("missing fixture key for regression scan")
	}
	rawKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(fixtureKey)))
	if err != nil {
		t.Fatal("invalid fixture key")
	}
	image, err := os.ReadFile(filepath.Join(prefix, "lib/grillo/guest/1.1/initramfs.cpio.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if err := guest.VerifyRuntimeImage(image, [][]byte{rawKey, bytes.TrimSpace(fixtureKey)}); err != nil {
		t.Fatal("runtime secret/namespace scan", err)
	}
	// Runtime PATH contains only tested distro-managed dependencies, no checkout,
	// Go, Node, npm, Podman, external Helm, Grillo daemon or network helper.
	tools := filepath.Join(work, "tools")
	if err := os.Mkdir(tools, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"qemu-system-x86_64", "pasta", "ip", "nft"} {
		source, err := exec.LookPath(name)
		if err != nil {
			t.Skipf("SKIP: missing host dependency %s", name)
		}
		absolute, _ := filepath.Abs(source)
		if err := os.Symlink(absolute, filepath.Join(tools, name)); err != nil {
			t.Fatal(err)
		}
	}
	cwd := filepath.Join(work, "arbitrary cwd")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	env := []string{}
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "GRILLO_") && !strings.HasPrefix(value, "XDG_") && !strings.HasPrefix(value, "PATH=") && !strings.HasPrefix(value, "HOME=") {
			env = append(env, value)
		}
	}
	env = append(env, "PATH="+tools, "HOME="+work)
	for _, variable := range []struct{ name, dir string }{{"XDG_RUNTIME_DIR", "r"}, {"XDG_STATE_HOME", "s"}, {"XDG_DATA_HOME", "d"}, {"XDG_CACHE_HOME", "c"}} {
		dir := filepath.Join(work, variable.dir)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		env = append(env, variable.name+"="+dir)
	}
	socket := filepath.Join(work, "r/grillo/grillod.sock")
	client := api.NewClient(socket)
	var daemon linux.ProcessID
	var vmIdentities []linux.ProcessID
	cleanupOK := false
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		healthy := false
		if _, err := client.Health(cleanup); err == nil {
			healthy = true
		}
		ok := true
		if healthy {
			for _, name := range []string{"hello-compose", "hello-helm"} {
				id, err := client.Down(cleanup, name, false)
				if err != nil {
					t.Error("cleanup down", name, err)
					ok = false
					continue
				}
				if err := waitInstalledOperation(cleanup, client, id); err != nil {
					t.Error("cleanup operation", err)
					ok = false
				}
			}
			if err := client.Shutdown(cleanup); err != nil {
				t.Error("daemon shutdown", err)
				ok = false
			}
		}
		for daemon.Alive() && cleanup.Err() == nil {
			time.Sleep(50 * time.Millisecond)
		}
		if daemon.Alive() {
			t.Error("owned daemon still alive; state retained")
			ok = false
		}
		for _, identity := range vmIdentities {
			if identity.Alive() {
				t.Error("owned VMM still alive; state retained")
				ok = false
			}
		}
		for _, dir := range []string{"backend", "build-backend"} {
			entries, err := os.ReadDir(filepath.Join(work, "d/grillo", dir))
			if err != nil && !os.IsNotExist(err) {
				t.Error(err)
				ok = false
			}
			if len(entries) != 0 {
				t.Error("backend resources retained", dir)
				ok = false
			}
		}
		if entries, err := os.ReadDir("/proc"); err == nil {
			for _, entry := range entries {
				pid, err := strconv.Atoi(entry.Name())
				if err != nil || pid == os.Getpid() {
					continue
				}
				file, err := os.Open(filepath.Join("/proc", entry.Name(), "cmdline"))
				if err != nil {
					continue
				}
				data, _ := io.ReadAll(io.LimitReader(file, 32<<10))
				file.Close()
				if bytes.Contains(data, []byte(work)) {
					if identity, err := linux.Identify(pid); err == nil && identity.Alive() {
						t.Error("operation-owned host process retained", pid)
						ok = false
					}
				}
			}
		} else {
			t.Error("cannot inspect operation-owned process cleanup")
			ok = false
		}
		if !cleanupOK || !ok {
			t.Log("Gate failed; operation-owned evidence retained for review")
			return
		}
		_ = filepath.WalkDir(prefix, func(path string, entry os.DirEntry, err error) error {
			if err == nil && entry.IsDir() {
				return os.Chmod(path, 0700)
			}
			return err
		})
		if err := os.RemoveAll(work); err != nil {
			t.Error(err)
		}
	})
	cli := filepath.Join(prefix, "bin/grillo")
	run := func(args ...string) []byte {
		t.Helper()
		commandCtx, stop := context.WithTimeout(ctx, 120*time.Second)
		defer stop()
		command := exec.CommandContext(commandCtx, cli, args...)
		command.Env = env
		command.Dir = cwd
		var output installedOutput
		command.Stdout = &output
		command.Stderr = &output
		if err := command.Run(); err != nil {
			t.Fatalf("installed CLI %s: %v\n%s", args[0], err, output.String())
		}
		return output.Bytes()
	}
	run("version", "--json")
	run("doctor", "--json")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	env = append(env, "GRILLO_DEMO_PORT="+strconv.Itoa(port))
	compose := filepath.Join(prefix, "share/grillo/examples/hello-compose/compose.yaml")
	run("plan", compose)
	run("up", compose) // actual on-demand installed daemon startup
	daemon, err = installedDaemonIdentity(socket)
	if err != nil {
		t.Fatal(err)
	}
	if daemon.Executable != filepath.Join(prefix, "libexec/grillo/grillod") {
		t.Fatal("daemon resolved outside installed prefix")
	}
	captureVM := func(application string) {
		t.Helper()
		view, err := client.View(ctx, application)
		if err != nil || len(view.Sandboxes) != 1 || view.Sandboxes[0].VMM == nil {
			t.Fatal("real sandbox metrics unavailable", err)
		}
		identity, err := linux.Identify(view.Sandboxes[0].VMM.PID)
		if err != nil {
			t.Fatal(err)
		}
		vmIdentities = append(vmIdentities, identity)
	}
	captureVM("hello-compose")
	probeInstalledHTTP(t, ctx, "http://127.0.0.1:"+strconv.Itoa(port)+"/", "grillo-compose-ok")
	probeInstalledUI(t, ctx, cli, cwd, env)
	probeInstalledHTTP(t, ctx, "http://127.0.0.1:"+strconv.Itoa(port)+"/", "grillo-compose-ok")
	run("ps")
	run("inspect", "hello-compose")
	run("status", "hello-compose", "--output=json")
	// CLI completion already occurred. A new process still reaches the running VM.
	run("exec", "hello-compose", "web", "--", "/bin/wget", "-qO-", "http://127.0.0.1:8080/")
	run("down", "hello-compose")
	chart := filepath.Join(prefix, "share/grillo/examples/hello-helm")
	run("plan", chart, "--release", "hello-helm")
	run("up", chart, "--release", "hello-helm")
	captureVM("hello-helm")
	var report struct {
		Routes []api.RouteStatus `json:"routes"`
	}
	if err := json.Unmarshal(run("inspect", "hello-helm"), &report); err != nil || len(report.Routes) != 1 {
		t.Fatal("missing actual Helm route", err)
	}
	endpoint := report.Routes[0].Endpoint
	if !strings.HasPrefix(endpoint, "http://127.0.0.1:") {
		t.Fatal("Helm route is not loopback HTTP")
	}
	probeInstalledHTTP(t, ctx, endpoint, "grillo-helm-ok")
	run("exec", "hello-helm", "web", "--", "/bin/wget", "-qO-", "http://hello-helm-web:80/")
	run("down", "hello-helm")
	cleanupOK = true
	t.Log("PASS: runtime base scan, moved/read-only prefix, no build tools on runtime PATH, installed CLI/daemon/helper/Helm, real Compose and Helm HTTP/DNS, CLI/UI-independent lifetime and embedded UI without Node, teardown checked by cleanup")
}

type installedOutput struct{ buffer bytes.Buffer }

func (b *installedOutput) String() string { return b.buffer.String() }
func (b *installedOutput) Bytes() []byte  { return b.buffer.Bytes() }

func (b *installedOutput) Write(data []byte) (int, error) {
	if b.buffer.Len()+len(data) > 128<<10 {
		return 0, fmt.Errorf("installed CLI output limit")
	}
	return b.buffer.Write(data)
}

func probeInstalledUI(t *testing.T, ctx context.Context, binary, cwd string, env []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "ui", "--port", "0")
	command.Env = env
	command.Dir = cwd
	command.Stderr = io.Discard
	pipe, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer func() { cancel(); <-done }()
	// The bootstrap fragment is a credential: never include this line in logs.
	line, err := bufio.NewReader(io.LimitReader(pipe, 4097)).ReadString('\n')
	if err != nil {
		t.Fatal("installed UI startup failed; output withheld")
	}
	endpoint, err := url.Parse(strings.TrimSpace(strings.TrimPrefix(line, "Grillo UI: ")))
	if err != nil || endpoint.Scheme != "http" || endpoint.Hostname() != "127.0.0.1" {
		t.Fatal("invalid installed UI loopback address; output withheld")
	}
	endpoint.Fragment = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		t.Fatal("invalid UI request")
	}
	response, err := (&http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("unexpected UI redirect") }}).Do(request)
	if err != nil {
		t.Fatal("installed UI HTTP unavailable")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 128<<10))
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), "/assets/") {
		t.Fatal("embedded UI entrypoint unavailable")
	}
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal("cannot stop owned UI")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("owned UI shutdown failed")
		}
		done <- nil
	case <-ctx.Done():
		t.Fatal("owned UI shutdown timeout")
	}
}

func installedDaemonIdentity(socket string) (linux.ProcessID, error) {
	connection, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		return linux.ProcessID{}, err
	}
	defer connection.Close()
	raw, err := connection.(*net.UnixConn).SyscallConn()
	if err != nil {
		return linux.ProcessID{}, err
	}
	var credentials *unix.Ucred
	var credentialErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, credentialErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return linux.ProcessID{}, err
	}
	if credentialErr != nil || credentials == nil || credentials.Uid != uint32(os.Geteuid()) {
		return linux.ProcessID{}, fmt.Errorf("invalid owned daemon peer")
	}
	return linux.Identify(int(credentials.Pid))
}
func waitInstalledOperation(ctx context.Context, client *api.Client, id string) error {
	for {
		operation, err := client.Operation(ctx, id)
		if err != nil {
			return err
		}
		if operation.State == "succeeded" {
			return nil
		}
		if operation.State != "running" {
			return fmt.Errorf("operation %s failed", id)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
func probeInstalledHTTP(t *testing.T, ctx context.Context, endpoint, marker string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	client := http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("unexpected workload redirect") }}
	for time.Now().Before(deadline) && ctx.Err() == nil {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err == nil {
			data, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
			response.Body.Close()
			if readErr == nil && response.StatusCode == 200 && strings.TrimSpace(string(data)) == marker {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("real installed HTTP endpoint failed")
}
