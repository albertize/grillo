//go:build linux

// SPDX-License-Identifier: Apache-2.0

package observe

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// ProbeKind selects a probe implementation.
type ProbeKind string

const (
	ProbeExec ProbeKind = "exec"
	ProbeHTTP ProbeKind = "http"
	ProbeTCP  ProbeKind = "tcp"
)

// Probe is one health check.
type Probe struct {
	Kind      ProbeKind
	Container string
	// Exec
	Command []string
	// HTTP
	URL string
	// TCP
	Host string
	Port int

	InitialDelay     time.Duration
	Period           time.Duration
	Timeout          time.Duration
	SuccessThreshold int
	FailureThreshold int
}

func (p Probe) validate() error {
	switch p.Kind {
	case ProbeExec:
		if len(p.Command) == 0 {
			return fmt.Errorf("observe: exec probe needs a command")
		}
	case ProbeHTTP:
		if p.URL == "" {
			return fmt.Errorf("observe: http probe needs a URL")
		}
	case ProbeTCP:
		if p.Host == "" || p.Port == 0 {
			return fmt.Errorf("observe: tcp probe needs a host and port")
		}
	default:
		return fmt.Errorf("observe: unknown probe kind %q", p.Kind)
	}
	return nil
}

func (p Probe) period() time.Duration {
	if p.Period <= 0 {
		return 10 * time.Second
	}
	return p.Period
}

func (p Probe) timeout() time.Duration {
	if p.Timeout <= 0 {
		return time.Second
	}
	return p.Timeout
}

func (p Probe) successThreshold() int {
	if p.SuccessThreshold <= 0 {
		return 1
	}
	return p.SuccessThreshold
}

func (p Probe) failureThreshold() int {
	if p.FailureThreshold <= 0 {
		return 3
	}
	return p.FailureThreshold
}

// Prober executes a probe. Implementations must respect the context deadline and
// must not leave child processes behind on timeout.
type Prober interface {
	Probe(ctx context.Context, probe Probe) (healthy bool, detail string)
}

// DirectProber runs probes on the host. Exec probes run in their own process
// group so a timeout kills the whole subtree.
type DirectProber struct {
	HTTPClient *http.Client
}

// Probe implements Prober.
func (p DirectProber) Probe(ctx context.Context, probe Probe) (bool, string) {
	switch probe.Kind {
	case ProbeExec:
		return p.exec(ctx, probe.Command)
	case ProbeHTTP:
		return p.http(ctx, probe.URL)
	case ProbeTCP:
		return p.tcp(ctx, probe.Host, probe.Port)
	default:
		return false, "unknown probe kind"
	}
}

func (p DirectProber) exec(ctx context.Context, command []string) (bool, string) {
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		return false, err.Error()
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			return true, ""
		}
		return false, err.Error()
	case <-ctx.Done():
		// Kill the whole process group so children do not survive the probe.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return false, "probe timed out"
	}
}

func (p DirectProber) http(ctx context.Context, url string) (bool, string) {
	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err.Error()
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return true, ""
	}
	return false, "status " + strconv.Itoa(resp.StatusCode)
}

func (p DirectProber) tcp(ctx context.Context, host string, port int) (bool, string) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false, err.Error()
	}
	_ = conn.Close()
	return true, ""
}

// ProbeConfig groups the three probe roles for one container.
type ProbeConfig struct {
	Startup   *Probe
	Readiness *Probe
	Liveness  *Probe
}

// ProbeStatus is the observed health of one container.
type ProbeStatus struct {
	Container         string
	StartupDone       bool
	Ready             bool
	Live              bool
	Failed            bool
	StartupFailures   int
	ReadinessFailures int
	LivenessFailures  int
	LastError         string
	LastRun           time.Time
}

type probeCounter struct {
	success int
	failure int
	lastRun time.Time
}

type probeTarget struct {
	container   string
	cfg         ProbeConfig
	startedAt   time.Time
	startupDone bool
	ready       bool
	live        bool
	failed      bool
	startup     probeCounter
	readiness   probeCounter
	liveness    probeCounter
	lastError   string
	lastRun     time.Time
}

// Runner schedules probes. Tick is synchronous and runs at most one probe per
// container at a time, so runs never overlap; a production loop calls Tick on a
// timer, and tests call Tick directly with an injected clock.
type Runner struct {
	clock  Clock
	prober Prober
	emit   func(Event)
	// OnReadinessChange and OnLivenessFailure are optional callbacks.
	OnReadinessChange func(container string, ready bool)
	OnLivenessFailure func(container string)

	mu      sync.Mutex
	targets map[string]*probeTarget
}

// NewRunner returns a probe runner.
func NewRunner(clock Clock, prober Prober, emit func(Event)) *Runner {
	if clock == nil {
		clock = RealClock{}
	}
	return &Runner{clock: clock, prober: prober, emit: emit, targets: map[string]*probeTarget{}}
}

