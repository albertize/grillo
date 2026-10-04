// SPDX-License-Identifier: Apache-2.0

// Package compose parses and compiles Compose files into the Grillo IR. It is a
// pure frontend: it imports only the IR and source-location packages and never
// touches the daemon, VMM, or network. Unsupported fields produce structured
// diagnostics instead of being silently discarded.
package compose

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/source"
)

// Options configures compilation.
type Options struct {
	// Path is the Compose file path (for the source map and env_file resolution).
	Path string
	// Environment is the interpolation environment (shell plus .env). Shell
	// values take precedence; callers merge accordingly.
	Environment map[string]string
	// ProjectName overrides the project/application name.
	ProjectName string
}

// Result is a compiled application with diagnostics and a source map.
type Result struct {
	Application model.Application
	Diagnostics source.List
	SourceMap   *source.Map
}

// Compile parses and compiles a Compose document.
func Compile(ctx context.Context, data []byte, opts Options) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	result := Result{SourceMap: source.NewMap()}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return result, fmt.Errorf("compose: parse: %w", err)
	}
	if len(document.Content) == 0 {
		// An empty document is an empty application, not an error.
		result.Application = emptyApplication(opts)
		return result, nil
	}
	root := document.Content[0]
	file := opts.Path
	env := opts.Environment
	if env == nil {
		env = map[string]string{}
	}
	interpolateNode(root, env, &result.Diagnostics, file)

	app := emptyApplication(opts)
	if name, ok := scalarValue(root, "name"); ok && name != "" {
		app.Identity.Name = name
	}

	services, ok := mapGet(root, "services")
	if !ok || services.Kind != yaml.MappingNode {
		result.Diagnostics = append(result.Diagnostics, diagnostic(source.SeverityError, source.Unsupported, "compose.missing_services", root, file, "", "a Compose file must define services", "", ""))
		result.Application = app
		return result, nil
	}

	volumeNodes, _ := mapGet(root, "volumes")
	networkNodes, _ := mapGet(root, "networks")
	topVolumes := topLevelVolumes(volumeNodes, &result.Diagnostics, file)
	_ = topLevelNetworks(networkNodes, &result.Diagnostics, file)
	externalVolumes := externalVolumeNames(volumeNodes)

	for i := 0; i < len(services.Content); i += 2 {
		nameNode := services.Content[i]
		serviceNode := services.Content[i+1]
		name := nameNode.Value
		workload, service, volumes, networks := compileService(name, serviceNode, topVolumes, externalVolumes, &result.Diagnostics, file, opts)
		app.Workloads = append(app.Workloads, workload)
		if service != nil {
			app.Services = append(app.Services, *service)
		}
		app.Volumes = append(app.Volumes, volumes...)
		for _, network := range networks {
			if !containsNetwork(app.Networks, network.Name) {
				app.Networks = append(app.Networks, network)
			}
		}
		result.SourceMap.Set("workload/"+name, spanOf(nameNode, file))
	}
	if !networkTopologySupported(app) {
		result.Diagnostics = append(result.Diagnostics, diagnostic(
			source.SeverityError, source.Unsupported, "compose.multi_network", root, file, "", "networks",
			"multiple distinct named-network topologies are not supported",
			"the runtime gives one application one network; services with different network membership would be silently flattened",
		))
	}
	result.Application = app
	return result, nil
}

// networkTopologySupported reports whether every workload shares the same
// effective network set. The current backend runs one network per application,
// so differing membership would be flattened.
func networkTopologySupported(app model.Application) bool {
	if len(app.Workloads) == 0 {
		return true
	}
	var first []string
	for i := range app.Workloads {
		set := effectiveNetworks(app.Workloads[i])
		if i == 0 {
			first = set
			continue
		}
		if !equalStringSlices(first, set) {
			return false
		}
	}
	return true
}

