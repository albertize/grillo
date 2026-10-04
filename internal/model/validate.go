// SPDX-License-Identifier: Apache-2.0

package model

import (
	"fmt"
	"sort"
	"strings"

	"github.com/albertize/grillo/internal/source"
)

// Stable diagnostic codes. Codes are part of the compatibility contract and
// must not change meaning once published.
const (
	CodeVersionUnknown      = "ir.version.unknown"
	CodeIdentityInvalid     = "ir.identity.invalid"
	CodeDuplicateName       = "ir.duplicate.name"
	CodeReferenceMissing    = "ir.reference.missing"
	CodeReferenceCycle      = "ir.cycle.dependency"
	CodeQuantityNegative    = "ir.quantity.negative"
	CodeRequestExceedsGuest = "ir.request.exceeds-guest"
	CodeSupportUnsupported  = "ir.support.unsupported"
	CodeSupportDegraded     = "ir.support.degraded"
	CodeEnvConflict         = "ir.env.conflict"
	CodePortDuplicate       = "ir.port.duplicate"
	CodeVolumeSourceMissing = "ir.volume.source-missing"
	CodeImageMissing        = "ir.image.missing"
	CodeSelectorNoMatch     = "ir.selector.no-match"
)

// Validate checks the IR's structural and runtime invariants. It is pure: it has
// no side effects and never mutates app. The capability set determines which
// features are actually available; a feature absent from caps is unavailable.
func Validate(app Application, caps Capabilities) source.List {
	var diags source.List
	add := func(d source.Diagnostic) { diags = append(diags, d) }

	if app.APIVersion != "" && !VersionSupported(app.APIVersion) {
		add(source.Diagnostic{
			Code: CodeVersionUnknown, Severity: source.SeverityError,
			Resource: "metadata", Field: "apiVersion",
			Message:     fmt.Sprintf("unknown API version %q", app.APIVersion),
			Consequence: "the application cannot be read by this build",
			Remediation: fmt.Sprintf("use %s", APIVersion),
		})
	}
	if app.Kind != "" && app.Kind != KindApplication {
		add(source.Diagnostic{
			Code: CodeVersionUnknown, Severity: source.SeverityError,
			Resource: "metadata", Field: "kind",
			Message: fmt.Sprintf("unknown kind %q", app.Kind),
		})
	}
	if strings.TrimSpace(app.Identity.Name) == "" {
		add(source.Diagnostic{
			Code: CodeIdentityInvalid, Severity: source.SeverityError,
			Resource: "metadata", Field: "name", Message: "application name is required",
		})
	}

	volumes := indexVolumes(app)
	configs := indexConfigs(app)
	secrets := indexSecrets(app)
	workloads := indexWorkloads(app)
	services := indexServices(app)

	checkDuplicates(app, add)
	checkVolumes(app, add)
	for i := range app.Workloads {
		checkWorkload(app, &app.Workloads[i], volumes, configs, secrets, add)
	}
	checkServices(app, workloads, services, add)
	checkRoutes(app, services, secrets, add)
	checkCycles(app, workloads, add)
	checkCapabilities(app, caps, add)

	diags.Sort()
	return diags
}

func checkDuplicates(app Application, add func(source.Diagnostic)) {
	seen := map[string]bool{}
	report := func(resource, kind, name string) {
		if name == "" {
			return
		}
		key := kind + "/" + name
		if seen[key] {
			add(source.Diagnostic{
				Code: CodeDuplicateName, Severity: source.SeverityError,
				Resource: resource, Field: "name",
				Message: fmt.Sprintf("duplicate %s name %q", kind, name),
			})
			return
		}
		seen[key] = true
	}
	for _, w := range app.Workloads {
		report("workload/"+w.ID, "workload", w.ID)
	}
	for _, v := range app.Volumes {
		report("volume/"+v.Name, "volume", v.Name)
	}
	for _, c := range app.Configs {
		report("config/"+c.Name, "config", c.Name)
	}
	for _, s := range app.Secrets {
		report("secret/"+s.Name, "secret", s.Name)
	}
	for _, s := range app.Services {
		report("service/"+s.Name, "service", s.Name)
	}
}

