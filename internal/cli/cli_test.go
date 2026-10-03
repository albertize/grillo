//go:build linux

// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grillo.local/grillo/internal/api"
	"grillo.local/grillo/internal/model"
	"grillo.local/grillo/internal/observe"
)

type fakeClient struct {
	applied    model.Application
	downCalled bool
	operation  api.Operation
	execArgs   []string
}

func (f *fakeClient) Version(context.Context) (api.VersionInfo, error) { return api.VersionInfo{}, nil }
func (f *fakeClient) Health(context.Context) (api.Health, error) {
	return api.Health{Status: "ok"}, nil
}
func (f *fakeClient) Apply(_ context.Context, app model.Application) (string, error) {
	f.applied = app
	return "op-1", nil
}
func (f *fakeClient) Down(_ context.Context, _ string, _ bool) (string, error) {
	f.downCalled = true
	return "op-2", nil
}
func (f *fakeClient) Operation(context.Context, string) (api.Operation, error) {
	return f.operation, nil
}
func (f *fakeClient) Applications(context.Context) ([]string, error) {
	return []string{"backend"}, nil
}
func (f *fakeClient) Restart(context.Context, string) (string, error) { return "op-r", nil }
func (f *fakeClient) Status(context.Context, string) ([]api.ContainerStatus, error) {
	return []api.ContainerStatus{{Container: "app", State: "running"}}, nil
}
func (f *fakeClient) ExecStream(_ context.Context, _, _ string, args []string, stdout, _ io.Writer) (int, error) {
	f.execArgs = args
	_, _ = io.WriteString(stdout, "out:"+strings.Join(args, " "))
	return 3, nil
}
func (f *fakeClient) Events(context.Context, uint64) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (f *fakeClient) ListLogs(context.Context, uint64, string, string) ([]observe.LogRecord, error) {
	return nil, nil
}
func (f *fakeClient) FollowLogs(context.Context, uint64, string, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

type fakeTerminal struct {
	tty      bool
	raw      int
	restored int
}

func (f *fakeTerminal) IsTerminal() bool { return f.tty }
func (f *fakeTerminal) MakeRaw() (func(), error) {
	f.raw++
	return func() { f.restored++ }, nil
}

func nativeManifest(t *testing.T) string {
	t.Helper()
	app := model.Application{
		APIVersion: model.APIVersion,
		Kind:       model.KindApplication,
		Identity:   model.Identity{Name: "backend", Namespace: "default"},
		Workloads: []model.Workload{{
			ID:       "app",
			Kind:     model.WorkloadDeployment,
			Replicas: 1,
			Template: model.SandboxTemplate{Containers: []model.Container{{Name: "api", Image: model.ImageRef{Reference: "busybox:1.37"}}}},
		}},
	}
	data, err := model.MarshalManifest(app)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "app.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newTestApp(t *testing.T, client Client, terminal Terminal) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	return &App{
		Stdin:          bytes.NewReader(nil),
		Stdout:         stdout,
		Stderr:         stderr,
		Terminal:       terminal,
		ClientFactory:  func(string) Client { return client },
		Ensure:         func(context.Context, string, string, time.Duration) error { return nil },
		DaemonPath:     "/bin/true",
		StartupTimeout: time.Second,
	}, stdout, stderr
}

func TestPlanIsOfflineAndListsActions(t *testing.T) {
	app, stdout, _ := newTestApp(t, &fakeClient{}, &fakeTerminal{})
	manifest := nativeManifest(t)
	if code := app.Run(context.Background(), []string{"plan", manifest}); code != 0 {
		t.Fatalf("plan exit = %d", code)
	}
	if !strings.Contains(stdout.String(), "CreateSandbox") {
		t.Fatalf("plan output = %q", stdout.String())
	}
}

func TestPlanRejectsInvalidManifest(t *testing.T) {
	app, _, stderr := newTestApp(t, &fakeClient{}, &fakeTerminal{})
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := app.Run(context.Background(), []string{"plan", path}); code == 0 {
		t.Fatalf("plan accepted invalid manifest; stderr=%q", stderr.String())
	}
}

func TestUpAppliesAndWaits(t *testing.T) {
	client := &fakeClient{operation: api.Operation{State: "succeeded", Kind: "apply"}}
	app, stdout, _ := newTestApp(t, client, &fakeTerminal{})
	if code := app.Run(context.Background(), []string{"up", nativeManifest(t)}); code != 0 {
		t.Fatalf("up exit = %d (%s)", code, stdout.String())
	}
	if client.applied.Identity.Name != "backend" {
		t.Fatalf("apply did not receive the application: %+v", client.applied)
	}
}

func TestDownAppliesVolumesFlag(t *testing.T) {
	client := &fakeClient{operation: api.Operation{State: "succeeded", Kind: "down"}}
	app, _, _ := newTestApp(t, client, &fakeTerminal{})
	if code := app.Run(context.Background(), []string{"down", "--volumes", "backend"}); code != 0 {
		t.Fatalf("down exit = %d", code)
	}
	if !client.downCalled {
		t.Fatal("down did not call the API")
	}
}