func effectiveNetworks(workload model.Workload) []string {
	if len(workload.Template.Networks) == 0 {
		return []string{"default"}
	}
	seen := map[string]bool{}
	var set []string
	for _, name := range workload.Template.Networks {
		if !seen[name] {
			seen[name] = true
			set = append(set, name)
		}
	}
	sort.Strings(set)
	return set
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func emptyApplication(opts Options) model.Application {
	name := opts.ProjectName
	if name == "" {
		name = projectNameFromPath(opts.Path)
	}
	return model.Application{
		APIVersion: model.APIVersion,
		Kind:       model.KindApplication,
		Identity:   model.Identity{Name: name, Namespace: model.DefaultNamespace},
		Source:     &source.Source{Kind: source.KindCompose, Path: opts.Path},
	}
}

func projectNameFromPath(path string) string {
	if path == "" {
		return "compose"
	}
	base := filepath.Base(filepath.Dir(path))
	if base == "." || base == "/" || base == "" {
		base = filepath.Base(path)
		base = strings.TrimSuffix(base, filepath.Ext(base))
	}
	return sanitizeName(base)
}

func compileService(name string, node *yaml.Node, topVolumes map[string]model.Volume, externalVolumes map[string]bool, diagnostics *source.List, file string, opts Options) (model.Workload, *model.Service, []model.Volume, []model.Network) {
	workload := model.Workload{
		ID:       name,
		Kind:     model.WorkloadDeployment,
		Replicas: 1,
		Labels:   map[string]string{"io.grillo.compose.service": name},
	}
	container := model.Container{Name: name}
	var service *model.Service
	var volumes []model.Volume
	networkNames := map[string]bool{}

	if node.Kind != yaml.MappingNode {
		*diagnostics = append(*diagnostics, diagnostic(source.SeverityError, source.Unsupported, "compose.invalid_service", node, file, "services/"+name, "", "service must be a mapping", ""))
		return workload, nil, nil, nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		key := keyNode.Value
		path := "services.*." + key
		entry, known := supportFor(path, "services."+name+"."+key)
		if !known {
			*diagnostics = append(*diagnostics, diagnostic(source.SeverityError, source.Unsupported, "compose.unknown_field", keyNode, file, "services/"+name, key, "unsupported field "+key, "this field has no Grillo mapping and blocks apply"))
			continue
		}
		if entry.State == source.Unsupported {
			*diagnostics = append(*diagnostics, diagnostic(source.SeverityError, entry.State, "compose.unsupported", keyNode, file, "services/"+name, key, "unsupported field "+key, entry.Consequence))
			continue
		}
		if entry.State != source.Supported {
			*diagnostics = append(*diagnostics, diagnostic(source.SeverityWarning, entry.State, "compose.degraded", keyNode, file, "services/"+name, key, entry.Consequence, entry.Consequence))
		}
		switch key {
		case "image":
			container.Image.Reference = valueNode.Value
		case "entrypoint":
			container.Command = commandList(valueNode, false)
		case "command":
			container.Args = commandList(valueNode, false)
		case "environment":
			container.Env = append(container.Env, environmentVars(valueNode, opts.Environment)...)
		case "env_file":
			container.Env = mergeEnv(envFileVars(valueNode, opts, diagnostics, file), container.Env)
		case "ports":
			ports, hostPorts, diags := parsePorts(valueNode, file, "services/"+name, container.Name)
			*diagnostics = append(*diagnostics, diags...)
			container.Ports = append(container.Ports, ports...)
			if len(hostPorts) > 0 {
				if service == nil {
					service = &model.Service{Name: name, Selector: workload.Labels}
				}
				service.Ports = append(service.Ports, hostPorts...)
			}
		case "expose":
			for _, item := range sequence(valueNode) {
				port, err := strconv.Atoi(strings.TrimSpace(item.Value))
				if err != nil {
					continue
				}
				container.Ports = append(container.Ports, model.ContainerPort{ContainerPort: int32(port)})
			}
		case "volumes":
			mounts, vols, diags := parseVolumes(name, valueNode, topVolumes, externalVolumes, file)
			*diagnostics = append(*diagnostics, diags...)
			container.Mounts = append(container.Mounts, mounts...)
			volumes = append(volumes, vols...)
		case "healthcheck":
			container.Probes.Liveness = parseHealthcheck(valueNode, file, "services/"+name)
		case "depends_on":
			workload.DependsOn = dependsOn(valueNode, diagnostics, file, "services/"+name)
		case "restart":
			workload.RestartPolicy = restartPolicy(valueNode.Value)
		case "user":
			container.User = valueNode.Value
		case "working_dir":
			container.WorkingDir = valueNode.Value
		case "read_only":
			workload.Template.SecurityProfile.ReadOnlyRootFilesystem = boolValue(valueNode)
		case "labels", "container_name":
			// Metadata-only; already reported as VALIDATE_ONLY.
		case "cap_add":
			for _, item := range sequence(valueNode) {
				workload.Template.SecurityProfile.CapabilitiesAdd = append(workload.Template.SecurityProfile.CapabilitiesAdd, item.Value)
			}
		case "cap_drop":
			for _, item := range sequence(valueNode) {
				workload.Template.SecurityProfile.CapabilitiesDrop = append(workload.Template.SecurityProfile.CapabilitiesDrop, item.Value)
			}
		case "deploy":
			compileDeploy(valueNode, &workload, &container, diagnostics, file, name)
		case "networks":
			for _, network := range networkNamesFrom(valueNode) {
				networkNames[network] = true
			}
		}
	}

	// Every Compose service is reachable by name on its networks, so always
	// publish a Service (with ports when declared) even without a host port.
	if service == nil {
		service = &model.Service{Name: name, Selector: workload.Labels}
	}
	if container.Image.Reference == "" && !hasBuild(node) {
		*diagnostics = append(*diagnostics, diagnostic(source.SeverityError, source.Unsupported, "compose.missing_image", node, file, "services/"+name, "image", "service "+name+" has neither image nor build", ""))
	}
	workload.Template.Containers = []model.Container{container}
	for network := range networkNames {
		workload.Template.Networks = append(workload.Template.Networks, network)
	}
	var networks []model.Network
	for network := range networkNames {
		networks = append(networks, model.Network{Name: network})
	}
	return workload, service, volumes, networks
}

func compileDeploy(node *yaml.Node, workload *model.Workload, container *model.Container, diagnostics *source.List, file, service string) {
	if node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		value := node.Content[i+1]
		switch key {
		case "replicas":
			if replicas, err := strconv.Atoi(value.Value); err == nil {
				workload.Replicas = int32(replicas)
			}
		case "resources":
			limits, ok := mapGet(value, "limits")
			if !ok {
				continue
			}
			if cpus, ok := scalarValue(limits, "cpus"); ok {
				if millicores, err := parseCPUs(cpus); err == nil {
					container.Resources.Limits.CPU = millicores
				}
			}
			if memory, ok := scalarValue(limits, "memory"); ok {
				if bytes, err := parseBytes(memory); err == nil {
					container.Resources.Limits.Memory = bytes
				}
			}
		case "restart_policy":
			if condition, ok := scalarValue(value, "condition"); ok {
				workload.RestartPolicy = restartPolicy(condition)
			}
		}
	}
}