func checkVolumes(app Application, add func(source.Diagnostic)) {
	for _, v := range app.Volumes {
		resource := "volume/" + v.Name
		if v.Capacity < 0 {
			add(negativeQuantity(resource, "capacity"))
		}
		if v.Kind == VolumeBind && strings.TrimSpace(v.Source) == "" {
			add(source.Diagnostic{
				Code: CodeVolumeSourceMissing, Severity: source.SeverityError,
				Resource: resource, Field: "source",
				Message:     "bind volume requires a host source path",
				Consequence: "the sandbox cannot start without a bind source",
			})
		}
	}
}

func checkWorkload(app Application, w *Workload, volumes map[string]bool, configs map[string]map[string]bool, secrets map[string]bool, add func(source.Diagnostic)) {
	resource := "workload/" + w.ID
	if w.Replicas < 0 {
		add(source.Diagnostic{Code: CodeQuantityNegative, Severity: source.SeverityError, Resource: resource, Field: "replicas", Message: "replicas must not be negative"})
	}

	containerNames := map[string]bool{}
	for _, c := range append(append([]Container{}, w.Template.InitContainers...), w.Template.Containers...) {
		if containerNames[c.Name] {
			add(source.Diagnostic{Code: CodeDuplicateName, Severity: source.SeverityError, Resource: resource + "/container/" + c.Name, Field: "name", Message: fmt.Sprintf("duplicate container name %q", c.Name)})
		}
		containerNames[c.Name] = true
	}
	if len(w.Template.Containers) == 0 {
		add(source.Diagnostic{Code: CodeReferenceMissing, Severity: source.SeverityError, Resource: resource, Field: "template.containers", Message: "workload has no containers"})
	}

	attached := map[string]bool{}
	for _, name := range w.Template.Volumes {
		attached[name] = true
	}
	checkContainer := func(c Container, kind string) {
		cresource := resource + "/" + kind + "/" + c.Name
		if strings.TrimSpace(c.Image.Reference) == "" {
			add(source.Diagnostic{Code: CodeImageMissing, Severity: source.SeverityError, Resource: cresource, Field: "image.reference", Message: "container image reference is required"})
		}
		portKeys := map[string]bool{}
		for _, p := range c.Ports {
			key := fmt.Sprintf("%s/%d", p.Protocol, p.ContainerPort)
			if portKeys[key] {
				add(source.Diagnostic{Code: CodePortDuplicate, Severity: source.SeverityError, Resource: cresource, Field: "ports", Message: fmt.Sprintf("duplicate container port %d/%s", p.ContainerPort, p.Protocol)})
			}
			portKeys[key] = true
		}
		for _, env := range c.Env {
			if env.ValueFrom != nil && env.Value != "" {
				add(source.Diagnostic{Code: CodeEnvConflict, Severity: source.SeverityError, Resource: cresource, Field: "env." + env.Name, Message: "env var sets both value and valueFrom"})
			}
			if env.ValueFrom == nil {
				continue
			}
			if ref := env.ValueFrom.ConfigRef; ref != nil {
				keys, ok := configs[ref.Config]
				if !ok {
					add(missingRef(cresource, "env."+env.Name, "config", ref.Config))
				} else if !keys[ref.Key] {
					add(source.Diagnostic{Code: CodeReferenceMissing, Severity: source.SeverityError, Resource: cresource, Field: "env." + env.Name, Message: fmt.Sprintf("config %q has no key %q", ref.Config, ref.Key)})
				}
			}
			if ref := env.ValueFrom.SecretRef; ref != nil {
				if !secrets[ref.Secret] {
					add(missingRef(cresource, "env."+env.Name, "secret", ref.Secret))
				}
			}
		}
		for _, m := range c.Mounts {
			if !volumes[m.Volume] {
				add(missingRef(cresource, "mounts", "volume", m.Volume))
				continue
			}
			if len(attached) > 0 && !attached[m.Volume] {
				add(source.Diagnostic{Code: CodeReferenceMissing, Severity: source.SeverityError, Resource: cresource, Field: "mounts", Message: fmt.Sprintf("volume %q is not attached to the sandbox", m.Volume)})
			}
		}
	}
	for _, c := range w.Template.InitContainers {
		checkContainer(c, "initContainer")
	}
	for _, c := range w.Template.Containers {
		checkContainer(c, "container")
	}

	checkGuestRequests(w, add)
}

