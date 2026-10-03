// SPDX-License-Identifier: Apache-2.0

package model

import (
	"sort"
	"strings"
)

// DefaultNamespace is the local Kubernetes namespace Grillo emulates.
const DefaultNamespace = "default"

// Normalize applies documented defaults and sorts semantically unordered
// collections so planning and hashing are deterministic. It preserves argument,
// init-container, and override order; only unordered collections are reordered.
func Normalize(app *Application) {
	if app == nil {
		return
	}
	if app.Identity.Namespace == "" {
		app.Identity.Namespace = DefaultNamespace
	}
	for i := range app.Workloads {
		normalizeWorkload(&app.Workloads[i])
	}
	sort.SliceStable(app.Workloads, func(i, j int) bool { return app.Workloads[i].ID < app.Workloads[j].ID })

	for i := range app.Services {
		svc := &app.Services[i]
		for j := range svc.Ports {
			if svc.Ports[j].Protocol == "" {
				svc.Ports[j].Protocol = "TCP"
			}
		}
		sort.SliceStable(svc.Ports, func(a, b int) bool {
			p, q := svc.Ports[a], svc.Ports[b]
			if p.Port != q.Port {
				return p.Port < q.Port
			}
			return p.Name < q.Name
		})
	}
	sort.SliceStable(app.Services, func(i, j int) bool { return app.Services[i].Name < app.Services[j].Name })

	for i := range app.Routes {
		if app.Routes[i].PathType == "" {
			app.Routes[i].PathType = "Prefix"
		}
	}
	sort.SliceStable(app.Routes, func(i, j int) bool {
		a, b := app.Routes[i], app.Routes[j]
		if a.Hostname != b.Hostname {
			return a.Hostname < b.Hostname
		}
		return a.Path < b.Path
	})

	sort.SliceStable(app.Volumes, func(i, j int) bool { return app.Volumes[i].Name < app.Volumes[j].Name })
	for i := range app.Configs {
		entries := app.Configs[i].Entries
		sort.SliceStable(entries, func(a, b int) bool { return entries[a].Key < entries[b].Key })
	}
	sort.SliceStable(app.Configs, func(i, j int) bool { return app.Configs[i].Name < app.Configs[j].Name })
	sort.SliceStable(app.Secrets, func(i, j int) bool { return app.Secrets[i].Name < app.Secrets[j].Name })
	sort.SliceStable(app.Networks, func(i, j int) bool { return app.Networks[i].Name < app.Networks[j].Name })
}

func normalizeWorkload(w *Workload) {
	if w.RestartPolicy == "" {
		w.RestartPolicy = RestartAlways
	}
	sort.Strings(w.DependsOn)
	sort.Strings(w.Template.Volumes)
	sort.Strings(w.Template.Networks)

	for i := range w.Template.InitContainers {
		normalizeContainer(&w.Template.InitContainers[i])
	}
	for i := range w.Template.Containers {
		normalizeContainer(&w.Template.Containers[i])
	}
	// App containers are unordered; init containers keep their declared order.
	sort.SliceStable(w.Template.Containers, func(i, j int) bool {
		return w.Template.Containers[i].Name < w.Template.Containers[j].Name
	})
}

func normalizeContainer(c *Container) {
	for i := range c.Ports {
		if c.Ports[i].Protocol == "" {
			c.Ports[i].Protocol = "TCP"
		}
	}
	sort.SliceStable(c.Ports, func(i, j int) bool {
		a, b := c.Ports[i], c.Ports[j]
		if a.ContainerPort != b.ContainerPort {
			return a.ContainerPort < b.ContainerPort
		}
		if a.Protocol != b.Protocol {
			return a.Protocol < b.Protocol
		}
		return a.Name < b.Name
	})
	sort.SliceStable(c.Env, func(i, j int) bool { return c.Env[i].Name < c.Env[j].Name })
	sort.SliceStable(c.Mounts, func(i, j int) bool {
		a, b := c.Mounts[i], c.Mounts[j]
		if a.MountPath != b.MountPath {
			return a.MountPath < b.MountPath
		}
		return a.Volume < b.Volume
	})
	normalizeProbes(&c.Probes)
}

func normalizeProbes(p *Probes) {
	for _, probe := range []*Probe{p.Startup, p.Readiness, p.Liveness} {
		if probe == nil {
			continue
		}
		if probe.HTTP != nil && probe.HTTP.Scheme == "" {
			probe.HTTP.Scheme = "HTTP"
		}
	}
}

// canonicalName normalizes a name for comparison, trimming surrounding space.
func canonicalName(s string) string { return strings.TrimSpace(s) }
