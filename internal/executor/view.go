//go:build linux

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/observe"
	"github.com/albertize/grillo/internal/sandbox"
)

// View projects declared metadata and actual observations without exposing
// configs, env, private paths, guest specs or untrusted runtime error strings.
func (e *Executor) View(ctx context.Context, application string) (observe.ApplicationView, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	e.mu.Lock()
	app, ok := e.desired[application]
	var runtimes []sandboxRuntime
	for _, rt := range e.runtimes {
		if rt.app == application {
			runtimes = append(runtimes, *rt)
		}
	}
	networkFailed := e.networkErrors[application] != nil
	e.mu.Unlock()
	if !ok {
		return observe.ApplicationView{}, fmt.Errorf("application not found")
	}
	out := declaredView(app)
	if networkFailed {
		out.Diagnostics = append(out.Diagnostics, observe.DiagnosticView{Code: "network_unavailable", Message: "Application network requires attention; run grillo status."})
	}
	sort.Slice(runtimes, func(i, j int) bool { return runtimes[i].id < runtimes[j].id })
	for _, rt := range runtimes {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		vcpu := e.cfg.VCPU
		if vcpu == 0 {
			vcpu = 1
		}
		view := observe.SandboxView{ID: rt.id, Workload: rt.workload, Backend: "QEMU microvm", IP: rt.ip, Ready: rt.ready, Draining: rt.draining, State: "unavailable", VCPU: vcpu, GuestBudgetBytes: int64(e.cfg.MemoryMiB) * 1024 * 1024}
		obs, err := e.cfg.Backend.Inspect(ctx, sandbox.Handle{ID: rt.id})
		if err == nil {
			view.State = string(obs.State)
			if obs.State == sandbox.StateRunning && obs.PID > 0 {
				if sample, err := observe.SampleProcess(obs.PID); err == nil {
					view.VMM = &observe.VMMView{Time: sample.Time, PID: sample.PID, RSSBytes: sample.RSSBytes, CPUTimeMS: sample.CPUTimeMS}
				}
			}
		} else {
			out.Diagnostics = append(out.Diagnostics, observe.DiagnosticView{Code: "sandbox_unavailable", Resource: rt.id, Message: "Sandbox observation unavailable."})
		}
		if rt.guest != nil {
			status, err := rt.guest.Status(ctx)
			if err == nil {
				for _, c := range status.Containers {
					cv := observe.ContainerObservation{Name: c.Name, State: c.State, ExitCode: c.ExitCode}
					if rt.runner != nil {
						if probe, ok := rt.runner.Status(c.Name); ok {
							cv.Ready, cv.Live, cv.StartupDone = &probe.Ready, &probe.Live, &probe.StartupDone
							if !probe.LastRun.IsZero() {
								cv.LastProbeTime = &probe.LastRun
							}
						}
					}
					view.Containers = append(view.Containers, cv)
				}
			} else {
				out.Diagnostics = append(out.Diagnostics, observe.DiagnosticView{Code: "guest_unavailable", Resource: rt.id, Message: "Container observation unavailable."})
			}
		}
		out.Sandboxes = append(out.Sandboxes, view)
	}
	return out, nil
}

func declaredView(app model.Application) observe.ApplicationView {
	out := observe.ApplicationView{Application: app.Identity.Name, Time: time.Now().UTC()}
	if app.Source != nil {
		out.SourceKind, out.SourcePath = string(app.Source.Kind), app.Source.Path
	}
	for _, w := range app.Workloads {
		v := observe.WorkloadView{ID: w.ID, Kind: string(w.Kind), Replicas: w.Replicas}
		for i, c := range allContainers(w) {
			cv := observe.ContainerView{Name: c.Name, Image: c.Image.Reference, Init: i < len(w.Template.InitContainers), StartupProbe: c.Probes.Startup != nil, ReadinessProbe: c.Probes.Readiness != nil, LivenessProbe: c.Probes.Liveness != nil}
			for _, m := range c.Mounts {
				cv.Mounts = append(cv.Mounts, observe.MountView{Volume: m.Volume, Path: m.MountPath, ReadOnly: m.ReadOnly})
			}
			for _, p := range c.Ports {
				cv.Ports = append(cv.Ports, observe.PortView{Port: p.ContainerPort, HostPort: p.HostPort, Protocol: p.Protocol})
			}
			v.Containers = append(v.Containers, cv)
		}
		out.Workloads = append(out.Workloads, v)
	}
	for _, svc := range app.Services {
		v := observe.ServiceView{Name: svc.Name, Headless: svc.Headless}
		for _, p := range svc.Ports {
			v.Ports = append(v.Ports, p.Port)
		}
		for _, w := range app.Workloads {
			match := len(svc.Selector) > 0
			for key, value := range svc.Selector {
				if w.Labels[key] != value {
					match = false
				}
			}
			if match {
				v.Workloads = append(v.Workloads, w.ID)
			}
		}
		out.Services = append(out.Services, v)
	}
	for _, r := range app.Routes {
		out.Routes = append(out.Routes, observe.RouteView{Hostname: r.Hostname, Path: r.Path, Service: r.Service, Endpoint: r.Endpoint})
	}
	for _, v := range app.Volumes {
		out.Volumes = append(out.Volumes, observe.VolumeView{Name: v.Name, Kind: string(v.Kind), ReadOnly: v.ReadOnly, AccessMode: v.AccessMode, Capacity: int64(v.Capacity)})
	}
	for _, c := range app.Configs {
		out.Configs = append(out.Configs, observe.MetadataView{Name: c.Name, EntryCount: len(c.Entries)})
	}
	for _, s := range app.Secrets {
		out.Secrets = append(out.Secrets, observe.MetadataView{Name: s.Name})
	}
	out.Diagnostics = append(out.Diagnostics, observe.DiagnosticView{Code: "source_diagnostics_unavailable", Message: "Original compiler diagnostics are not retained. Run grillo plan on the source for field-level compatibility diagnostics."})
	return out
}
