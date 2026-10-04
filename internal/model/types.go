// SPDX-License-Identifier: Apache-2.0

// Package model defines the versioned application intermediate representation
// (IR), its validation, normalization, canonical hashing, capability handling,
// and public redaction. It is pure: it imports no VMM, network, process, or
// frontend code (only the dependency-free source-location package).
package model

import "github.com/albertize/grillo/internal/source"

// APIVersion is the current IR schema version.
const APIVersion = "grillo.dev/v1alpha1"

// KindApplication is the only top-level kind in this version.
const KindApplication = "Application"

// Identity is the persistent identity of an application. The natural key is
// (user, application, namespace, name).
type Identity struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// Application is the compiled unit of work produced by every frontend.
type Application struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Identity   Identity       `json:"metadata"`
	Source     *source.Source `json:"source,omitempty"`

	Workloads []Workload  `json:"workloads,omitempty"`
	Services  []Service   `json:"services,omitempty"`
	Routes    []Route     `json:"routes,omitempty"`
	Volumes   []Volume    `json:"volumes,omitempty"`
	Configs   []Config    `json:"configs,omitempty"`
	Secrets   []SecretRef `json:"secrets,omitempty"`
	Networks  []Network   `json:"networks,omitempty"`
}

// WorkloadKind is a portable workload shape, not a Kubernetes object kind.
type WorkloadKind string

const (
	WorkloadDeployment  WorkloadKind = "Deployment"
	WorkloadStatefulSet WorkloadKind = "StatefulSet"
	WorkloadDaemonSet   WorkloadKind = "DaemonSet"
	WorkloadJob         WorkloadKind = "Job"
	WorkloadCronJob     WorkloadKind = "CronJob"
)

// RestartPolicy mirrors the application-facing restart intent.
type RestartPolicy string

const (
	RestartAlways    RestartPolicy = "Always"
	RestartOnFailure RestartPolicy = "OnFailure"
	RestartNever     RestartPolicy = "Never"
)

// Workload is one declarative workload and its sandbox template.
type Workload struct {
	ID     string            `json:"id"`
	Kind   WorkloadKind      `json:"kind"`
	Labels map[string]string `json:"labels,omitempty"`

	Replicas int32 `json:"replicas"`

	// DependsOn is an experimental ordering hint understood only by the native
	// JSON manifest. It is not a Kubernetes or Compose concept.
	DependsOn []string `json:"dependsOn,omitempty"`

	Template         SandboxTemplate   `json:"template"`
	RestartPolicy    RestartPolicy     `json:"restartPolicy,omitempty"`
	UpdatePolicy     *UpdatePolicy     `json:"updatePolicy,omitempty"`
	CompletionPolicy *CompletionPolicy `json:"completionPolicy,omitempty"`
}

// UpdatePolicy controls how replicas are replaced.
type UpdatePolicy struct {
	Strategy       string `json:"strategy,omitempty"`
	MaxUnavailable *int32 `json:"maxUnavailable,omitempty"`
	MaxSurge       *int32 `json:"maxSurge,omitempty"`
}

// CompletionPolicy applies to Jobs and CronJobs.
type CompletionPolicy struct {
	Completions  int32 `json:"completions,omitempty"`
	Parallelism  int32 `json:"parallelism,omitempty"`
	BackoffLimit int32 `json:"backoffLimit,omitempty"`
}

// SandboxTemplate describes one microVM: its containers, resources, and shared
// sandbox-level settings. One template maps to one Pod-equivalent sandbox.
type SandboxTemplate struct {
	Hostname        string          `json:"hostname,omitempty"`
	InitContainers  []Container     `json:"initContainers,omitempty"`
	Containers      []Container     `json:"containers"`
	GuestResources  Resources       `json:"guestResources,omitempty"`
	Volumes         []string        `json:"volumes,omitempty"`
	Networks        []string        `json:"networks,omitempty"`
	DNS             *DNSConfig      `json:"dns,omitempty"`
	SecurityProfile SecurityProfile `json:"securityProfile,omitempty"`
}