func checkGuestRequests(w *Workload, add func(source.Diagnostic)) {
	var cpu Millicores
	var mem Bytes
	for _, c := range append(append([]Container{}, w.Template.InitContainers...), w.Template.Containers...) {
		cpu += c.Resources.Requests.CPU
		mem += c.Resources.Requests.Memory
	}
	resource := "workload/" + w.ID
	capCPU := w.Template.GuestResources.Requests.CPU
	if w.Template.GuestResources.Limits.CPU > capCPU {
		capCPU = w.Template.GuestResources.Limits.CPU
	}
	capMem := w.Template.GuestResources.Requests.Memory
	if w.Template.GuestResources.Limits.Memory > capMem {
		capMem = w.Template.GuestResources.Limits.Memory
	}
	if capCPU > 0 && cpu > capCPU {
		add(source.Diagnostic{Code: CodeRequestExceedsGuest, Severity: source.SeverityError, Resource: resource, Field: "template.guestResources.cpu", Message: fmt.Sprintf("container CPU requests (%s) exceed the guest budget (%s)", cpu, capCPU)})
	}
	if capMem > 0 && mem > capMem {
		add(source.Diagnostic{Code: CodeRequestExceedsGuest, Severity: source.SeverityError, Resource: resource, Field: "template.guestResources.memory", Message: fmt.Sprintf("container memory requests (%s) exceed the guest budget (%s)", mem, capMem)})
	}
}

func checkServices(app Application, workloads map[string]*Workload, services map[string]*Service, add func(source.Diagnostic)) {
	for _, svc := range app.Services {
		resource := "service/" + svc.Name
		names := map[string]bool{}
		numbers := map[string]bool{}
		for _, p := range svc.Ports {
			if p.Port < 0 {
				add(negativeQuantity(resource, "ports.port"))
			}
			if p.Name != "" {
				if names[p.Name] {
					add(source.Diagnostic{Code: CodePortDuplicate, Severity: source.SeverityError, Resource: resource, Field: "ports", Message: fmt.Sprintf("duplicate service port name %q", p.Name)})
				}
				names[p.Name] = true
			}
			key := fmt.Sprintf("%s/%d", p.Protocol, p.Port)
			if numbers[key] {
				add(source.Diagnostic{Code: CodePortDuplicate, Severity: source.SeverityError, Resource: resource, Field: "ports", Message: fmt.Sprintf("duplicate service port %d/%s", p.Port, p.Protocol)})
			}
			numbers[key] = true
		}
		matched := matchWorkloads(workloads, svc.Selector)
		if len(svc.Selector) > 0 && len(matched) == 0 {
			add(source.Diagnostic{Code: CodeSelectorNoMatch, Severity: source.SeverityWarning, Resource: resource, Field: "selector", Message: "service selector matches no workload"})
		}
		for _, p := range svc.Ports {
			if p.TargetPort == nil || p.TargetPort.Name == "" {
				continue
			}
			if !namedPortExists(matched, p.TargetPort.Name) {
				add(source.Diagnostic{Code: CodeReferenceMissing, Severity: source.SeverityError, Resource: resource, Field: "ports.targetPort", Message: fmt.Sprintf("no matched container exposes port %q", p.TargetPort.Name)})
			}
		}
	}
}

func checkRoutes(app Application, services map[string]*Service, secrets map[string]bool, add func(source.Diagnostic)) {
	for _, route := range app.Routes {
		resource := "route/" + routeName(route)
		svc, ok := services[route.Service]
		if !ok {
			add(missingRef(resource, "service", "service", route.Service))
		} else if route.Port != nil && !serviceHasPort(svc, route.Port) {
			add(source.Diagnostic{Code: CodeReferenceMissing, Severity: source.SeverityError, Resource: resource, Field: "port", Message: "route port does not match any service port"})
		}
		if route.TLSRef != nil && !secrets[route.TLSRef.Secret] {
			add(missingRef(resource, "tlsRef", "secret", route.TLSRef.Secret))
		}
	}
}

