//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"bytes"
	"context"
	"io"
	"net"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/albertize/grillo/internal/guestproto"
)

// fakeRuntime records lifecycle calls and returns scripted results. It exercises
// the agent's logic; the real runc path is covered by the KVM integration test,
// not here.
type fakeRuntime struct {
	streamOut  []byte
	mu         sync.Mutex
	calls      []string
	runExit    map[string]int
	appRunning map[string]bool
	execOut    []byte
	execErr    []byte
	execExit   int
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{runExit: map[string]int{}, appRunning: map[string]bool{}}
}

func (f *fakeRuntime) record(s string) {
	f.mu.Lock()
	f.calls = append(f.calls, s)
	f.mu.Unlock()
}

func (f *fakeRuntime) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeRuntime) Run(_ context.Context, name, _ string) (ExitStatus, []byte, []byte, error) {
	f.record("run:" + name)
	f.mu.Lock()
	code := f.runExit[name]
	f.mu.Unlock()
	return ExitStatus{PID: 1, ExitCode: code}, []byte("init-out"), nil, nil
}

func (f *fakeRuntime) RunStreaming(_ context.Context, name, _ string, stdout, _ io.Writer) (ExitStatus, error) {
	f.record("runstream:" + name)
	f.mu.Lock()
	code := f.runExit[name]
	out := f.streamOut
	f.mu.Unlock()
	if stdout != nil && out != nil {
		_, _ = stdout.Write(out)
	}
	return ExitStatus{PID: 1, ExitCode: code}, nil
}

func (f *fakeRuntime) StartDetached(_ context.Context, name, _ string) error {
	f.record("start:" + name)
	f.mu.Lock()
	f.appRunning[name] = true
	f.mu.Unlock()
	return nil
}

func (f *fakeRuntime) Exec(_ context.Context, name string, _ []string) (ExitStatus, []byte, []byte, error) {
	f.record("exec:" + name)
	f.mu.Lock()
	defer f.mu.Unlock()
	return ExitStatus{ExitCode: f.execExit}, f.execOut, f.execErr, nil
}

func (f *fakeRuntime) Kill(_ context.Context, name string, _ syscall.Signal) error {
	f.record("kill:" + name)
	return nil
}

func (f *fakeRuntime) Delete(_ context.Context, name string, _ bool) error {
	f.record("delete:" + name)
	return nil
}

func (f *fakeRuntime) State(_ context.Context, name string) (ContainerState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.appRunning[name] {
		return ContainerState{Status: "running"}, nil
	}
	return ContainerState{Status: "stopped"}, nil
}

func testAgentKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

func testSandbox() guestproto.SandboxSpec {
	return guestproto.SandboxSpec{
		ID:       "sbx-1",
		Hostname: "pod-1",
		Containers: []guestproto.ContainerSpec{
			{Name: "setup", Init: true, Rootfs: "/rootfs/setup", Args: []string{"/bin/setup"}},
			{Name: "api", Rootfs: "/rootfs/api", Args: []string{"/bin/api"}},
			{Name: "sidecar", Rootfs: "/rootfs/sidecar", Args: []string{"/bin/sidecar"}},
		},
	}
}

func newAgentPair(t *testing.T, rt Runtime) *guestproto.Client {
	t.Helper()
	agent := NewAgent(rt, t.TempDir())
	if err := agent.Prepare(); err != nil {
		t.Fatal(err)
	}
	serverConn, clientConn := net.Pipe()
	cfg := guestproto.HandshakeConfig{Sandbox: "sbx-1", Key: testAgentKey(), Agent: "test"}
	srvCh := make(chan *guestproto.Server, 1)
	go func() {
		s, err := guestproto.NewServer(context.Background(), serverConn, cfg, agent)
		if err != nil {
			return
		}
		srvCh <- s
		_ = s.Serve(context.Background())
	}()
	client, err := guestproto.NewClient(context.Background(), clientConn, cfg)
	if err != nil {
		t.Fatal(err)
	}
	server := <-srvCh
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	return client
}

func sandboxPtr(s guestproto.SandboxSpec) *guestproto.SandboxSpec { return &s }

func TestAgentStartOrder(t *testing.T) {
	rt := newFakeRuntime()
	client := newAgentPair(t, rt)
	result, err := client.Start(context.Background(), guestproto.StartRequest{Sandbox: sandboxPtr(testSandbox())})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "running" || len(result.Containers) != 3 {
		t.Fatalf("start result = %+v", result)
	}
	want := []string{"runstream:setup", "start:api", "start:sidecar"}
	if got := rt.callLog(); !reflect.DeepEqual(got, want) {
		t.Fatalf("call order = %v, want %v", got, want)
	}
}

func TestAgentInitFailureBlocksApps(t *testing.T) {
	rt := newFakeRuntime()
	rt.runExit["setup"] = 1
	client := newAgentPair(t, rt)
	_, err := client.Start(context.Background(), guestproto.StartRequest{Sandbox: sandboxPtr(testSandbox())})
	if err == nil {
		t.Fatal("expected init failure")
	}
	if pe, ok := guestproto.AsError(err); !ok || pe.Code != guestproto.CodeInternal {
		t.Fatalf("err = %v, want internal error", err)
	}
	for _, call := range rt.callLog() {
		if strings.HasPrefix(call, "start:") {
			t.Fatalf("application container started despite init failure: %v", rt.callLog())
		}
	}
}

func TestAgentExecStreams(t *testing.T) {
	rt := newFakeRuntime()
	rt.execOut = []byte("hello")
	rt.execErr = []byte("warn")
	rt.execExit = 7
	client := newAgentPair(t, rt)
	var stdout, stderr bytes.Buffer
	result, err := client.Exec(context.Background(), guestproto.ExecRequest{Container: "api", Args: []string{"echo", "hello"}}, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "hello" || stderr.String() != "warn" || result.ExitCode != 7 {
		t.Fatalf("exec stdout=%q stderr=%q result=%+v", stdout.String(), stderr.String(), result)
	}
}

func TestAgentStatusProbeStop(t *testing.T) {
	rt := newFakeRuntime()
	client := newAgentPair(t, rt)
	ctx := context.Background()
	if _, err := client.Start(ctx, guestproto.StartRequest{Sandbox: sandboxPtr(testSandbox())}); err != nil {
		t.Fatal(err)
	}
	status, err := client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "running" || len(status.Containers) != 3 {
		t.Fatalf("status = %+v", status)
	}
	probe, err := client.Probe(ctx, guestproto.ProbeRequest{Kind: guestproto.ProbeExec, Container: "api", Command: []string{"/bin/true"}})
	if err != nil {
		t.Fatal(err)
	}
	if !probe.Healthy {
		t.Fatalf("probe = %+v", probe)
	}
	stop, err := client.Stop(ctx, guestproto.StopRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !stop.Stopped {
		t.Fatalf("stop = %+v", stop)
	}
}

func TestAgentStartRequiresValidSpec(t *testing.T) {
	client := newAgentPair(t, newFakeRuntime())
	if _, err := client.Start(context.Background(), guestproto.StartRequest{}); err == nil {
		t.Fatal("expected error for missing sandbox spec")
	}
}
