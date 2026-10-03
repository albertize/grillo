//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"grillo.local/grillo/internal/guestproto"
)

// Agent implements the guest side of the control protocol: it owns the sandbox
// spec, writes OCI bundles, runs init containers in order, starts application
// containers, and serves status, exec, probe, and stop requests.
type Agent struct {
	Runtime Runtime
	WorkDir string

	mu         sync.Mutex
	sandbox    guestproto.SandboxSpec
	containers map[string]*containerState
	started    bool

	dnsMu sync.Mutex
	dns   interface{ Close() error }
}

type containerState struct {
	spec   guestproto.ContainerSpec
	bundle string
	status string
	exit   ExitStatus
}

// NewAgent returns an agent whose bundles live under workDir.
func NewAgent(rt Runtime, workDir string) *Agent {
	return &Agent{
		Runtime:    rt,
		WorkDir:    workDir,
		containers: make(map[string]*containerState),
	}
}

// Handle implements guestproto.Handler.
func (a *Agent) Handle(ctx context.Context, req guestproto.Message, stream *guestproto.Stream) (any, *guestproto.Error) {
	switch req.Type {
	case guestproto.TypeStatus:
		return a.status(ctx)
	case guestproto.TypeStart:
		return a.start(ctx, req)
	case guestproto.TypeStop:
		return a.stop(ctx, req)
	case guestproto.TypeExec:
		return a.exec(ctx, req, stream)
	case guestproto.TypeProbe:
		return a.probe(ctx, req)
	case guestproto.TypeRestart:
		return a.restart(ctx, req)
	default:
		return nil, guestproto.Errorf(guestproto.CodeUnsupported, "message type %s", req.Type)
	}
}

