// SPDX-License-Identifier: Apache-2.0

package model

import "github.com/albertize/grillo/internal/source"

// knownVersions lists the IR versions this build can read. Only the current
// version is understood; unknown versions must be rejected rather than guessed.
var knownVersions = []string{APIVersion}

// VersionSupported reports whether the IR version is known.
func VersionSupported(version string) bool {
	for _, v := range knownVersions {
		if v == version {
			return true
		}
	}
	return false
}

// Feature is a capability the model may require from the runtime.
type Feature string

const (
	FeatureBindMount              Feature = "volume.bind"
	FeatureManagedVolume          Feature = "volume.managed"
	FeaturePVCMount               Feature = "volume.pvc"
	FeatureInitContainers         Feature = "sandbox.initContainers"
	FeatureMultipleContainers     Feature = "sandbox.multipleContainers"
	FeatureExecProbe              Feature = "probe.exec"
	FeatureHTTPProbe              Feature = "probe.http"
	FeatureTCPProbe               Feature = "probe.tcp"
	FeatureReadOnlyRootFilesystem Feature = "security.readOnlyRootFilesystem"
	FeaturePrivileged             Feature = "security.privileged"
	FeatureHostPort               Feature = "network.hostPort"
	FeatureRoute                  Feature = "network.route"
	FeatureStatefulSet            Feature = "workload.statefulSet"
	FeatureJob                    Feature = "workload.job"
)

// Support describes the default compatibility of a feature and its user impact.
type Support struct {
	Consequence string
	Remediation string
}

// Registry is the single source of truth for feature compatibility wording, so
// validators and the published compatibility data cannot diverge.
type Registry struct {
	entries map[Feature]Support
}

// DefaultRegistry returns the built-in feature registry. Until real runtime
// integration proves otherwise, features are described conservatively.
func DefaultRegistry() *Registry {
	return &Registry{entries: map[Feature]Support{
		FeatureBindMount:              {Consequence: "host bind mounts cross the isolation boundary", Remediation: "authorize bind mounts explicitly"},
		FeatureManagedVolume:          {Consequence: "data persists in a Grillo-managed volume", Remediation: ""},
		FeaturePVCMount:               {Consequence: "local PVC-compatible storage is used", Remediation: ""},
		FeatureInitContainers:         {Consequence: "init containers run in order before app containers", Remediation: ""},
		FeatureMultipleContainers:     {Consequence: "containers share one sandbox network and localhost", Remediation: ""},
		FeatureExecProbe:              {Consequence: "probes exec inside the container", Remediation: ""},
		FeatureHTTPProbe:              {Consequence: "probes issue HTTP requests from the guest", Remediation: ""},
		FeatureTCPProbe:               {Consequence: "probes open TCP connections from the guest", Remediation: ""},
		FeatureReadOnlyRootFilesystem: {Consequence: "the container root filesystem is read-only", Remediation: ""},
		FeaturePrivileged:             {Consequence: "privileged containers weaken isolation", Remediation: "avoid privileged unless required"},
		FeatureHostPort:               {Consequence: "the host publishes a port to the sandbox", Remediation: ""},
		FeatureRoute:                  {Consequence: "an HTTP route fronts a service", Remediation: ""},
		FeatureStatefulSet:            {Consequence: "stable identity and per-replica storage are required", Remediation: ""},
		FeatureJob:                    {Consequence: "the workload runs to completion", Remediation: ""},
	}}
}

// Lookup returns the support description for a feature.
func (r *Registry) Lookup(f Feature) (Support, bool) {
	if r == nil {
		return Support{}, false
	}
	s, ok := r.entries[f]
	return s, ok
}

// Capabilities is the set of features the runtime can actually provide. A
// missing feature is treated as unavailable, never as available by default.
type Capabilities struct {
	states map[Feature]source.Compatibility
}

// NewCapabilities returns an empty capability set.
func NewCapabilities() Capabilities { return Capabilities{states: map[Feature]source.Compatibility{}} }

// FullCapabilities marks every known feature as supported. It is for pure
// structural validation and tests, not a runtime claim.
func FullCapabilities() Capabilities {
	caps := NewCapabilities()
	for _, f := range allFeatures() {
		caps.Set(f, source.Supported)
	}
	return caps
}

// Set records the compatibility of a feature.
func (c *Capabilities) Set(f Feature, state source.Compatibility) {
	if c.states == nil {
		c.states = map[Feature]source.Compatibility{}
	}
	c.states[f] = state
}

// State returns the compatibility of a feature and whether it was reported.
func (c Capabilities) State(f Feature) (source.Compatibility, bool) {
	state, ok := c.states[f]
	return state, ok
}

func allFeatures() []Feature {
	return []Feature{
		FeatureBindMount, FeatureManagedVolume, FeaturePVCMount,
		FeatureInitContainers, FeatureMultipleContainers,
		FeatureExecProbe, FeatureHTTPProbe, FeatureTCPProbe,
		FeatureReadOnlyRootFilesystem, FeaturePrivileged,
		FeatureHostPort, FeatureRoute, FeatureStatefulSet, FeatureJob,
	}
}

// featureUse records where a required feature was seen.
type featureUse struct {
	feature  Feature
	resource string
	field    string
}

// requiredFeatures walks an application and reports the features it needs.
func requiredFeatures(app Application) []featureUse {
	var uses []featureUse
	add := func(f Feature, resource, field string) {
		uses = append(uses, featureUse{feature: f, resource: resource, field: field})
	}
	for _, vol := range app.Volumes {
		resource := "volume/" + vol.Name
		switch vol.Kind {
		case VolumeBind:
			add(FeatureBindMount, resource, "kind")
		case VolumeManaged:
			add(FeatureManagedVolume, resource, "kind")
		case VolumePVC:
			add(FeaturePVCMount, resource, "kind")
		}
	}
	for _, w := range app.Workloads {
		resource := "workload/" + w.ID
		switch w.Kind {
		case WorkloadStatefulSet:
			add(FeatureStatefulSet, resource, "kind")
		case WorkloadJob, WorkloadCronJob:
			add(FeatureJob, resource, "kind")
		}
		if len(w.Template.InitContainers) > 0 {
			add(FeatureInitContainers, resource, "template.initContainers")
		}
		if len(w.Template.Containers) > 1 {
			add(FeatureMultipleContainers, resource, "template.containers")
		}
		if w.Template.SecurityProfile.ReadOnlyRootFilesystem {
			add(FeatureReadOnlyRootFilesystem, resource, "template.securityProfile.readOnlyRootFilesystem")
		}
		if w.Template.SecurityProfile.Privileged {
			add(FeaturePrivileged, resource, "template.securityProfile.privileged")
		}
		for _, container := range w.Template.Containers {
			cresource := resource + "/container/" + container.Name
			for _, port := range container.Ports {
				if port.HostPort > 0 {
					add(FeatureHostPort, cresource, "ports.hostPort")
				}
			}
			for _, probe := range []*Probe{container.Probes.Startup, container.Probes.Readiness, container.Probes.Liveness} {
				if probe == nil {
					continue
				}
				switch probe.Kind {
				case ProbeExec:
					add(FeatureExecProbe, cresource, "probes")
				case ProbeHTTP:
					add(FeatureHTTPProbe, cresource, "probes")
				case ProbeTCP:
					add(FeatureTCPProbe, cresource, "probes")
				}
			}
		}
	}
	if len(app.Routes) > 0 {
		add(FeatureRoute, "routes", "routes")
	}
	return uses
}