func hasBuild(service *yaml.Node) bool {
	_, ok := mapGet(service, "build")
	return ok
}

func dependsOn(node *yaml.Node, diagnostics *source.List, file, resource string) []string {
	var names []string
	switch node.Kind {
	case yaml.SequenceNode:
		for _, item := range node.Content {
			names = append(names, item.Value)
		}
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			names = append(names, node.Content[i].Value)
			if condition, ok := scalarValue(node.Content[i+1], "condition"); ok && condition != "service_started" {
				*diagnostics = append(*diagnostics, diagnostic(source.SeverityWarning, source.Degraded, "compose.depends_on_condition", node.Content[i+1], file, resource, "depends_on", "condition "+condition+" is not guaranteed; only startup ordering is applied", ""))
			}
		}
	}
	return names
}

func restartPolicy(value string) model.RestartPolicy {
	switch strings.TrimSpace(value) {
	case "always", "unless-stopped":
		return model.RestartAlways
	case "on-failure":
		return model.RestartOnFailure
	case "no", "":
		return model.RestartNever
	default:
		return model.RestartAlways
	}
}

func boolValue(node *yaml.Node) bool {
	value, err := strconv.ParseBool(strings.TrimSpace(node.Value))
	return err == nil && value
}

func parseHealthcheck(node *yaml.Node, file, resource string) *model.Probe {
	test, ok := mapGet(node, "test")
	if !ok {
		return nil
	}
	var args []string
	disable := false
	for _, item := range sequence(test) {
		if item.Value == "NONE" {
			disable = true
		}
		args = append(args, item.Value)
	}
	if disable || len(args) == 0 {
		return nil
	}
	command := args
	switch args[0] {
	case "CMD":
		command = args[1:]
	case "CMD-SHELL":
		command = []string{"/bin/sh", "-c", strings.Join(args[1:], " ")}
	default:
		command = args
	}
	probe := &model.Probe{Kind: model.ProbeExec, Exec: &model.ExecAction{Command: command}, FailureThreshold: 3}
	if timeout, ok := scalarValue(node, "timeout"); ok {
		if duration, err := parseDuration(timeout); err == nil {
			probe.TimeoutSeconds = int32(duration.Seconds())
		}
	}
	if interval, ok := scalarValue(node, "interval"); ok {
		if duration, err := parseDuration(interval); err == nil {
			probe.PeriodSeconds = int32(duration.Seconds())
		}
	}
	if retries, ok := scalarValue(node, "retries"); ok {
		if value, err := strconv.Atoi(retries); err == nil {
			probe.FailureThreshold = int32(value)
		}
	}
	if start, ok := scalarValue(node, "start_period"); ok {
		if duration, err := parseDuration(start); err == nil {
			probe.InitialDelaySeconds = int32(duration.Seconds())
		}
	}
	return probe
}