// DNSConfig is the sandbox resolver configuration.
type DNSConfig struct {
	Nameservers []string `json:"nameservers,omitempty"`
	Search      []string `json:"search,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// SecurityProfile captures explicit sandbox/container security intent.
type SecurityProfile struct {
	ReadOnlyRootFilesystem bool     `json:"readOnlyRootFilesystem,omitempty"`
	RunAsUser              *int64   `json:"runAsUser,omitempty"`
	RunAsGroup             *int64   `json:"runAsGroup,omitempty"`
	CapabilitiesAdd        []string `json:"capabilitiesAdd,omitempty"`
	CapabilitiesDrop       []string `json:"capabilitiesDrop,omitempty"`
	Privileged             bool     `json:"privileged,omitempty"`
	SeccompProfile         string   `json:"seccompProfile,omitempty"`
}

// Container is one OCI container inside a sandbox.
type Container struct {
	Name    string    `json:"name"`
	Image   ImageRef  `json:"image"`
	Command *[]string `json:"command,omitempty"`
	Args    *[]string `json:"args,omitempty"`

	Env        []EnvVar        `json:"env,omitempty"`
	Mounts     []VolumeMount   `json:"mounts,omitempty"`
	Ports      []ContainerPort `json:"ports,omitempty"`
	Resources  Resources       `json:"resources,omitempty"`
	Probes     Probes          `json:"probes,omitempty"`
	WorkingDir string          `json:"workingDir,omitempty"`
	User       string          `json:"user,omitempty"`
	// SecurityProfile, when present, is the resolved per-container policy.
	// Otherwise the sandbox template policy applies.
	SecurityProfile *SecurityProfile `json:"securityProfile,omitempty"`
}

// ImageRef is an image reference and, when known, its resolved digest.
type ImageRef struct {
	Reference  string `json:"reference"`
	Digest     string `json:"digest,omitempty"`
	PullPolicy string `json:"pullPolicy,omitempty"`
}

// ContainerPort is an exposed container port.
type ContainerPort struct {
	Name          string `json:"name,omitempty"`
	ContainerPort int32  `json:"containerPort"`
	Protocol      string `json:"protocol,omitempty"`
	HostPort      int32  `json:"hostPort,omitempty"`
}

// EnvVar is a literal value or a reference to a Config or Secret.
type EnvVar struct {
	Name      string     `json:"name"`
	Value     string     `json:"value,omitempty"`
	ValueFrom *EnvSource `json:"valueFrom,omitempty"`
}

// EnvSource selects a key from a Config or Secret.
type EnvSource struct {
	ConfigRef *ConfigKeyRef `json:"configRef,omitempty"`
	SecretRef *SecretKeyRef `json:"secretRef,omitempty"`
}

// ConfigKeyRef references a key in a Config.
type ConfigKeyRef struct {
	Config string `json:"config"`
	Key    string `json:"key"`
}

// SecretKeyRef references a key in a Secret. It never carries a value.
type SecretKeyRef struct {
	Secret string `json:"secret"`
	Key    string `json:"key"`
}

// VolumeMount attaches a named volume at a path inside one container.
type VolumeMount struct {
	Volume    string `json:"volume"`
	MountPath string `json:"mountPath"`
	ReadOnly  bool   `json:"readOnly,omitempty"`
	SubPath   string `json:"subPath,omitempty"`
}

// VolumeKind distinguishes storage classes with different security properties.
type VolumeKind string

const (
	VolumeEphemeral VolumeKind = "ephemeral"
	VolumeManaged   VolumeKind = "managed"
	VolumeBind      VolumeKind = "bind"
	VolumePVC       VolumeKind = "pvc"
)

// Volume is a named storage source.
type Volume struct {
	Name       string     `json:"name"`
	Kind       VolumeKind `json:"kind"`
	Source     string     `json:"source,omitempty"`
	ReadOnly   bool       `json:"readOnly,omitempty"`
	AccessMode string     `json:"accessMode,omitempty"`
	Capacity   Bytes      `json:"capacity,omitempty"`
	SubPath    string     `json:"subPath,omitempty"`
}

// Service is a stable name and selector for sandbox endpoints.
type Service struct {
	Name                     string            `json:"name"`
	Selector                 map[string]string `json:"selector,omitempty"`
	Ports                    []ServicePort     `json:"ports,omitempty"`
	Headless                 bool              `json:"headless,omitempty"`
	PublishNotReadyAddresses bool              `json:"publishNotReadyAddresses,omitempty"`
}

// ServicePort maps a service port to a container port by name or number.
type ServicePort struct {
	Name       string   `json:"name,omitempty"`
	Protocol   string   `json:"protocol,omitempty"`
	Port       int32    `json:"port"`
	TargetPort *PortRef `json:"targetPort,omitempty"`
}

// PortRef is a named or numeric port reference.
type PortRef struct {
	Name   string `json:"name,omitempty"`
	Number int32  `json:"number,omitempty"`
}

// Route is a developer-facing HTTP entry point derived from an Ingress or an
// explicit local exposure rule.
type Route struct {
	Hostname string        `json:"hostname,omitempty"`
	Path     string        `json:"path,omitempty"`
	PathType string        `json:"pathType,omitempty"`
	Service  string        `json:"service"`
	Port     *PortRef      `json:"port,omitempty"`
	TLSRef   *SecretKeyRef `json:"tlsRef,omitempty"`
	Endpoint string        `json:"endpoint,omitempty"`
}

// Network is a named logical network attachment.
type Network struct {
	Name string `json:"name"`
}

// ProbeKind selects one probe action.
type ProbeKind string

const (
	ProbeExec ProbeKind = "exec"
	ProbeHTTP ProbeKind = "http"
	ProbeTCP  ProbeKind = "tcp"
)

// Probe is a startup, readiness, or liveness check.
type Probe struct {
	Kind ProbeKind   `json:"kind"`
	Exec *ExecAction `json:"exec,omitempty"`
	HTTP *HTTPAction `json:"http,omitempty"`
	TCP  *TCPAction  `json:"tcp,omitempty"`

	InitialDelaySeconds int32 `json:"initialDelaySeconds,omitempty"`
	TimeoutSeconds      int32 `json:"timeoutSeconds,omitempty"`
	PeriodSeconds       int32 `json:"periodSeconds,omitempty"`
	FailureThreshold    int32 `json:"failureThreshold,omitempty"`
	SuccessThreshold    int32 `json:"successThreshold,omitempty"`
}

// ExecAction runs a command inside the container.
type ExecAction struct {
	Command []string `json:"command"`
}

// HTTPAction performs an HTTP GET.
type HTTPAction struct {
	Path        string       `json:"path,omitempty"`
	Port        PortRef      `json:"port"`
	Scheme      string       `json:"scheme,omitempty"`
	Host        string       `json:"host,omitempty"`
	HTTPHeaders []HTTPHeader `json:"httpHeaders,omitempty"`
}

// TCPAction opens a TCP connection.
type TCPAction struct {
	Port PortRef `json:"port"`
	Host string  `json:"host,omitempty"`
}

// HTTPHeader is a probe request header.
type HTTPHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Probes groups the three probe kinds.
type Probes struct {
	Startup   *Probe `json:"startup,omitempty"`
	Readiness *Probe `json:"readiness,omitempty"`
	Liveness  *Probe `json:"liveness,omitempty"`
}

// Resources separates requests and limits using integer quantities.
type Resources struct {
	Requests ResourceList `json:"requests,omitempty"`
	Limits   ResourceList `json:"limits,omitempty"`
}

// ResourceList holds CPU in millicores and memory in bytes.
type ResourceList struct {
	CPU    Millicores `json:"cpu,omitempty"`
	Memory Bytes      `json:"memory,omitempty"`
}

// Config is a set of configuration entries with optional source locations.
type Config struct {
	Name    string        `json:"name"`
	Entries []ConfigEntry `json:"entries,omitempty"`
}

// ConfigEntry is one text or binary configuration value.
type ConfigEntry struct {
	Key       string `json:"key"`
	Text      string `json:"text,omitempty"`
	Binary    []byte `json:"binary,omitempty"`
	Sensitive bool   `json:"sensitive,omitempty"`
}

// SecretRef identifies a secret and its version. It never contains a value.
type SecretRef struct {
	Name    string `json:"name"`
	ID      string `json:"id,omitempty"`
	Version string `json:"version,omitempty"`
}
