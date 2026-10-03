//go:build linux

// SPDX-License-Identifier: Apache-2.0

package observe

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type manualClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

type scriptedProber struct {
	mu      sync.Mutex
	clock   Clock
	healthy bool
	block   time.Duration
	calls   int
}

func (p *scriptedProber) setHealthy(healthy bool) {
	p.mu.Lock()
	p.healthy = healthy
	p.mu.Unlock()
}

func (p *scriptedProber) Probe(ctx context.Context, _ Probe) (bool, string) {
	p.mu.Lock()
	p.calls++
	healthy, block := p.healthy, p.block
	p.mu.Unlock()
	if block > 0 {
		select {
		case <-time.After(block):
		case <-ctx.Done():
			return false, "timeout"
		}
	}
	if healthy {
		return true, ""
	}
	return false, "unhealthy"
}

func execProbe() Probe {
	return Probe{Kind: ProbeExec, Command: []string{"/bin/true"}, Period: 5 * time.Second}
}

func TestRunnerStartupGatesReadinessAndLiveness(t *testing.T) {
	clock := &manualClock{now: time.Unix(0, 0)}
	prober := &scriptedProber{clock: clock, healthy: true}
	runner := NewRunner(clock, prober, nil)
	readiness := execProbe()
	liveness := execProbe()
	if err := runner.Set("app", ProbeConfig{Startup: &Probe{Kind: ProbeExec, Command: []string{"/bin/true"}, Period: 10 * time.Second}, Readiness: &readiness, Liveness: &liveness}); err != nil {
		t.Fatal(err)
	}
	runner.Tick(context.Background())
	status, _ := runner.Status("app")
	if !status.StartupDone || status.Ready || status.Live {
		t.Fatalf("after first tick: %+v", status)
	}
	runner.Tick(context.Background())
	status, _ = runner.Status("app")
	if !status.Ready || !status.Live {
		t.Fatalf("readiness/liveness did not run after startup: %+v", status)
	}
}

func TestRunnerThresholdsAndCallbacks(t *testing.T) {
	clock := &manualClock{now: time.Unix(0, 0)}
	prober := &scriptedProber{clock: clock, healthy: true}
	runner := NewRunner(clock, prober, nil)
	var livenessFailures int
	runner.OnLivenessFailure = func(string) { livenessFailures++ }
	readiness := execProbe()
	readiness.Period = 10 * time.Second
	readiness.FailureThreshold = 2
	liveness := execProbe()
	liveness.Period = 10 * time.Second
	liveness.FailureThreshold = 2
	if err := runner.Set("app", ProbeConfig{Readiness: &readiness, Liveness: &liveness}); err != nil {
		t.Fatal(err)
	}
	runner.Tick(context.Background())
	if status, _ := runner.Status("app"); !status.Ready || !status.Live {
		t.Fatalf("expected ready and live: %+v", status)
	}
	prober.setHealthy(false)
	clock.advance(10 * time.Second)
	runner.Tick(context.Background())
	if status, _ := runner.Status("app"); !status.Ready || !status.Live {
		t.Fatalf("single failure should not flip below threshold: %+v", status)
	}
	clock.advance(10 * time.Second)
	runner.Tick(context.Background())
	status, _ := runner.Status("app")
	if status.Ready || status.Live {
		t.Fatalf("expected not ready and not live: %+v", status)
	}
	if livenessFailures != 1 {
		t.Fatalf("liveness failure callback count = %d", livenessFailures)
	}
}

func TestRunnerStartupFailure(t *testing.T) {
	clock := &manualClock{now: time.Unix(0, 0)}
	prober := &scriptedProber{clock: clock, healthy: false}
	var events []Event
	runner := NewRunner(clock, prober, func(e Event) { events = append(events, e) })
	startup := execProbe()
	startup.Period = time.Second
	startup.FailureThreshold = 2
	if err := runner.Set("app", ProbeConfig{Startup: &startup}); err != nil {
		t.Fatal(err)
	}
	runner.Tick(context.Background())
	clock.advance(time.Second)
	runner.Tick(context.Background())
	status, _ := runner.Status("app")
	if !status.Failed {
		t.Fatalf("expected startup failure: %+v", status)
	}
	if len(events) == 0 || !strings.Contains(events[len(events)-1].Kind, "probe") {
		t.Fatalf("events = %+v", events)
	}
}

func TestRunnerTickDoesNotOverlap(t *testing.T) {
	clock := &manualClock{now: time.Unix(0, 0)}
	prober := &scriptedProber{clock: clock, healthy: true, block: 20 * time.Millisecond}
	runner := NewRunner(clock, prober, nil)
	readiness := execProbe()
	readiness.Period = time.Nanosecond // always due
	if err := runner.Set("app", ProbeConfig{Readiness: &readiness}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		clock.advance(time.Millisecond)
		runner.Tick(context.Background())
	}
	prober.mu.Lock()
	calls := prober.calls
	prober.mu.Unlock()
	if calls != 3 {
		t.Fatalf("probe calls = %d, want 3 (one per tick, no overlap)", calls)
	}
}

func TestDirectProberExecTimeoutKillsChild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	prober := DirectProber{}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	healthy, detail := prober.Probe(ctx, Probe{Kind: ProbeExec, Command: []string{"/bin/sh", "-c", "echo $$ > " + pidFile + "; exec sleep 30"}})
	if healthy {
		t.Fatalf("expected timeout, detail=%q", detail)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	time.Sleep(100 * time.Millisecond)
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("probe child %d survived the timeout", pid)
	}
}

func TestDirectProberHTTPAndTCP(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer ok.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	defer bad.Close()
	prober := DirectProber{}
	if healthy, detail := prober.Probe(context.Background(), Probe{Kind: ProbeHTTP, URL: ok.URL}); !healthy {
		t.Fatalf("healthy endpoint: %q", detail)
	}
	if healthy, _ := prober.Probe(context.Background(), Probe{Kind: ProbeHTTP, URL: bad.URL}); healthy {
		t.Fatal("500 endpoint reported healthy")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	host, portStr, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portStr)
	if healthy, detail := prober.Probe(context.Background(), Probe{Kind: ProbeTCP, Host: host, Port: port}); !healthy {
		t.Fatalf("open port: %q", detail)
	}
	if healthy, _ := prober.Probe(context.Background(), Probe{Kind: ProbeTCP, Host: host, Port: 1}); healthy {
		t.Fatal("closed port reported healthy")
	}
}

func TestDirectProberExecSuccessAndFailure(t *testing.T) {
	prober := DirectProber{}
	if healthy, detail := prober.Probe(context.Background(), Probe{Kind: ProbeExec, Command: []string{"/bin/true"}}); !healthy {
		t.Fatalf("true: %q", detail)
	}
	if healthy, _ := prober.Probe(context.Background(), Probe{Kind: ProbeExec, Command: []string{"/bin/false"}}); healthy {
		t.Fatal("false reported healthy")
	}
}