func parsePorts(node *yaml.Node, file, resource, container string) ([]model.ContainerPort, []model.ServicePort, source.List) {
	var ports []model.ContainerPort
	var servicePorts []model.ServicePort
	var diagnostics source.List
	for _, item := range sequence(node) {
		target, published, protocol, address, err := parsePortEntry(item)
		if err != nil {
			diagnostics = append(diagnostics, diagnostic(source.SeverityError, source.Unsupported, "compose.invalid_port", item, file, resource, "ports", err.Error(), ""))
			continue
		}
		if protocol != "tcp" && protocol != "udp" {
			diagnostics = append(diagnostics, diagnostic(source.SeverityError, source.Unsupported, "compose.port_protocol", item, file, resource, "ports", "unsupported port protocol "+protocol, "only tcp and udp are supported"))
			continue
		}
		port := model.ContainerPort{ContainerPort: int32(target), Protocol: protocol}
		if published > 0 {
			port.HostPort = int32(published)
			servicePorts = append(servicePorts, model.ServicePort{Port: int32(published), TargetPort: &model.PortRef{Number: int32(target)}})
		}
		ports = append(ports, port)
		if address != "" && address != "0.0.0.0" && address != "::" {
			diagnostics = append(diagnostics, diagnostic(source.SeverityWarning, source.ValidateOnly, "compose.port_address", item, file, resource, "ports", "host bind address "+address+" is ignored; Grillo publishes on loopback", ""))
		}
	}
	return ports, servicePorts, diagnostics
}

