// SPDX-License-Identifier: Apache-2.0

package kubernetes

import "grillo.local/grillo/internal/source"

// supportEntry records how Grillo treats a kind or field.
type supportEntry struct {
	state       source.Compatibility
	consequence string
}

// kinds maps a Kubernetes kind to its MVP support state. Tier 3 and F4 kinds
// are rejected with an explanation instead of being silently ignored.
var kinds = map[string]supportEntry{
	"Pod":                   {source.Supported, ""},
	"Deployment":            {source.Supported, ""},
	"Service":               {source.Supported, ""},
	"Ingress":               {source.Supported, ""},
	"ConfigMap":             {source.Supported, ""},
	"Secret":                {source.Supported, ""},
	"PersistentVolumeClaim": {source.Supported, ""},
	// Tier 2.
	"DaemonSet":           {source.Degraded, "interpreted as one local replica"},
	"ServiceAccount":      {source.ValidateOnly, "metadata only; no credential behavior"},
	"PodDisruptionBudget": {source.ValidateOnly, "validation only"},
	"NetworkPolicy":       {source.Unsupported, "a reduced local policy model is not implemented"},
	// Tier 1 but scheduled for F4.
	"StatefulSet": {source.Unsupported, "StatefulSet identity and per-ordinal storage are scheduled for F4"},
	"Job":         {source.Unsupported, "Job completion semantics are scheduled for F4"},
	"CronJob":     {source.Unsupported, "CronJob scheduling is scheduled for F4"},
}

// podSpecFields maps a PodSpec field to its support state.
var podSpecFields = map[string]supportEntry{
	"containers":                    {source.Supported, ""},
	"initContainers":                {source.Supported, ""},
	"volumes":                       {source.Supported, ""},
	"restartPolicy":                 {source.Supported, ""},
	"hostname":                      {source.Supported, ""},
	"securityContext":               {source.Supported, ""},
	"dnsConfig":                     {source.Degraded, "search domains and nameservers are applied; other options may be ignored"},
	"nodeSelector":                  {source.Unsupported, "multi-node scheduling is not supported"},
	"affinity":                      {source.Unsupported, "scheduling is not supported"},
	"tolerations":                   {source.Unsupported, "scheduling is not supported"},
	"topologySpreadConstraints":     {source.Unsupported, "scheduling is not supported"},
	"priorityClassName":             {source.Unsupported, "scheduling is not supported"},
	"hostNetwork":                   {source.Unsupported, "host networking is not supported"},
	"hostPID":                       {source.Unsupported, "host PID namespace is not supported"},
	"hostIPC":                       {source.Unsupported, "host IPC namespace is not supported"},
	"serviceAccountName":            {source.ValidateOnly, "metadata only"},
	"automountServiceAccountToken":  {source.ValidateOnly, "metadata only"},
	"dnsPolicy":                     {source.ValidateOnly, "the guest resolver is fixed"},
	"terminationGracePeriodSeconds": {source.ValidateOnly, "ignored"},
	"enableServiceLinks":            {source.ValidateOnly, "ignored"},
	"subdomain":                     {source.ValidateOnly, "ignored"},
}

// containerFields maps a container field to its support state.
var containerFields = map[string]supportEntry{
	"name":                     {source.Supported, ""},
	"image":                    {source.Supported, ""},
	"imagePullPolicy":          {source.Supported, ""},
	"command":                  {source.Supported, ""},
	"args":                     {source.Supported, ""},
	"env":                      {source.Supported, ""},
	"envFrom":                  {source.Supported, ""},
	"volumeMounts":             {source.Supported, ""},
	"ports":                    {source.Supported, ""},
	"resources":                {source.Supported, ""},
	"workingDir":               {source.Supported, ""},
	"securityContext":          {source.Supported, ""},
	"startupProbe":             {source.Supported, ""},
	"readinessProbe":           {source.Supported, ""},
	"livenessProbe":            {source.Supported, ""},
	"terminationMessagePath":   {source.ValidateOnly, "ignored"},
	"terminationMessagePolicy": {source.ValidateOnly, "ignored"},
	"stdin":                    {source.Unsupported, "interactive stdin is not supported"},
	"stdinOnce":                {source.Unsupported, "interactive stdin is not supported"},
	"tty":                      {source.Unsupported, "TTY is not supported"},
	"lifecycle":                {source.Unsupported, "lifecycle hooks are not implemented"},
}

// volumeSourceFields maps a Pod volume source to its support state.
var volumeSourceFields = map[string]supportEntry{
	"name":                  {source.Supported, ""},
	"emptyDir":              {source.Supported, ""},
	"persistentVolumeClaim": {source.Supported, ""},
	"configMap":             {source.Unsupported, "projected ConfigMap volumes are not implemented; use env or a Config entry"},
	"secret":                {source.Unsupported, "projected Secret volumes are not implemented; use env"},
	"hostPath":              {source.Unsupported, "host paths are not exposed to workloads"},
	"projected":             {source.Unsupported, "projected volumes are not implemented"},
	"downwardAPI":           {source.Unsupported, "the downward API is not implemented"},
	"nfs":                   {source.Unsupported, "network filesystems are not supported"},
	"csi":                   {source.Unsupported, "CSI drivers are not supported"},
}

// fieldSupport resolves a dotted field path within a kind.
func fieldSupport(registry map[string]supportEntry, path string) (supportEntry, bool) {
	entry, ok := registry[path]
	return entry, ok
}