// Set registers or replaces a container's probes.
func (r *Runner) Set(container string, cfg ProbeConfig) error {
	for _, probe := range []*Probe{cfg.Startup, cfg.Readiness, cfg.Liveness} {
		if probe != nil {
			if err := probe.validate(); err != nil {
				return err
			}
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.targets[container] = &probeTarget{
		container:   container,
		cfg:         cfg,
		startedAt:   r.clock.Now(),
		startupDone: cfg.Startup == nil,
	}
	return nil
}

// Remove stops probing a container.
func (r *Runner) Remove(container string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.targets, container)
}

// Status returns a container's current health.
func (r *Runner) Status(container string) (ProbeStatus, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	target, ok := r.targets[container]
	if !ok {
		return ProbeStatus{}, false
	}
	return ProbeStatus{
		Container:         container,
		StartupDone:       target.startupDone,
		Ready:             target.ready,
		Live:              target.live,
		Failed:            target.failed,
		StartupFailures:   target.startup.failure,
		ReadinessFailures: target.readiness.failure,
		LivenessFailures:  target.liveness.failure,
		LastError:         target.lastError,
		LastRun:           target.lastRun,
	}, true
}

// Tick runs every probe that is due at the current clock time.
func (r *Runner) Tick(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, target := range r.targets {
		r.tick(ctx, target)
	}
}

func (r *Runner) tick(ctx context.Context, target *probeTarget) {
	if target.failed {
		return
	}
	now := r.clock.Now()
	if !target.startupDone {
		probe := target.cfg.Startup
		if !due(target.startedAt, target.startup, probe, now) {
			return
		}
		healthy, detail := r.runProbe(ctx, *probe)
		target.startup.lastRun = now
		target.lastRun = now
		target.lastError = detail
		if healthy {
			target.startup.success++
			if target.startup.success >= probe.successThreshold() {
				target.startupDone = true
			}
		} else {
			target.startup.failure++
			target.startup.success = 0
			if target.startup.failure >= probe.failureThreshold() {
				target.failed = true
				r.emitEvent("startup", target.container, "probe.failed", detail)
			}
		}
		return // readiness and liveness are gated by startup
	}
	if probe := target.cfg.Readiness; probe != nil && due(target.startedAt, target.readiness, probe, now) {
		healthy, detail := r.runProbe(ctx, *probe)
		target.readiness.lastRun = now
		target.lastRun = now
		target.lastError = detail
		r.updateBoolean(probe, &target.readiness, &target.ready, healthy, func(ready bool) {
			r.emitEvent("readiness", target.container, "probe.readiness", boolReason(ready))
			if r.OnReadinessChange != nil {
				r.OnReadinessChange(target.container, ready)
			}
		})
	}
	if probe := target.cfg.Liveness; probe != nil && due(target.startedAt, target.liveness, probe, now) {
		healthy, detail := r.runProbe(ctx, *probe)
		target.liveness.lastRun = now
		target.lastRun = now
		target.lastError = detail
		r.updateBoolean(probe, &target.liveness, &target.live, healthy, nil)
		if !healthy && target.liveness.failure >= probe.failureThreshold() {
			r.emitEvent("liveness", target.container, "probe.liveness", detail)
			if r.OnLivenessFailure != nil {
				r.OnLivenessFailure(target.container)
			}
		}
	}
}

func (r *Runner) updateBoolean(probe *Probe, counter *probeCounter, state *bool, healthy bool, onChange func(bool)) {
	changed := false
	if healthy {
		counter.success++
		counter.failure = 0
		if counter.success >= probe.successThreshold() && !*state {
			*state = true
			changed = true
		}
	} else {
		counter.failure++
		counter.success = 0
		if counter.failure >= probe.failureThreshold() && *state {
			*state = false
			changed = true
		}
	}
	if changed && onChange != nil {
		onChange(*state)
	}
}

func (r *Runner) runProbe(ctx context.Context, probe Probe) (bool, string) {
	probeCtx, cancel := context.WithTimeout(ctx, probe.timeout())
	defer cancel()
	return r.prober.Probe(probeCtx, probe)
}

func (r *Runner) emitEvent(role, container, kind, detail string) {
	if r.emit == nil {
		return
	}
	r.emit(Event{
		Time:     r.clock.Now().UTC(),
		Kind:     kind,
		Resource: container,
		Message:  detail,
		Reason:   role,
	})
}

func due(startedAt time.Time, counter probeCounter, probe *Probe, now time.Time) bool {
	if now.Before(startedAt.Add(probe.InitialDelay)) {
		return false
	}
	if counter.lastRun.IsZero() {
		return true
	}
	return now.Sub(counter.lastRun) >= probe.period()
}

func boolReason(ready bool) string {
	if ready {
		return "ready"
	}
	return "not ready"
}