func parsePortEntry(node *yaml.Node) (target, published int, protocol, address string, err error) {
	if node.Kind == yaml.MappingNode {
		targetValue, _ := scalarValue(node, "target")
		publishedValue, _ := scalarValue(node, "published")
		protocol, _ = scalarValue(node, "protocol")
		address, _ = scalarValue(node, "host_ip")
		target, err = strconv.Atoi(targetValue)
		if err != nil {
			return 0, 0, "", "", fmt.Errorf("invalid target port %q", targetValue)
		}
		if publishedValue != "" {
			published, err = strconv.Atoi(publishedValue)
			if err != nil {
				return 0, 0, "", "", fmt.Errorf("invalid published port %q", publishedValue)
			}
		}
		if protocol == "" {
			protocol = "tcp"
		}
		return target, published, protocol, address, nil
	}
	text := strings.TrimSpace(node.Value)
	if index := strings.IndexByte(text, '/'); index >= 0 {
		protocol = text[index+1:]
		text = text[:index]
	}
	if protocol == "" {
		protocol = "tcp"
	}
	parts := strings.Split(text, ":")
	switch len(parts) {
	case 1:
		target, err = strconv.Atoi(parts[0])
	case 2:
		published, err = strconv.Atoi(parts[0])
		if err == nil {
			target, err = strconv.Atoi(parts[1])
		}
	case 3:
		address = parts[0]
		published, err = strconv.Atoi(parts[1])
		if err == nil {
			target, err = strconv.Atoi(parts[2])
		}
	default:
		err = fmt.Errorf("invalid port %q", text)
	}
	if err != nil {
		return 0, 0, "", "", fmt.Errorf("invalid port %q", text)
	}
	return target, published, protocol, address, nil
}

func parseVolumes(service string, node *yaml.Node, topVolumes map[string]model.Volume, externalVolumes map[string]bool, file string) ([]model.VolumeMount, []model.Volume, source.List) {
	var mounts []model.VolumeMount
	var volumes []model.Volume
	var diagnostics source.List
	for index, item := range sequence(node) {
		mount, volume, err := parseVolumeEntry(service, index, item, topVolumes, externalVolumes, filepath.Dir(file))
		if err != nil {
			diagnostics = append(diagnostics, diagnostic(source.SeverityError, source.Unsupported, "compose.invalid_volume", item, file, "services/"+service, "volumes", err.Error(), ""))
			continue
		}
		mounts = append(mounts, mount)
		if volume != nil {
			volumes = append(volumes, *volume)
		}
	}
	return mounts, volumes, diagnostics
}

func parseVolumeEntry(service string, index int, node *yaml.Node, topVolumes map[string]model.Volume, externalVolumes map[string]bool, baseDir string) (model.VolumeMount, *model.Volume, error) {
	var mount model.VolumeMount
	var volume *model.Volume
	if node.Kind == yaml.MappingNode {
		typ, _ := scalarValue(node, "type")
		sourceValue, _ := scalarValue(node, "source")
		target, _ := scalarValue(node, "target")
		readOnly, _ := scalarValue(node, "read_only")
		mount.MountPath = mountPath(target)
		mount.ReadOnly = readOnly == "true"
		switch typ {
		case "bind":
			mount.Volume = fmt.Sprintf("%s-%d", service, index)
			volume = &model.Volume{Name: mount.Volume, Kind: model.VolumeBind, Source: resolveBindSource(sourceValue, baseDir)}
		case "volume":
			if sourceValue == "" {
				mount.Volume = fmt.Sprintf("%s-anon-%d", service, index)
				volume = &model.Volume{Name: mount.Volume, Kind: model.VolumeEphemeral}
			} else {
				if externalVolumes[sourceValue] {
					return model.VolumeMount{}, nil, fmt.Errorf("external volume %q must already exist and is not supported", sourceValue)
				}
				mount.Volume = sourceValue
				volume = existingOrNewVolume(sourceValue, topVolumes)
			}
		default:
			return model.VolumeMount{}, nil, fmt.Errorf("unsupported volume type %q", typ)
		}
		return mount, volume, nil
	}
	parts := strings.Split(node.Value, ":")
	switch len(parts) {
	case 1:
		// Anonymous volume, e.g. "/var/lib/data".
		mount.MountPath = parts[0]
		mount.Volume = fmt.Sprintf("%s-anon-%d", service, index)
		volume = &model.Volume{Name: mount.Volume, Kind: model.VolumeEphemeral}
	case 2, 3:
		source := parts[0]
		mount.MountPath = parts[1]
		if len(parts) == 3 && parts[2] == "ro" {
			mount.ReadOnly = true
		}
		if strings.HasPrefix(source, ".") || strings.HasPrefix(source, "/") || strings.HasPrefix(source, "~") {
			mount.Volume = fmt.Sprintf("%s-%d", service, index)
			volume = &model.Volume{Name: mount.Volume, Kind: model.VolumeBind, Source: resolveBindSource(source, baseDir)}
		} else {
			if externalVolumes[source] {
				return model.VolumeMount{}, nil, fmt.Errorf("external volume %q must already exist and is not supported", source)
			}
			mount.Volume = source
			volume = existingOrNewVolume(source, topVolumes)
		}
	default:
		return model.VolumeMount{}, nil, fmt.Errorf("invalid volume %q", node.Value)
	}
	return mount, volume, nil
}