func (a *Agent) start(ctx context.Context, req guestproto.Message) (any, *guestproto.Error) {
	var sr guestproto.StartRequest
	if err := guestproto.UnmarshalPayload(req.Payload, &sr); err != nil {
		return nil, guestproto.Errorf(guestproto.CodeBadRequest, "%v", err)
	}
	if sr.Sandbox == nil {
		return nil, guestproto.Errorf(guestproto.CodeBadRequest, "start requires a sandbox spec")
	}
	if err := sr.Sandbox.Validate(); err != nil {
		return nil, guestproto.Errorf(guestproto.CodeBadRequest, "%v", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.started {
		return nil, guestproto.Errorf(guestproto.CodeBusy, "sandbox already started")
	}
	a.sandbox = *sr.Sandbox
	if err := MountShares(a.sandbox.Shares); err != nil {
		return nil, guestproto.Errorf(guestproto.CodeInternal, "%v", err)
	}
	if err := a.startDNS(a.sandbox.DNS); err != nil {
		return nil, guestproto.Errorf(guestproto.CodeInternal, "%v", err)
	}
	a.containers = make(map[string]*containerState, len(a.sandbox.Containers))
	for _, c := range a.sandbox.Containers {
		bundle := filepath.Join(a.WorkDir, "bundles", c.Name)
		if err := WriteBundle(bundle, c, a.sandbox); err != nil {
			return nil, guestproto.Errorf(guestproto.CodeInternal, "%v", err)
		}
		a.containers[c.Name] = &containerState{spec: c, bundle: bundle, status: "created"}
	}

	// Init containers run to completion in declaration order and must succeed.
	for _, c := range a.sandbox.Containers {
		if !c.Init {
			continue
		}
		state := a.containers[c.Name]
		status, stdout, stderr, err := a.Runtime.Run(ctx, c.Name, state.bundle)
		if err != nil {
			return nil, guestproto.Errorf(guestproto.CodeInternal, "init container %s: %v", c.Name, err)
		}
		state.status = "exited"
		state.exit = status
		if status.ExitCode != 0 {
			return nil, guestproto.Errorf(guestproto.CodeInternal, "init container %s failed with exit code %d: %s%s", c.Name, status.ExitCode, stdout, stderr)
		}
	}

	// Application and sidecar containers start detached.
	for _, c := range a.sandbox.Containers {
		if c.Init {
			continue
		}
		state := a.containers[c.Name]
		if err := a.Runtime.StartDetached(ctx, c.Name, state.bundle); err != nil {
			return nil, guestproto.Errorf(guestproto.CodeInternal, "start container %s: %v", c.Name, err)
		}
		state.status = "running"
	}
	a.started = true
	return guestproto.StartResult{State: "running", Containers: a.containerStatusesLocked(ctx)}, nil
}

func (a *Agent) stop(ctx context.Context, req guestproto.Message) (any, *guestproto.Error) {
	var sr guestproto.StopRequest
	if err := guestproto.UnmarshalPayload(req.Payload, &sr); err != nil {
		return nil, guestproto.Errorf(guestproto.CodeBadRequest, "%v", err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopLocked(ctx)
	return guestproto.StopResult{Stopped: true}, nil
}

// Shutdown stops all containers. It is used on guest shutdown.
func (a *Agent) Shutdown(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopLocked(ctx)
}

func (a *Agent) stopLocked(ctx context.Context) {
	a.stopDNS()
	// Stop application containers first, newest first, then remove the rest.
	for i := len(a.sandbox.Containers) - 1; i >= 0; i-- {
		c := a.sandbox.Containers[i]
		if c.Init {
			continue
		}
		_ = a.Runtime.Kill(ctx, c.Name, syscall.SIGTERM)
		_ = a.Runtime.Delete(ctx, c.Name, true)
	}
	for i := len(a.sandbox.Containers) - 1; i >= 0; i-- {
		c := a.sandbox.Containers[i]
		if !c.Init {
			continue
		}
		_ = a.Runtime.Delete(ctx, c.Name, true)
	}
	for _, state := range a.containers {
		if state.status == "running" {
			state.status = "stopped"
		}
	}
	a.started = false
}

func (a *Agent) status(ctx context.Context) (any, *guestproto.Error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	state := "created"
	if a.started {
		state = "running"
	}
	return guestproto.StatusResult{State: state, Zombies: countZombies(), Containers: a.containerStatusesLocked(ctx)}, nil
}

func (a *Agent) containerStatusesLocked(ctx context.Context) []guestproto.ContainerStatus {
	out := make([]guestproto.ContainerStatus, 0, len(a.sandbox.Containers))
	for _, c := range a.sandbox.Containers {
		state := a.containers[c.Name]
		status := state.status
		pid := 0
		if !c.Init && status == "running" {
			if rs, err := a.Runtime.State(ctx, c.Name); err == nil {
				if rs.Status != "" {
					status = rs.Status
				}
				pid = rs.PID
			}
		}
		out = append(out, guestproto.ContainerStatus{
			Name:     c.Name,
			State:    status,
			ExitCode: state.exit.ExitCode,
			PID:      pid,
		})
	}
	return out
}

func (a *Agent) restart(ctx context.Context, req guestproto.Message) (any, *guestproto.Error) {
	var rr guestproto.RestartRequest
	if err := guestproto.UnmarshalPayload(req.Payload, &rr); err != nil {
		return nil, guestproto.Errorf(guestproto.CodeBadRequest, "%v", err)
	}
	if rr.Container == "" {
		return nil, guestproto.Errorf(guestproto.CodeBadRequest, "restart requires a container")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state, ok := a.containers[rr.Container]
	if !ok {
		return nil, guestproto.Errorf(guestproto.CodeNotFound, "container %q not found", rr.Container)
	}
	_ = a.Runtime.Kill(ctx, rr.Container, syscall.SIGKILL)
	_ = a.Runtime.Delete(ctx, rr.Container, true)
	if err := a.Runtime.StartDetached(ctx, rr.Container, state.bundle); err != nil {
		state.status = "failed"
		return nil, guestproto.Errorf(guestproto.CodeInternal, "restart %s: %v", rr.Container, err)
	}
	state.status = "running"
	state.exit = ExitStatus{}
	return guestproto.RestartResult{State: "running"}, nil
}

func (a *Agent) exec(ctx context.Context, req guestproto.Message, stream *guestproto.Stream) (any, *guestproto.Error) {
	var er guestproto.ExecRequest
	if err := guestproto.UnmarshalPayload(req.Payload, &er); err != nil {
		return nil, guestproto.Errorf(guestproto.CodeBadRequest, "%v", err)
	}
	if er.Container == "" {
		return nil, guestproto.Errorf(guestproto.CodeBadRequest, "exec requires a container")
	}
	if len(er.Args) == 0 {
		return nil, guestproto.Errorf(guestproto.CodeBadRequest, "exec requires a command")
	}
	status, stdout, stderr, err := a.Runtime.Exec(ctx, er.Container, er.Args)
	if err != nil {
		return nil, guestproto.Errorf(guestproto.CodeInternal, "%v", err)
	}
	if stream != nil {
		if len(stdout) > 0 {
			_, _ = stream.WriteStdout(stdout)
		}
		if len(stderr) > 0 {
			_, _ = stream.WriteStderr(stderr)
		}
		_ = stream.Exit(status.ExitCode)
	}
	return guestproto.ExecResult{ExitCode: status.ExitCode}, nil
}

func (a *Agent) probe(ctx context.Context, req guestproto.Message) (any, *guestproto.Error) {
	var pr guestproto.ProbeRequest
	if err := guestproto.UnmarshalPayload(req.Payload, &pr); err != nil {
		return nil, guestproto.Errorf(guestproto.CodeBadRequest, "%v", err)
	}
	timeout := time.Duration(pr.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	switch pr.Kind {
	case guestproto.ProbeExec:
		if pr.Container == "" || len(pr.Command) == 0 {
			return nil, guestproto.Errorf(guestproto.CodeBadRequest, "exec probe requires a container and command")
		}
		status, _, stderr, err := a.Runtime.Exec(probeCtx, pr.Container, pr.Command)
		if err != nil {
			return guestproto.ProbeResult{Healthy: false, Detail: err.Error()}, nil
		}
		if status.ExitCode != 0 {
			return guestproto.ProbeResult{Healthy: false, Detail: fmt.Sprintf("exit %d: %s", status.ExitCode, stderr)}, nil
		}
		return guestproto.ProbeResult{Healthy: true}, nil
	case guestproto.ProbeTCP:
		var d net.Dialer
		conn, err := d.DialContext(probeCtx, "tcp", net.JoinHostPort(pr.Host, fmt.Sprintf("%d", pr.Port)))
		if err != nil {
			return guestproto.ProbeResult{Healthy: false, Detail: err.Error()}, nil
		}
		_ = conn.Close()
		return guestproto.ProbeResult{Healthy: true}, nil
	case guestproto.ProbeHTTP:
		req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, pr.URL, nil)
		if err != nil {
			return nil, guestproto.Errorf(guestproto.CodeBadRequest, "%v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return guestproto.ProbeResult{Healthy: false, Detail: err.Error()}, nil
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			return guestproto.ProbeResult{Healthy: true}, nil
		}
		return guestproto.ProbeResult{Healthy: false, Detail: fmt.Sprintf("status %d", resp.StatusCode)}, nil
	default:
		return nil, guestproto.Errorf(guestproto.CodeBadRequest, "unknown probe kind %q", pr.Kind)
	}
}

// Prepare creates the agent's working directories.
func (a *Agent) Prepare() error {
	for _, dir := range []string{a.WorkDir, filepath.Join(a.WorkDir, "bundles"), filepath.Join(a.WorkDir, "logs")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("guest: create %s: %w", dir, err)
		}
	}
	return nil
}
