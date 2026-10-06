//go:build linux

// SPDX-License-Identifier: Apache-2.0

package observe

import "time"

// ApplicationView is an allowlisted public projection, not a serialized IR or
// guest spec. Literal environment, config, probe commands and secret values
// intentionally have no representation here.
type ApplicationView struct {
	Application string           `json:"application"`
	Time        time.Time        `json:"time"`
	SourceKind  string           `json:"sourceKind,omitempty"`
	SourcePath  string           `json:"sourcePath,omitempty"`
	Workloads   []WorkloadView   `json:"workloads"`
	Sandboxes   []SandboxView    `json:"sandboxes"`
	Services    []ServiceView    `json:"services"`
	Routes      []RouteView      `json:"routes"`
	Volumes     []VolumeView     `json:"volumes"`
	Configs     []MetadataView   `json:"configs"`
	Secrets     []MetadataView   `json:"secrets"`
	Diagnostics []DiagnosticView `json:"diagnostics"`
}

type WorkloadView struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Replicas   int32           `json:"replicas"`
	Containers []ContainerView `json:"containers"`
}
type ContainerView struct {
	Name           string      `json:"name"`
	Image          string      `json:"image"`
	Init           bool        `json:"init"`
	Mounts         []MountView `json:"mounts"`
	Ports          []PortView  `json:"ports"`
	StartupProbe   bool        `json:"startupProbe"`
	ReadinessProbe bool        `json:"readinessProbe"`
	LivenessProbe  bool        `json:"livenessProbe"`
}
type MountView struct {
	Volume   string `json:"volume"`
	Path     string `json:"path"`
	ReadOnly bool   `json:"readOnly"`
}
type PortView struct {
	Port     int32  `json:"port"`
	HostPort int32  `json:"hostPort,omitempty"`
	Protocol string `json:"protocol"`
}
type SandboxView struct {
	ID               string                 `json:"id"`
	Workload         string                 `json:"workload"`
	Backend          string                 `json:"backend"`
	IP               string                 `json:"ip,omitempty"`
	State            string                 `json:"state"`
	Ready            bool                   `json:"ready"`
	Draining         bool                   `json:"draining"`
	VCPU             int                    `json:"vcpu"`
	GuestBudgetBytes int64                  `json:"guestBudgetBytes"`
	VMM              *VMMView               `json:"vmm,omitempty"`
	Containers       []ContainerObservation `json:"containers"`
}

// VMMView reports sampled process data only. CPU percentage requires deltas
// and is deliberately absent; it must not be represented by a fabricated zero.
type VMMView struct {
	Time      time.Time `json:"time"`
	PID       int       `json:"pid"`
	RSSBytes  int64     `json:"rssBytes"`
	CPUTimeMS int64     `json:"cpuTimeMs"`
}

type ContainerObservation struct {
	Name          string     `json:"name"`
	State         string     `json:"state"`
	ExitCode      int        `json:"exitCode"`
	Ready         *bool      `json:"ready,omitempty"`
	Live          *bool      `json:"live,omitempty"`
	StartupDone   *bool      `json:"startupDone,omitempty"`
	LastProbeTime *time.Time `json:"lastProbeTime,omitempty"`
}
type ServiceView struct {
	Name     string  `json:"name"`
	Headless bool    `json:"headless"`
	Ports    []int32 `json:"ports"`
	// Workloads are inferred from the declared selector, not measured traffic.
	Workloads []string `json:"workloads"`
}
type RouteView struct {
	Hostname string `json:"hostname"`
	Path     string `json:"path"`
	Service  string `json:"service"`
	Endpoint string `json:"endpoint,omitempty"`
}
type VolumeView struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	ReadOnly   bool   `json:"readOnly"`
	AccessMode string `json:"accessMode,omitempty"`
	Capacity   int64  `json:"capacity"`
}
type MetadataView struct {
	Name       string `json:"name"`
	EntryCount int    `json:"entryCount,omitempty"`
}
type DiagnosticView struct {
	Code     string `json:"code"`
	Resource string `json:"resource,omitempty"`
	Message  string `json:"message"`
}