// resolveBindSource makes a project-relative bind path absolute against the
// Compose file's directory.
func resolveBindSource(source, baseDir string) string {
	if strings.HasPrefix(source, ".") {
		return filepath.Clean(filepath.Join(baseDir, source))
	}
	return source
}

// mountPath normalizes a container mount target.
func mountPath(target string) string {
	if target == "" {
		return "/"
	}
	if !strings.HasPrefix(target, "/") {
		return "/" + target
	}
	return target
}

func existingOrNewVolume(name string, topVolumes map[string]model.Volume) *model.Volume {
	if volume, ok := topVolumes[name]; ok {
		copy := volume
		return &copy
	}
	return &model.Volume{Name: name, Kind: model.VolumeManaged}
}

func topLevelVolumes(node *yaml.Node, diagnostics *source.List, file string) map[string]model.Volume {
	volumes := map[string]model.Volume{}
	if node == nil || node.Kind != yaml.MappingNode {
		return volumes
	}
	for i := 0; i < len(node.Content); i += 2 {
		name := node.Content[i].Value
		volumes[name] = model.Volume{Name: name, Kind: model.VolumeManaged}
	}
	return volumes
}

func externalVolumeNames(node *yaml.Node) map[string]bool {
	external := map[string]bool{}
	if node == nil || node.Kind != yaml.MappingNode {
		return external
	}
	for i := 0; i < len(node.Content); i += 2 {
		if value, ok := scalarValue(node.Content[i+1], "external"); ok && value == "true" {
			external[node.Content[i].Value] = true
		}
	}
	return external
}

func topLevelNetworks(node *yaml.Node, _ *source.List, _ string) []model.Network {
	var networks []model.Network
	if node == nil || node.Kind != yaml.MappingNode {
		return networks
	}
	for i := 0; i < len(node.Content); i += 2 {
		networks = append(networks, model.Network{Name: node.Content[i].Value})
	}
	return networks
}

func networkNamesFrom(node *yaml.Node) []string {
	var names []string
	switch node.Kind {
	case yaml.SequenceNode:
		for _, item := range node.Content {
			names = append(names, item.Value)
		}
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			names = append(names, node.Content[i].Value)
		}
	}
	return names
}

func environmentVars(node *yaml.Node, env map[string]string) []model.EnvVar {
	var vars []model.EnvVar
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i].Value
			value := node.Content[i+1]
			if value.Tag == "!!null" {
				if hostValue, ok := env[key]; ok {
					vars = append(vars, model.EnvVar{Name: key, Value: hostValue})
				}
				continue
			}
			vars = append(vars, model.EnvVar{Name: key, Value: value.Value})
		}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			key, value, found := strings.Cut(item.Value, "=")
			if !found {
				if hostValue, ok := env[key]; ok {
					vars = append(vars, model.EnvVar{Name: key, Value: hostValue})
				} else {
					vars = append(vars, model.EnvVar{Name: key, Value: ""})
				}
				continue
			}
			vars = append(vars, model.EnvVar{Name: key, Value: value})
		}
	}
	return vars
}

