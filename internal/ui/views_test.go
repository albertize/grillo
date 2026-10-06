//go:build linux

// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/observe"
)

type richCore struct {
	fakeCore
	mu                  sync.Mutex
	since               uint64
	resource, container string
	args                []string
	fail, block, large  bool
	canceled            chan struct{}
}

func fixtureView() observe.ApplicationView {
	return observe.ApplicationView{Application: "backend", Time: time.Now(), SourceKind: "helm", SourcePath: "example/chart", Workloads: []observe.WorkloadView{{ID: "api", Kind: "Deployment", Replicas: 1, Containers: []observe.ContainerView{{Name: "app", Image: "fixture:local", Mounts: []observe.MountView{{Volume: "data", Path: "/data"}}, ReadinessProbe: true}}}}, Sandboxes: []observe.SandboxView{{ID: "backend-api-0", Workload: "api", Backend: "QEMU microvm", State: "running", Ready: true, GuestBudgetBytes: 512 << 20, VCPU: 1, Containers: []observe.ContainerObservation{{Name: "app", State: "running"}}}}, Services: []observe.ServiceView{{Name: "api", Workloads: []string{"api"}}}, Routes: []observe.RouteView{{Hostname: "demo.local", Path: "/", Service: "api", Endpoint: "http://127.0.0.1:1234"}}, Volumes: []observe.VolumeView{{Name: "data", Kind: "bind"}}, Configs: []observe.MetadataView{{Name: "settings", EntryCount: 1}}, Secrets: []observe.MetadataView{{Name: "token"}}, Diagnostics: []observe.DiagnosticView{{Code: "source_diagnostics_unavailable", Message: "Run grillo plan on source."}}}
}
func (c *richCore) View(context.Context, string) (observe.ApplicationView, error) {
	if c.fail {
		return observe.ApplicationView{}, errors.New("private-backend-detail")
	}
	return fixtureView(), nil
}
func (c *richCore) Exec(ctx context.Context, app, container string, args []string) (int, string, string, error) {
	c.mu.Lock()
	c.args = append([]string{}, args...)
	c.mu.Unlock()
	if _, ok := ctx.Deadline(); !ok {
		return 0, "", "", errors.New("missing deadline")
	}
	if c.block {
		<-ctx.Done()
		close(c.canceled)
		return 0, "", "", ctx.Err()
	}
	if c.fail {
		return 0, "", "", errors.New("private-backend-detail")
	}
	if c.large {
		return 0, strings.Repeat("x", 1<<20), strings.Repeat("y", 1<<20), nil
	}
	return 7, "<img src=x onerror=window.pwned=true>", "stderr-marker", nil
}
func (c *richCore) ListLogs(_ context.Context, since uint64, resource, container string) ([]observe.LogRecord, error) {
	c.mu.Lock()
	c.since, c.resource, c.container = since, resource, container
	c.mu.Unlock()
	if c.fail {
		return nil, errors.New("private-backend-detail")
	}
	if since > 0 || resource != "" && resource != "backend-api-0" || container != "" && container != "app" {
		return nil, nil
	}
	return []observe.LogRecord{{Sequence: 1, Resource: "backend-api-0", Container: "app", Stream: "stdout", Line: "<img src=x onerror=window.pwned=true>"}, {Sequence: 2, Resource: "backend-api-0", Container: "app", Stream: "stderr", Line: "log-stderr-marker"}}, nil
}
func (c *richCore) Events(ctx context.Context, since uint64) (io.ReadCloser, error) {
	c.mu.Lock()
	c.since = since
	c.mu.Unlock()
	if c.fail {
		return nil, errors.New("private-backend-detail")
	}
	if since > 0 {
		return io.NopCloser(strings.NewReader("id: 3\nevent: sandbox.running\ndata: {\"seq\":3,\"kind\":\"sandbox.running\"}\n\n")), nil
	}
	return io.NopCloser(strings.NewReader("id: 1\nevent: events.gap\ndata: {\"seq\":1,\"kind\":\"events.gap\"}\n\nid: 2\nevent: sandbox.running\ndata: {\"seq\":2,\"kind\":\"sandbox.running\",\"resource\":\"backend-api-0\"}\n\n")), nil
}
func authorized(b *Bridge, method, path, body string) *http.Request {
	r := httptest.NewRequest(method, "http://127.0.0.1:9090"+path, strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: b.session})
	r.Header.Set("Origin", "http://127.0.0.1:9090")
	r.Header.Set("Content-Type", "application/json")
	return r
}
func TestViewsExecFiltersAndCursor(t *testing.T) {
	c := &richCore{}
	b := New(c)
	handler := b.Handler()
	for _, path := range []string{"/v1/applications/backend/view", "/v1/logs", "/v1/events"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "http://127.0.0.1:9090"+path, nil)
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("unprotected %s", path)
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, authorized(b, "GET", "/v1/applications/backend/view", ""))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "guestBudgetBytes") || strings.Contains(w.Body.String(), "private-backend-detail") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, authorized(b, "GET", "/v1/logs?since=42&resource=pod&container=web", ""))
	if c.since != 42 || c.resource != "pod" || c.container != "web" {
		t.Fatal("lost log filters")
	}
	w = httptest.NewRecorder()
	r := authorized(b, "GET", "/v1/events?since=2", "")
	r.Header.Set("Last-Event-ID", "99")
	handler.ServeHTTP(w, r)
	if c.since != 99 || !strings.Contains(w.Body.String(), "id: 3") || !strings.Contains(w.Body.String(), "event: message") {
		t.Fatal("lost SSE cursor/frame", w.Body.String())
	}
	for _, path := range []string{"/v1/events?since=invalid", "/v1/logs?since=-1"} {
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, authorized(b, "GET", path, ""))
		if w.Code != 400 {
			t.Fatal("invalid cursor accepted")
		}
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, authorized(b, "POST", "/v1/exec", `{"application":"backend","container":"backend-api-0/app","args":["echo","literal;not-a-host-shell"]}`))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"exitCode":7`) || len(c.args) != 2 || c.args[1] != "literal;not-a-host-shell" {
		t.Fatal("exec contract", w.Code, w.Body.String())
	}
	for _, mutate := range []func(*http.Request){func(r *http.Request) { r.Header.Del("Origin") }, func(r *http.Request) { r.Header.Set("Origin", "http://127.0.0.1:9091") }, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }} {
		w = httptest.NewRecorder()
		r = authorized(b, "POST", "/v1/exec", `{"application":"backend","container":"app","args":["id"]}`)
		mutate(r)
		handler.ServeHTTP(w, r)
		if w.Code < 400 {
			t.Fatal("unsafe exec accepted")
		}
	}
	for _, body := range []string{`{}`, `{"application":"backend","container":"app","args":[]}`, `{"application":"backend","container":"app","args":["id"],"extra":1}`, strings.Repeat("x", 33<<10)} {
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, authorized(b, "POST", "/v1/exec", body))
		if w.Code != 400 {
			t.Fatal("invalid exec accepted", w.Code)
		}
	}
	c.fail = true
	for _, path := range []string{"/v1/applications/backend/view", "/v1/logs", "/v1/events"} {
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, authorized(b, "GET", path, ""))
		if w.Code < 400 || strings.Contains(w.Body.String(), "private-backend-detail") {
			t.Fatal("failure leaked or hidden")
		}
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, authorized(b, "POST", "/v1/exec", `{"application":"backend","container":"app","args":["id"]}`))
	if w.Code != 502 || strings.Contains(w.Body.String(), "private-backend-detail") {
		t.Fatal("exec failure leaked")
	}
}
func TestExecOutputBound(t *testing.T) {
	b := New(&richCore{large: true})
	w := httptest.NewRecorder()
	b.Handler().ServeHTTP(w, authorized(b, "POST", "/v1/exec", `{"application":"backend","container":"app","args":["id"]}`))
	var out struct {
		Stdout    string
		Stderr    string
		Truncated bool
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Stdout)+len(out.Stderr) != 1<<20 || !out.Truncated {
		t.Fatal("output limit not enforced")
	}
}

func TestExecCancellationAndLimits(t *testing.T) {
	c := &richCore{block: true, canceled: make(chan struct{})}
	b := New(c)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := authorized(b, "POST", "/v1/exec", `{"application":"backend","container":"app","args":["sleep","300"]}`).WithContext(ctx)
	b.Handler().ServeHTTP(httptest.NewRecorder(), r)
	select {
	case <-c.canceled:
	case <-time.After(time.Second):
		t.Fatal("exec cancellation not propagated")
	}
	for range cap(b.execs) {
		b.execs <- struct{}{}
	}
	w := httptest.NewRecorder()
	b.Handler().ServeHTTP(w, authorized(b, "POST", "/v1/exec", `{}`))
	if w.Code != 429 {
		t.Fatal("exec slots unbounded")
	}
	for range cap(b.streams) {
		b.streams <- struct{}{}
	}
	w = httptest.NewRecorder()
	b.Handler().ServeHTTP(w, authorized(b, "GET", "/v1/events", ""))
	if w.Code != 429 {
		t.Fatal("stream slots unbounded")
	}
}