// checkCycles detects cycles in the experimental workload dependency graph.
func checkCycles(app Application, workloads map[string]*Workload, add func(source.Diagnostic)) {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var stack []string
	var visit func(id string) bool

	visit = func(id string) bool {
		color[id] = gray
		stack = append(stack, id)
		w := workloads[id]
		if w != nil {
			for _, dep := range w.DependsOn {
				if _, ok := workloads[dep]; !ok {
					continue
				}
				switch color[dep] {
				case gray:
					cycle := append(append([]string{}, stack...), dep)
					add(source.Diagnostic{
						Code: CodeReferenceCycle, Severity: source.SeverityError,
						Resource: "workload/" + id, Field: "dependsOn",
						Message:     "dependency cycle: " + strings.Join(cycle, " -> "),
						Consequence: "the workloads cannot be ordered for startup",
					})
					return true
				case white:
					if visit(dep) {
						return true
					}
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[id] = black
		return false
	}

	ids := make([]string, 0, len(workloads))
	for id := range workloads {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if color[id] == white {
			if visit(id) {
				return
			}
		}
	}
}

func checkCapabilities(app Application, caps Capabilities, add func(source.Diagnostic)) {
	registry := DefaultRegistry()
	for _, use := range requiredFeatures(app) {
		state, ok := caps.State(use.feature)
		if !ok {
			state = source.Unsupported
		}
		support, _ := registry.Lookup(use.feature)
		switch state {
		case source.Unsupported, source.ValidateOnly:
			add(source.Diagnostic{
				Code: CodeSupportUnsupported, Severity: source.SeverityError,
				Compatibility: state, Resource: use.resource, Field: use.field,
				Message:     fmt.Sprintf("feature %s is %s", use.feature, state),
				Consequence: support.Consequence, Remediation: support.Remediation,
			})
		case source.Degraded:
			add(source.Diagnostic{
				Code: CodeSupportDegraded, Severity: source.SeverityWarning,
				Compatibility: state, Resource: use.resource, Field: use.field,
				Message:     fmt.Sprintf("feature %s is DEGRADED", use.feature),
				Consequence: support.Consequence, Remediation: support.Remediation,
			})
		}
	}
}

func indexVolumes(app Application) map[string]bool {
	out := make(map[string]bool, len(app.Volumes))
	for _, v := range app.Volumes {
		out[v.Name] = true
	}
	return out
}

func indexConfigs(app Application) map[string]map[string]bool {
	out := make(map[string]map[string]bool, len(app.Configs))
	for _, c := range app.Configs {
		keys := make(map[string]bool, len(c.Entries))
		for _, e := range c.Entries {
			keys[e.Key] = true
		}
		out[c.Name] = keys
	}
	return out
}

func indexSecrets(app Application) map[string]bool {
	out := make(map[string]bool, len(app.Secrets))
	for _, s := range app.Secrets {
		out[s.Name] = true
	}
	return out
}

func indexWorkloads(app Application) map[string]*Workload {
	out := make(map[string]*Workload, len(app.Workloads))
	for i := range app.Workloads {
		out[app.Workloads[i].ID] = &app.Workloads[i]
	}
	return out
}

func indexServices(app Application) map[string]*Service {
	out := make(map[string]*Service, len(app.Services))
	for i := range app.Services {
		out[app.Services[i].Name] = &app.Services[i]
	}
	return out
}

func matchWorkloads(workloads map[string]*Workload, selector map[string]string) []*Workload {
	var matched []*Workload
	for _, w := range workloads {
		if labelsMatch(w.Labels, selector) {
			matched = append(matched, w)
		}
	}
	return matched
}

func labelsMatch(labels, selector map[string]string) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func namedPortExists(workloads []*Workload, name string) bool {
	for _, w := range workloads {
		for _, c := range w.Template.Containers {
			for _, p := range c.Ports {
				if p.Name == name {
					return true
				}
			}
		}
	}
	return false
}

func serviceHasPort(svc *Service, ref *PortRef) bool {
	for _, p := range svc.Ports {
		if ref.Name != "" && p.Name == ref.Name {
			return true
		}
		if ref.Number != 0 && p.Port == ref.Number {
			return true
		}
	}
	return false
}

func routeName(route Route) string {
	if route.Hostname == "" {
		return route.Path
	}
	return route.Hostname
}

func missingRef(resource, field, kind, name string) source.Diagnostic {
	return source.Diagnostic{
		Code: CodeReferenceMissing, Severity: source.SeverityError,
		Resource: resource, Field: field,
		Message: fmt.Sprintf("referenced %s %q does not exist", kind, name),
	}
}

func negativeQuantity(resource, field string) source.Diagnostic {
	return source.Diagnostic{Code: CodeQuantityNegative, Severity: source.SeverityError, Resource: resource, Field: field, Message: "quantity must not be negative"}
}
