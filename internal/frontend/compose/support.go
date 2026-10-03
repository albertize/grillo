// SPDX-License-Identifier: Apache-2.0

package compose

import "grillo.local/grillo/internal/source"

// supportEntry records the honest support state and consequence of a Compose
// field. Unknown fields are unsupported and produce a diagnostic rather than
// being silently discarded.
type supportEntry struct {
	State       source.Compatibility
	Consequence string
}

// registry maps a field path (with `*` for a service name or list index) to its
// support entry.
var registry = map[string]supportEntry{
	"services.*.image":          {source.Supported, "container image"},
	"services.*.build":          {source.ValidateOnly, "build context is delegated to the image builder; the IR references the resulting image tag"},
	"services.*.entrypoint":     {source.Supported, "process entrypoint"},
	"services.*.command":        {source.Supported, "process arguments"},
	"services.*.environment":    {source.Supported, "literal environment"},
	"services.*.env_file":       {source.Supported, "environment file resolved relative to the Compose file"},
	"services.*.ports":          {source.Supported, "published ports"},
	"services.*.expose":         {source.Supported, "metadata-only exposed ports"},
	"services.*.volumes":        {source.Supported, "named, anonymous, and project-relative bind volumes"},
	"services.*.networks":       {source.Supported, "logical network attachments"},
	"services.*.healthcheck":    {source.Supported, "mapped to a liveness probe; it does not restart the container"},
	"services.*.depends_on":     {source.Degraded, "startup ordering only, not a readiness guarantee"},
	"services.*.restart":        {source.Supported, "restart policy"},
	"services.*.deploy":         {source.Supported, "replicas and representable resource limits"},
	"services.*.user":           {source.Supported, "container user"},
	"services.*.working_dir":    {source.Supported, "container working directory"},
	"services.*.read_only":      {source.Supported, "read-only root filesystem"},
	"services.*.labels":         {source.ValidateOnly, "metadata only"},
	"services.*.container_name": {source.ValidateOnly, "ignored; the sandbox is named after the service"},

	"services.*.deploy.replicas":                {source.Supported, "replica count"},
	"services.*.deploy.resources":               {source.Supported, "resource limits"},
	"services.*.deploy.resources.limits":        {source.Supported, "resource limits"},
	"services.*.deploy.resources.limits.cpus":   {source.Supported, "CPU limit"},
	"services.*.deploy.resources.limits.memory": {source.Supported, "memory limit"},
	"services.*.deploy.restart_policy":          {source.Degraded, "restart condition mapped; retry backoff is not represented"},
	"services.*.deploy.mode":                    {source.Unsupported, "global/replicated modes are not supported; use replicas"},
	"services.*.deploy.placement":               {source.Unsupported, "cluster placement is not supported"},
	"services.*.deploy.update_config":           {source.Unsupported, "rolling updates are not supported"},

	"services.*.secrets":      {source.Unsupported, "advanced Compose secrets are not supported"},
	"services.*.configs":      {source.Unsupported, "advanced Compose configs are not supported"},
	"services.*.profiles":     {source.Unsupported, "profiles are not supported"},
	"services.*.extends":      {source.Unsupported, "extends is not supported"},
	"services.*.devices":      {source.Unsupported, "host devices are not supported"},
	"services.*.privileged":   {source.Unsupported, "privileged containers are not supported"},
	"services.*.pid":          {source.Unsupported, "host PID mode is not supported"},
	"services.*.ipc":          {source.Unsupported, "host IPC mode is not supported"},
	"services.*.network_mode": {source.Unsupported, "host/container network modes are not supported"},
	"services.*.cap_add":      {source.Degraded, "additional capabilities are recorded but not guaranteed to be applied"},
	"services.*.cap_drop":     {source.Degraded, "dropped capabilities are recorded but not guaranteed to be applied"},
	"services.*.sysctls":      {source.Unsupported, "kernel sysctls are not supported"},
	"services.*.ulimits":      {source.Unsupported, "ulimits are not supported"},
	"services.*.tmpfs":        {source.Unsupported, "tmpfs mounts are not supported"},
	"services.*.shm_size":     {source.Unsupported, "shm size is not supported"},
	"services.*.logging":      {source.ValidateOnly, "logging driver options are ignored; logs are captured by Grillo"},
	"services.*.platform":     {source.Unsupported, "platform selection is not supported"},

	"volumes.*":  {source.Supported, "named volume"},
	"networks.*": {source.Supported, "logical network"},
}

// supportFor resolves a field path, expanding a concrete key to `*`.
func supportFor(path, wildcard string) (supportEntry, bool) {
	if entry, ok := registry[path]; ok {
		return entry, true
	}
	if wildcard != "" {
		if entry, ok := registry[wildcard]; ok {
			return entry, true
		}
	}
	return supportEntry{}, false
}
