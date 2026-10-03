//go:build linux

// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bufio"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"grillo.local/grillo/internal/model"
	"grillo.local/grillo/internal/observe"
	"grillo.local/grillo/internal/reconcile"
)

type fakeCore struct {
	mu      sync.Mutex
	applied int
	block   chan struct{}
	err     error
}

func (f *fakeCore) Apply(ctx context.Context, app model.Application) (reconcile.Result, error) {
	f.mu.Lock()
	f.applied++
	block := f.block
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return reconcile.Result{}, ctx.Err()
		}
	}
	return reconcile.Result{Application: app.Identity.Name}, f.err
}

func (f *fakeCore) Down(_ context.Context, application string, _ bool) (reconcile.Result, error) {
	return reconcile.Result{Application: application}, f.err
}

func startServer(t *testing.T, opts Options) (string, func()) {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "grillod.sock")
	server := NewServer(opts)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, socket) }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return socket, func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
	}
}

func testApp(name string) model.Application {
	return model.Application{
		APIVersion: model.APIVersion,
		Kind:       model.KindApplication,
		Identity:   model.Identity{Name: name, Namespace: "default"},
	}
}

func TestVersionAndHealth(t *testing.T) {
	socket, stop := startServer(t, Options{Core: &fakeCore{}, Version: "1.2.3"})
	defer stop()
	client := NewClient(socket)
	version, err := client.Version(context.Background())
	if err != nil || version.Version != "1.2.3" || version.APIVersion != "v1" {
		t.Fatalf("version = %+v err=%v", version, err)
	}
	if _, err := client.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func rawPost(t *testing.T, socket, path, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://localhost"+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := NewClient(socket).httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestMalformedRequestDoesNotCrash(t *testing.T) {
	socket, stop := startServer(t, Options{Core: &fakeCore{}, Version: "1"})
	defer stop()
	resp := rawPost(t, socket, "/v1/applications", "{not json")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	if _, err := NewClient(socket).Health(context.Background()); err != nil {
		t.Fatalf("server did not survive a malformed request: %v", err)
	}
}

func TestBoundedRequestBody(t *testing.T) {
	socket, stop := startServer(t, Options{Core: &fakeCore{}, MaxBodyBytes: 32})
	defer stop()
	body := `{"apiVersion":"grillo.dev/v1alpha1","kind":"Application","metadata":{"name":"` + strings.Repeat("x", 200) + `","namespace":"default"}}`
	resp := rawPost(t, socket, "/v1/applications", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestApplySurvivesClientDisconnect(t *testing.T) {
	block := make(chan struct{})
	core := &fakeCore{block: block}
	socket, stop := startServer(t, Options{Core: core})
	defer stop()
	client := NewClient(socket)
	ctx, cancel := context.WithCancel(context.Background())
	id, err := client.Apply(ctx, testApp("backend"))
	if err != nil {
		t.Fatal(err)
	}
	cancel() // client disconnects immediately
	close(block)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		operation, err := client.Operation(context.Background(), id)
		if err == nil && operation.State == "succeeded" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("accepted apply was canceled by the disconnected client")
}

func TestExplicitCancel(t *testing.T) {
	block := make(chan struct{})
	core := &fakeCore{block: block}
	socket, stop := startServer(t, Options{Core: core})
	defer stop()
	client := NewClient(socket)
	id, err := client.Apply(context.Background(), testApp("backend"))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Cancel(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		operation, err := client.Operation(context.Background(), id)
		if err == nil && operation.State == "canceled" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("operation was not canceled")
}

func TestUnauthorizedPeerRejected(t *testing.T) {
	socket, stop := startServer(t, Options{Core: &fakeCore{}, PeerUID: uint32(os.Getuid()) + 1000})
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := NewClient(socket).Health(ctx); err == nil {
		t.Fatal("a connection from an unexpected UID was accepted")
	}
}

func TestEventsStream(t *testing.T) {
	events, err := observe.OpenEvents(filepath.Join(t.TempDir(), "events"), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer events.Close()
	if _, err := events.Emit(observe.Event{Kind: "pod.started", Resource: "pod/app-0"}); err != nil {
		t.Fatal(err)
	}
	socket, stop := startServer(t, Options{Core: &fakeCore{}, Events: events})
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stream, err := NewClient(socket).Events(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "pod.started") {
			return
		}
	}
	t.Fatal("did not receive the event over SSE")
}

func TestConcurrentClients(t *testing.T) {
	socket, stop := startServer(t, Options{Core: &fakeCore{}})
	defer stop()
	client := NewClient(socket)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.Health(context.Background()); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestStreamSessionsDoNotLeak(t *testing.T) {
	events, _ := observe.OpenEvents(filepath.Join(t.TempDir(), "events"), 0, 0)
	defer events.Close()
	socket, stop := startServer(t, Options{Core: &fakeCore{}, Events: events})
	defer stop()
	client := NewClient(socket)
	baseline := runtime.NumGoroutine()
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		stream, err := client.Events(ctx, 0)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		cancel()
		stream.Close()
	}
	time.Sleep(300 * time.Millisecond)
	runtime.GC()
	after := runtime.NumGoroutine()
	if after > baseline+8 {
		t.Fatalf("goroutines grew from %d to %d after closing streams", baseline, after)
	}
}