func envFileVars(node *yaml.Node, opts Options, diagnostics *source.List, file string) []model.EnvVar {
	var vars []model.EnvVar
	for _, item := range sequence(node) {
		path := item.Value
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(opts.Path), path)
		}
		vars = append(vars, readEnvFile(path, diagnostics, file)...)
	}
	return vars
}

func readEnvFile(path string, diagnostics *source.List, file string) []model.EnvVar {
	data, err := os.ReadFile(path)
	if err != nil {
		*diagnostics = append(*diagnostics, diagnostic(source.SeverityWarning, source.ValidateOnly, "compose.env_file", nil, file, "", "env_file", "cannot read env_file "+path, ""))
		return nil
	}
	var vars []model.EnvVar
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		vars = append(vars, model.EnvVar{Name: strings.TrimSpace(key), Value: strings.Trim(value, `"'`)})
	}
	return vars
}

// mergeEnv applies override on top of base by key.
func mergeEnv(base, override []model.EnvVar) []model.EnvVar {
	index := map[string]int{}
	out := make([]model.EnvVar, 0, len(base)+len(override))
	for _, variable := range base {
		index[variable.Name] = len(out)
		out = append(out, variable)
	}
	for _, variable := range override {
		if position, ok := index[variable.Name]; ok {
			out[position] = variable
			continue
		}
		index[variable.Name] = len(out)
		out = append(out, variable)
	}
	return out
}

func commandList(node *yaml.Node, _ bool) *[]string {
	if node == nil || node.Tag == "!!null" {
		return nil
	}
	switch node.Kind {
	case yaml.SequenceNode:
		var args []string
		for _, item := range node.Content {
			args = append(args, item.Value)
		}
		return &args
	case yaml.ScalarNode:
		args := splitCommand(node.Value)
		return &args
	}
	return nil
}

// splitCommand performs shell-like word splitting without invoking a shell.
func splitCommand(input string) []string {
	var args []string
	var current strings.Builder
	inSingle, inDouble, escaped := false, false, false
	flush := func() {
		if current.Len() > 0 {
			args = append(args, current.String())
			current.Reset()
		}
	}
	for i := 0; i < len(input); i++ {
		c := input[i]
		switch {
		case escaped:
			current.WriteByte(c)
			escaped = false
		case c == '\\' && !inSingle:
			escaped = true
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case (c == ' ' || c == '\t') && !inSingle && !inDouble:
			flush()
		default:
			current.WriteByte(c)
		}
	}
	flush()
	return args
}

func parseDuration(value string) (time.Duration, error) {
	if duration, err := time.ParseDuration(value); err == nil {
		return duration, nil
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second, nil
	}
	return 0, fmt.Errorf("invalid duration %q", value)
}

func parseBytes(value string) (model.Bytes, error) {
	value = strings.TrimSpace(value)
	multiplier := int64(1)
	lower := strings.ToLower(value)
	for suffix, factor := range map[string]int64{"g": 1 << 30, "gb": 1 << 30, "m": 1 << 20, "mb": 1 << 20, "k": 1 << 10, "kb": 1 << 10, "b": 1} {
		if strings.HasSuffix(lower, suffix) {
			multiplier = factor
			value = value[:len(value)-len(suffix)]
			break
		}
	}
	number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, err
	}
	return model.Bytes(number * float64(multiplier)), nil
}

func parseCPUs(value string) (model.Millicores, error) {
	number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, err
	}
	return model.Millicores(number * 1000), nil
}

func containsNetwork(networks []model.Network, name string) bool {
	for _, network := range networks {
		if network.Name == name {
			return true
		}
	}
	return false
}

func sanitizeName(name string) string {
	var builder strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteByte('-')
		}
	}
	if builder.Len() == 0 {
		return "compose"
	}
	return builder.String()
}