func TestExecRunsAndRestoresTerminal(t *testing.T) {
	terminal := &fakeTerminal{tty: true}
	client := &fakeClient{}
	app, stdout, _ := newTestApp(t, client, terminal)
	code := app.Run(context.Background(), []string{"exec", "pod/backend-0", "api", "--", "sh", "-c", "echo hi"})
	if code != 3 {
		t.Fatalf("exec exit = %d, want the container exit code 3", code)
	}
	if terminal.raw != 1 || terminal.restored != 1 {
		t.Fatalf("raw=%d restored=%d, want 1/1", terminal.raw, terminal.restored)
	}
	if len(client.execArgs) != 3 || !strings.Contains(stdout.String(), "echo hi") {
		t.Fatalf("exec args=%v stdout=%q", client.execArgs, stdout.String())
	}
}

func TestStatusCommand(t *testing.T) {
	app, stdout, _ := newTestApp(t, &fakeClient{}, &fakeTerminal{})
	if code := app.Run(context.Background(), []string{"status", "backend"}); code != 0 {
		t.Fatalf("status exit = %d", code)
	}
	if !strings.Contains(stdout.String(), "backend/app") {
		t.Fatalf("status output = %q", stdout.String())
	}
	if code := app.Run(context.Background(), []string{"inspect", "application/backend"}); code != 0 {
		t.Fatalf("inspect exit = %d", code)
	}
}

func TestExecRequiresCommand(t *testing.T) {
	app, _, _ := newTestApp(t, &fakeClient{}, &fakeTerminal{})
	if code := app.Run(context.Background(), []string{"exec", "pod/backend-0"}); code != 2 {
		t.Fatalf("exit = %d, want usage error", code)
	}
}

func TestDoctorExitCodes(t *testing.T) {
	ok := &App{Stdout: &bytes.Buffer{}, Doctor: func() []Check { return []Check{{Name: "kvm", Status: "ok"}} }}
	if code := ok.Run(context.Background(), []string{"doctor"}); code != 0 {
		t.Fatalf("doctor exit = %d", code)
	}
	bad := &App{Stdout: &bytes.Buffer{}, Doctor: func() []Check { return []Check{{Name: "kvm", Status: "fail", Detail: "missing"}} }}
	if code := bad.Run(context.Background(), []string{"doctor"}); code != 1 {
		t.Fatalf("failing doctor exit = %d, want 1", code)
	}
}

func TestVersionAndUnknownCommand(t *testing.T) {
	app, stdout, _ := newTestApp(t, &fakeClient{}, &fakeTerminal{})
	app.Version = "9.9.9"
	if code := app.Run(context.Background(), []string{"version"}); code != 0 || !strings.Contains(stdout.String(), "9.9.9") {
		t.Fatalf("version = %q code=%d", stdout.String(), code)
	}
	if code := app.Run(context.Background(), []string{"frobnicate"}); code != 2 {
		t.Fatalf("unknown command exit = %d, want 2", code)
	}
}

func TestDoctorChecksAreWellFormed(t *testing.T) {
	checks := DoctorChecks()
	if len(checks) == 0 {
		t.Fatal("no doctor checks")
	}
	for _, check := range checks {
		switch check.Status {
		case "ok", "warn", "fail":
		default:
			t.Fatalf("check %q has invalid status %q", check.Name, check.Status)
		}
	}
}

func TestPsAndRestartCommands(t *testing.T) {
	client := &fakeClient{operation: api.Operation{State: "succeeded", Kind: "restart"}}
	app, stdout, _ := newTestApp(t, client, &fakeTerminal{})
	if code := app.Run(context.Background(), []string{"ps"}); code != 0 {
		t.Fatalf("ps exit = %d", code)
	}
	if !strings.Contains(stdout.String(), "backend") {
		t.Fatalf("ps output = %q", stdout.String())
	}
	if code := app.Run(context.Background(), []string{"restart", "backend"}); code != 0 {
		t.Fatalf("restart exit = %d", code)
	}
}

func TestUICommandStartsAndStops(t *testing.T) {
	app, stdout, _ := newTestApp(t, &fakeClient{}, &fakeTerminal{})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	code := app.Run(ctx, []string{"ui", "--port", "0"})
	if code != 0 {
		t.Fatalf("ui exit = %d", code)
	}
	if !strings.Contains(stdout.String(), "Grillo UI") {
		t.Fatalf("ui output = %q", stdout.String())
	}
}

func TestPlanComposeInput(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(composePath, []byte("name: demo\nservices:\n  web:\n    image: busybox:${TAG:-1.37}\n    environment:\n      EMPTY: \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app, stdout, stderr := newTestApp(t, &fakeClient{}, &fakeTerminal{})
	if code := app.Run(context.Background(), []string{"plan", composePath}); code != 0 {
		t.Fatalf("plan compose exit = %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "CreateSandbox") {
		t.Fatalf("plan output = %q", stdout.String())
	}
}

func TestPlanRejectsUnsupportedComposeField(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(composePath, []byte("services:\n  web:\n    image: busybox\n    privileged: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app, _, stderr := newTestApp(t, &fakeClient{}, &fakeTerminal{})
	if code := app.Run(context.Background(), []string{"plan", composePath}); code == 0 {
		t.Fatalf("unsupported field accepted: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "privileged") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
