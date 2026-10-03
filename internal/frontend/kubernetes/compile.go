// SPDX-License-Identifier: Apache-2.0

// Package kubernetes compiles a supported Kubernetes subset (Pod, Deployment,
// Service, Ingress, ConfigMap, Secret, PersistentVolumeClaim) into the Grillo
// IR. It is a pure frontend: unsupported kinds and fields are rejected with
// structured diagnostics, and Secret values are returned separately so they
// never enter the public IR or a plan diff.
package kubernetes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"grillo.local/grillo/internal/model"
	"grillo.local/grillo/internal/source"
)

// Options configures compilation.
type Options struct {
	Path          string
	Namespace     string
	AllowDegraded []string
}

// SecretData carries Secret values out of the compiler. It is never part of the
// public Application.
type SecretData struct {
	Name    string
	Version string
	Data    map[string][]byte
}

// Result is a compiled application with diagnostics and secret material.
type Result struct {
	Application model.Application
	Diagnostics source.List
	SourceMap   *source.Map
	Secrets     []SecretData
}

// Compile parses and compiles Kubernetes YAML.
func Compile(ctx context.Context, data []byte, opts Options) (Result, error) {
	if opts.Namespace == "" {
		opts.Namespace = model.DefaultNamespace
	}
	compiler := &compiler{
		opts:        opts,
		result:      Result{SourceMap: source.NewMap()},
		configKeys:  map[string]map[string]bool{},
		secretKeys:  map[string]map[string]bool{},
		allow:       map[string]bool{},
		application: emptyApplication(opts),
	}
	for _, code := range opts.AllowDegraded {
		compiler.allow[code] = true
	}
	docs, err := documents(data)
	if err != nil {
		return Result{}, err
	}
	if len(docs) == 0 {
		compiler.application = emptyApplication(opts)
		compiler.result.Application = compiler.application
		return compiler.result, nil
	}
	// First pass: ConfigMaps, Secrets, and PVCs, so env references can resolve.
	var workloads []*yaml.Node
	for _, doc := range docs {
		kind := scalar(doc, "kind")
		switch kind {
		case "ConfigMap":
			compiler.compileConfigMap(doc)
		case "Secret":
			compiler.compileSecret(doc)
		case "PersistentVolumeClaim":
			compiler.compilePVC(doc)
		default:
			workloads = append(workloads, doc)
		}
	}
	// Second pass: workloads and their dependents.
	for _, doc := range workloads {
		compiler.compileResource(ctx, doc)
	}
	compiler.result.Application = compiler.application
	return compiler.result, nil
}

type compiler struct {
	opts        Options
	result      Result
	application model.Application
	configKeys  map[string]map[string]bool
	secretKeys  map[string]map[string]bool
	allow       map[string]bool
}

func emptyApplication(opts Options) model.Application {
	name := "kubernetes"
	if opts.Path != "" {
		base := opts.Path
		if index := strings.LastIndexAny(base, "/\\"); index >= 0 {
			base = base[index+1:]
		}
		base = strings.TrimSuffix(base, ".yaml")
		base = strings.TrimSuffix(base, ".yml")
		if base != "" {
			name = sanitizeName(base)
		}
	}
	return model.Application{
		APIVersion: model.APIVersion,
		Kind:       model.KindApplication,
		Identity:   model.Identity{Name: name, Namespace: opts.Namespace},
		Source:     &source.Source{Kind: source.KindKubernetes, Path: opts.Path},
	}
}

func documents(data []byte) ([]*yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var docs []*yaml.Node
	for {
		var node yaml.Node
		err := decoder.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("kubernetes: parse: %w", err)
		}
		if len(node.Content) == 0 {
			continue
		}
		root := node.Content[0]
		if root.Kind == 0 {
			continue
		}
		if scalar(root, "kind") == "List" {
			if items, ok := mapGet(root, "items"); ok {
				for _, item := range sequence(items) {
					docs = append(docs, item)
				}
			}
			continue
		}
		docs = append(docs, root)
	}
	return docs, nil
}

func (c *compiler) compileResource(ctx context.Context, doc *yaml.Node) {
	_ = ctx
	kind := scalar(doc, "kind")
	apiVersion := scalar(doc, "apiVersion")
	if kind == "" {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.missing_kind", doc, "", "", "resource has no kind", "the resource cannot be compiled")
		return
	}
	entry, known := kinds[kind]
	if !known {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.unsupported_kind", doc, "", "", "unsupported or custom resource "+kind+" ("+apiVersion+")", "custom resources and operators are outside the MVP")
		return
	}
	if entry.state == source.Unsupported {
		c.add(source.SeverityError, entry.state, "kubernetes.unsupported_kind", doc, resourceID(kind, doc), "", kind+" is not supported", entry.consequence)
		return
	}
	if entry.state == source.Degraded {
		c.downgrade("kubernetes."+strings.ToLower(kind)+"_degraded", doc, resourceID(kind, doc), "", kind+" is degraded", entry.consequence)
	}
	if entry.state == source.ValidateOnly {
		// Metadata only: validate the namespace and move on.
		c.checkNamespace(doc, kind)
		return
	}
	if !c.checkNamespace(doc, kind) {
		return
	}
	switch kind {
	case "Pod":
		c.compilePod(doc)
	case "Deployment":
		c.compileDeployment(doc)
	case "DaemonSet":
		c.compileDeployment(doc)
	case "Service":
		c.compileService(doc)
	case "Ingress":
		c.compileIngress(doc)
	default:
		c.add(source.SeverityError, source.Unsupported, "kubernetes.unsupported_kind", doc, resourceID(kind, doc), "", "unsupported kind "+kind, "")
	}
}

func (c *compiler) checkNamespace(doc *yaml.Node, kind string) bool {
	namespace := metadata(doc, "namespace")
	if namespace != "" && namespace != c.opts.Namespace {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.namespace", doc, resourceID(kind, doc), "metadata.namespace", "namespace "+namespace+" is not supported", "only the "+c.opts.Namespace+" namespace is supported")
		return false
	}
	return true
}

// compileConfigMap records a Config.
func (c *compiler) compileConfigMap(doc *yaml.Node) {
	if !c.checkNamespace(doc, "ConfigMap") {
		return
	}
	name := metadata(doc, "name")
	if name == "" {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.missing_name", doc, "ConfigMap", "metadata.name", "ConfigMap has no name", "")
		return
	}
	config := model.Config{Name: name}
	keys := map[string]bool{}
	if data, ok := mapGet(doc, "data"); ok {
		for i := 0; i+1 < len(data.Content); i += 2 {
			key := data.Content[i].Value
			config.Entries = append(config.Entries, model.ConfigEntry{Key: key, Text: data.Content[i+1].Value})
			keys[key] = true
		}
	}
	if data, ok := mapGet(doc, "binaryData"); ok {
		for i := 0; i+1 < len(data.Content); i += 2 {
			key := data.Content[i].Value
			decoded, err := base64.StdEncoding.DecodeString(data.Content[i+1].Value)
			if err != nil {
				c.add(source.SeverityError, source.Unsupported, "kubernetes.binary_data", data.Content[i+1], resourceID("ConfigMap", doc), "binaryData."+key, "invalid base64 in binaryData", "")
				continue
			}
			config.Entries = append(config.Entries, model.ConfigEntry{Key: key, Binary: decoded})
			keys[key] = true
		}
	}
	c.configKeys[name] = keys
	c.application.Configs = append(c.application.Configs, config)
	c.result.SourceMap.Set("config/"+name, spanValue(doc, c.opts.Path))
}

// compileSecret records secret material outside the public IR.
func (c *compiler) compileSecret(doc *yaml.Node) {
	if !c.checkNamespace(doc, "Secret") {
		return
	}
	name := metadata(doc, "name")
	if name == "" {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.missing_name", doc, "Secret", "metadata.name", "Secret has no name", "")
		return
	}
	if secretType := scalar(doc, "type"); secretType != "" && secretType != "Opaque" {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.secret_type", doc, resourceID("Secret", doc), "type", "unsupported Secret type "+secretType, "service-account and TLS Secret behavior is not implemented")
		return
	}
	data := map[string][]byte{}
	if encoded, ok := mapGet(doc, "data"); ok {
		for i := 0; i+1 < len(encoded.Content); i += 2 {
			key := encoded.Content[i].Value
			decoded, err := base64.StdEncoding.DecodeString(encoded.Content[i+1].Value)
			if err != nil {
				c.add(source.SeverityError, source.Unsupported, "kubernetes.secret_data", encoded.Content[i+1], resourceID("Secret", doc), "data."+key, "invalid base64 in Secret data", "")
				continue
			}
			data[key] = decoded
		}
	}
	// stringData takes precedence over data.
	if plain, ok := mapGet(doc, "stringData"); ok {
		for i := 0; i+1 < len(plain.Content); i += 2 {
			data[plain.Content[i].Value] = []byte(plain.Content[i+1].Value)
		}
	}
	keys := map[string]bool{}
	for key := range data {
		keys[key] = true
	}
	c.secretKeys[name] = keys
	version := secretVersion(data)
	c.result.Secrets = append(c.result.Secrets, SecretData{Name: name, Version: version, Data: data})
	c.application.Secrets = append(c.application.Secrets, model.SecretRef{Name: name, Version: version})
	c.result.SourceMap.Set("secret/"+name, spanValue(doc, c.opts.Path))
}

func (c *compiler) compilePVC(doc *yaml.Node) {
	if !c.checkNamespace(doc, "PersistentVolumeClaim") {
		return
	}
	name := metadata(doc, "name")
	volume := model.Volume{Name: name, Kind: model.VolumePVC}
	spec, ok := mapGet(doc, "spec")
	if !ok {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.pvc_spec", doc, resourceID("PersistentVolumeClaim", doc), "spec", "PVC has no spec", "")
		return
	}
	if modes, ok := mapGet(spec, "accessModes"); ok {
		for _, mode := range sequence(modes) {
			switch mode.Value {
			case "ReadWriteOnce":
				volume.AccessMode = "ReadWriteOnce"
			case "ReadOnlyMany", "ReadWriteMany", "ReadWriteOncePod":
				c.add(source.SeverityError, source.Unsupported, "kubernetes.pvc_access_mode", mode, resourceID("PersistentVolumeClaim", doc), "spec.accessModes", "access mode "+mode.Value+" is not supported", "only ReadWriteOnce is supported locally")
			default:
				c.add(source.SeverityError, source.Unsupported, "kubernetes.pvc_access_mode", mode, resourceID("PersistentVolumeClaim", doc), "spec.accessModes", "unknown access mode "+mode.Value, "")
			}
		}
	}
	if mode := scalar(spec, "volumeMode"); mode != "" && mode != "Filesystem" {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.pvc_volume_mode", spec, resourceID("PersistentVolumeClaim", doc), "spec.volumeMode", "volumeMode "+mode+" is not supported", "only Filesystem volumes are supported")
	}
	if resources, ok := mapGet(spec, "resources"); ok {
		if requests, ok := mapGet(resources, "requests"); ok {
			if storage, ok := scalarValue(requests, "storage"); ok {
				if bytes, err := model.ParseBytes(storage); err == nil {
					volume.Capacity = bytes
				} else {
					c.add(source.SeverityError, source.Unsupported, "kubernetes.pvc_storage", requests, resourceID("PersistentVolumeClaim", doc), "spec.resources.requests.storage", "invalid storage quantity "+storage, "")
				}
			}
		}
	}
	if class := scalar(spec, "storageClassName"); class != "" {
		c.result.Diagnostics = append(c.result.Diagnostics, diagnostic(source.SeverityWarning, source.ValidateOnly, "kubernetes.pvc_storage_class", spec, c.opts.Path, resourceID("PersistentVolumeClaim", doc), "spec.storageClassName", "storageClassName "+class+" is validated but local storage is always used", ""))
	}
	c.application.Volumes = append(c.application.Volumes, volume)
	c.result.SourceMap.Set("volume/"+name, spanValue(doc, c.opts.Path))
}

// compilePod compiles a bare Pod.
func (c *compiler) compilePod(doc *yaml.Node) {
	name := metadata(doc, "name")
	spec, ok := mapGet(doc, "spec")
	if !ok {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.pod_spec", doc, resourceID("Pod", doc), "spec", "Pod has no spec", "")
		return
	}
	workload := model.Workload{
		ID:       name,
		Kind:     model.WorkloadDeployment,
		Replicas: 1,
		Labels:   labelMap(doc),
	}
	template, ok := c.compilePodSpec(resourceID("Pod", doc), spec)
	if !ok {
		return
	}
	workload.Template = template
	workload.RestartPolicy = podRestartPolicy(scalar(spec, "restartPolicy"))
	c.application.Workloads = append(c.application.Workloads, workload)
	c.result.SourceMap.Set("workload/"+name, spanValue(doc, c.opts.Path))
}

// compileDeployment compiles a Deployment or a degraded DaemonSet.
func (c *compiler) compileDeployment(doc *yaml.Node) {
	kind := scalar(doc, "kind")
	name := metadata(doc, "name")
	resource := resourceID(kind, doc)
	spec, ok := mapGet(doc, "spec")
	if !ok {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.deployment_spec", doc, resource, "spec", kind+" has no spec", "")
		return
	}
	workload := model.Workload{ID: name, Kind: model.WorkloadDeployment, Replicas: 1}
	if replicas, ok := scalarValue(spec, "replicas"); ok {
		if parsed, err := strconv.Atoi(replicas); err == nil {
			workload.Replicas = int32(parsed)
		}
	}
	if kind == "DaemonSet" {
		workload.Replicas = 1
	}
	templateNode, ok := mapGet(spec, "template")
	if !ok {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.template", spec, resource, "spec.template", kind+" has no template", "")
		return
	}
	templateMeta, _ := mapGet(templateNode, "metadata")
	templateLabels := labelMap(templateMeta)
	workload.Labels = templateLabels
	if selector, ok := mapGet(spec, "selector"); ok {
		selectorLabels := stringMap(mapValue(selector, "matchLabels"))
		if len(selectorLabels) == 0 {
			c.add(source.SeverityError, source.Unsupported, "kubernetes.selector", selector, resource, "spec.selector.matchLabels", "selector has no matchLabels", "")
		} else if !subset(selectorLabels, templateLabels) {
			c.add(source.SeverityError, source.Unsupported, "kubernetes.selector_mismatch", selector, resource, "spec.selector", "selector does not match the template labels", "the workload would select nothing")
		}
	} else {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.selector", spec, resource, "spec.selector", kind+" has no selector", "")
	}
	// Update strategy: RollingUpdate is the Kubernetes default and is not
	// implemented; Grillo replaces sandboxes (Recreate).
	strategy := "RollingUpdate"
	if strategyNode, ok := mapGet(spec, "strategy"); ok {
		if value, ok := scalarValue(strategyNode, "type"); ok {
			strategy = value
		}
	}
	if strategy != "Recreate" {
		c.downgrade("kubernetes.rollout_strategy", spec, resource, "spec.strategy", "strategy "+strategy+" is not implemented", "Grillo replaces sandboxes (Recreate); in-place rolling updates are not faithful")
	}
	workload.UpdatePolicy = &model.UpdatePolicy{Strategy: "Recreate"}
	templateSpec, ok := mapGet(templateNode, "spec")
	if !ok {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.template_spec", templateNode, resource, "spec.template.spec", "template has no spec", "")
		return
	}
	compiled, ok := c.compilePodSpec(resource, templateSpec)
	if !ok {
		return
	}
	workload.Template = compiled
	workload.RestartPolicy = podRestartPolicy(scalar(templateSpec, "restartPolicy"))
	if workload.RestartPolicy == "" {
		workload.RestartPolicy = model.RestartAlways
	}
	c.application.Workloads = append(c.application.Workloads, workload)
	c.result.SourceMap.Set("workload/"+name, spanValue(doc, c.opts.Path))
}

// compilePodSpec compiles a shared PodSpec.
func (c *compiler) compilePodSpec(resource string, spec *yaml.Node) (model.SandboxTemplate, bool) {
	var template model.SandboxTemplate
	c.checkFields(podSpecFields, spec, resource, "spec", "")
	if privileged, ok := scalarValue(spec, "hostNetwork"); ok && privileged == "true" {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.host_network", spec, resource, "spec.hostNetwork", "hostNetwork is not supported", "the workload must stay inside the guest network")
		return template, false
	}
	for _, forbidden := range []string{"hostPID", "hostIPC"} {
		if value, ok := scalarValue(spec, forbidden); ok && value == "true" {
			c.add(source.SeverityError, source.Unsupported, "kubernetes.host_namespace", spec, resource, "spec."+forbidden, forbidden+" is not supported", "the workload must not share host namespaces")
			return template, false
		}
	}
	if hostname, ok := scalarValue(spec, "hostname"); ok {
		template.Hostname = hostname
	}
	if podSecurity, ok := mapGet(spec, "securityContext"); ok {
		c.applySecurity(&template.SecurityProfile, podSecurity, resource, "spec.securityContext")
	}
	if dns, ok := mapGet(spec, "dnsConfig"); ok {
		config := &model.DNSConfig{}
		if nameservers, ok := mapGet(dns, "nameservers"); ok {
			for _, server := range sequence(nameservers) {
				config.Nameservers = append(config.Nameservers, server.Value)
			}
		}
		if searches, ok := mapGet(dns, "searches"); ok {
			for _, search := range sequence(searches) {
				config.Search = append(config.Search, search.Value)
			}
		}
		template.DNS = config
	}
	if volumes, ok := mapGet(spec, "volumes"); ok {
		for _, volume := range sequence(volumes) {
			c.compileVolume(volume, resource, &template)
		}
	}
	if containers, ok := mapGet(spec, "containers"); ok {
		for _, container := range sequence(containers) {
			compiled, ok := c.compileContainer(container, resource, "spec.containers", &template.SecurityProfile)
			if !ok {
				return template, false
			}
			template.Containers = append(template.Containers, compiled)
		}
	}
	if len(template.Containers) == 0 {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.no_containers", spec, resource, "spec.containers", "no containers", "")
		return template, false
	}
	if initContainers, ok := mapGet(spec, "initContainers"); ok {
		for _, container := range sequence(initContainers) {
			compiled, ok := c.compileContainer(container, resource, "spec.initContainers", &template.SecurityProfile)
			if !ok {
				return template, false
			}
			template.InitContainers = append(template.InitContainers, compiled)
		}
	}
	return template, true
}

func (c *compiler) compileVolume(node *yaml.Node, resource string, template *model.SandboxTemplate) {
	c.checkFields(volumeSourceFields, node, resource, "spec.volumes", "")
	name := scalar(node, "name")
	if name == "" {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.volume_name", node, resource, "spec.volumes.name", "volume has no name", "")
		return
	}
	switch {
	case has(node, "emptyDir"):
		c.application.Volumes = append(c.application.Volumes, model.Volume{Name: name, Kind: model.VolumeEphemeral})
	case has(node, "persistentVolumeClaim"):
		claim, _ := mapGet(node, "persistentVolumeClaim")
		claimName := scalar(claim, "claimName")
		if claimName == "" {
			c.add(source.SeverityError, source.Unsupported, "kubernetes.pvc_ref", node, resource, "spec.volumes.persistentVolumeClaim.claimName", "persistentVolumeClaim has no claimName", "")
			return
		}
		if !c.hasVolume(claimName) {
			c.add(source.SeverityError, source.Unsupported, "kubernetes.pvc_missing", node, resource, "spec.volumes.persistentVolumeClaim.claimName", "claim "+claimName+" is not defined", "declare the PersistentVolumeClaim in the same input")
			return
		}
		c.application.Volumes = append(c.application.Volumes, model.Volume{Name: name, Kind: model.VolumePVC, Source: claimName})
	case has(node, "hostPath"):
		c.add(source.SeverityError, source.Unsupported, "kubernetes.host_path", node, resource, "spec.volumes.hostPath", "hostPath is not supported", "host paths are not exposed to workloads")
	default:
		c.add(source.SeverityError, source.Unsupported, "kubernetes.volume_source", node, resource, "spec.volumes", "unsupported volume source", "")
	}
	template.Volumes = append(template.Volumes, name)
}

func (c *compiler) compileContainer(node *yaml.Node, resource, field string, profile *model.SecurityProfile) (model.Container, bool) {
	c.checkFields(containerFields, node, resource, field, "")
	var container model.Container
	container.Name = scalar(node, "name")
	if container.Name == "" {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.container_name", node, resource, field+".name", "container has no name", "")
		return container, false
	}
	image := scalar(node, "image")
	if image == "" {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.container_image", node, resource, field+".image", "container "+container.Name+" has no image", "")
		return container, false
	}
	container.Image = model.ImageRef{Reference: image, PullPolicy: scalar(node, "imagePullPolicy")}
	if command, ok := mapGet(node, "command"); ok {
		values := stringList(command)
		container.Command = &values
	}
	if args, ok := mapGet(node, "args"); ok {
		values := stringList(args)
		container.Args = &values
	}
	if workingDir, ok := scalarValue(node, "workingDir"); ok {
		container.WorkingDir = workingDir
	}
	if env, ok := mapGet(node, "env"); ok {
		for _, variable := range sequence(env) {
			c.compileEnvVar(variable, &container, resource, field)
		}
	}
	if envFrom, ok := mapGet(node, "envFrom"); ok {
		for _, from := range sequence(envFrom) {
			c.compileEnvFrom(from, &container, resource, field)
		}
	}
	if mounts, ok := mapGet(node, "volumeMounts"); ok {
		for _, mount := range sequence(mounts) {
			compiled := model.VolumeMount{
				Volume:    scalar(mount, "name"),
				MountPath: scalar(mount, "mountPath"),
				ReadOnly:  scalar(mount, "readOnly") == "true",
				SubPath:   scalar(mount, "subPath"),
			}
			if compiled.Volume == "" || compiled.MountPath == "" {
				c.add(source.SeverityError, source.Unsupported, "kubernetes.volume_mount", mount, resource, field+".volumeMounts", "volume mount needs a name and mountPath", "")
				continue
			}
			container.Mounts = append(container.Mounts, compiled)
		}
	}
	if ports, ok := mapGet(node, "ports"); ok {
		for _, port := range sequence(ports) {
			protocol := scalar(port, "protocol")
			if protocol == "" {
				protocol = "TCP"
			}
			if protocol != "TCP" && protocol != "UDP" {
				c.add(source.SeverityError, source.Unsupported, "kubernetes.port_protocol", port, resource, field+".ports.protocol", "protocol "+protocol+" is not supported", "")
				continue
			}
			number, _ := strconv.Atoi(scalar(port, "containerPort"))
			container.Ports = append(container.Ports, model.ContainerPort{
				Name:          scalar(port, "name"),
				ContainerPort: int32(number),
				Protocol:      protocol,
			})
		}
	}
	if resources, ok := mapGet(node, "resources"); ok {
		container.Resources = parseResources(resources)
	}
	if security, ok := mapGet(node, "securityContext"); ok {
		c.applySecurity(profile, security, resource, field+".securityContext")
		if profile.Privileged {
			c.add(source.SeverityError, source.Unsupported, "kubernetes.privileged", security, resource, field+".securityContext.privileged", "privileged containers are not supported", "privileged execution is outside the MVP")
			return container, false
		}
		container.User = userString(*profile)
	}
	container.Probes = c.compileProbes(node, resource, field)
	return container, true
}

func (c *compiler) compileEnvVar(node *yaml.Node, container *model.Container, resource, field string) {
	name := scalar(node, "name")
	if name == "" {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.env_name", node, resource, field+".env.name", "env var has no name", "")
		return
	}
	if value, ok := scalarValue(node, "value"); ok {
		container.Env = append(container.Env, model.EnvVar{Name: name, Value: value})
		return
	}
	valueFrom, ok := mapGet(node, "valueFrom")
	if !ok {
		container.Env = append(container.Env, model.EnvVar{Name: name})
		return
	}
	if ref, ok := mapGet(valueFrom, "configMapKeyRef"); ok {
		configName := scalar(ref, "name")
		key := scalar(ref, "key")
		optional := scalar(ref, "optional") == "true"
		if !c.keyExists(c.configKeys, configName, key, optional, node, resource, field) {
			return
		}
		container.Env = append(container.Env, model.EnvVar{Name: name, ValueFrom: &model.EnvSource{ConfigRef: &model.ConfigKeyRef{Config: configName, Key: key}}})
		return
	}
	if ref, ok := mapGet(valueFrom, "secretKeyRef"); ok {
		secretName := scalar(ref, "name")
		key := scalar(ref, "key")
		optional := scalar(ref, "optional") == "true"
		if !c.keyExists(c.secretKeys, secretName, key, optional, node, resource, field) {
			return
		}
		container.Env = append(container.Env, model.EnvVar{Name: name, ValueFrom: &model.EnvSource{SecretRef: &model.SecretKeyRef{Secret: secretName, Key: key}}})
		return
	}
	c.add(source.SeverityError, source.Unsupported, "kubernetes.env_source", valueFrom, resource, field+".env.valueFrom", "unsupported env source", "only configMapKeyRef and secretKeyRef are supported")
}

func (c *compiler) compileEnvFrom(node *yaml.Node, container *model.Container, resource, field string) {
	optional := scalar(node, "optional") == "true"
	if ref, ok := mapGet(node, "configMapRef"); ok {
		name := scalar(ref, "name")
		keys, ok := c.configKeys[name]
		if !ok {
			if !optional {
				c.add(source.SeverityError, source.Unsupported, "kubernetes.env_from_missing", node, resource, field+".envFrom.configMapRef", "ConfigMap "+name+" is not defined", "")
			}
			return
		}
		for _, key := range sortedKeys(keys) {
			container.Env = append(container.Env, model.EnvVar{Name: key, ValueFrom: &model.EnvSource{ConfigRef: &model.ConfigKeyRef{Config: name, Key: key}}})
		}
		return
	}
	if ref, ok := mapGet(node, "secretRef"); ok {
		name := scalar(ref, "name")
		keys, ok := c.secretKeys[name]
		if !ok {
			if !optional {
				c.add(source.SeverityError, source.Unsupported, "kubernetes.env_from_missing", node, resource, field+".envFrom.secretRef", "Secret "+name+" is not defined", "")
			}
			return
		}
		for _, key := range sortedKeys(keys) {
			container.Env = append(container.Env, model.EnvVar{Name: key, ValueFrom: &model.EnvSource{SecretRef: &model.SecretKeyRef{Secret: name, Key: key}}})
		}
		return
	}
	c.add(source.SeverityError, source.Unsupported, "kubernetes.env_from", node, resource, field+".envFrom", "unsupported envFrom source", "")
}

func (c *compiler) compileProbes(node *yaml.Node, resource, field string) model.Probes {
	var probes model.Probes
	probes.Startup = c.compileProbe(node, "startupProbe", resource, field)
	probes.Readiness = c.compileProbe(node, "readinessProbe", resource, field)
	probes.Liveness = c.compileProbe(node, "livenessProbe", resource, field)
	return probes
}

func (c *compiler) compileProbe(node *yaml.Node, key, resource, field string) *model.Probe {
	probeNode, ok := mapGet(node, key)
	if !ok {
		return nil
	}
	probe := &model.Probe{
		InitialDelaySeconds: int32(atoi(scalar(probeNode, "initialDelaySeconds"))),
		TimeoutSeconds:      int32(atoi(scalar(probeNode, "timeoutSeconds"))),
		PeriodSeconds:       int32(atoi(scalar(probeNode, "periodSeconds"))),
		FailureThreshold:    int32(atoi(scalar(probeNode, "failureThreshold"))),
		SuccessThreshold:    int32(atoi(scalar(probeNode, "successThreshold"))),
	}
	switch {
	case has(probeNode, "exec"):
		exec, _ := mapGet(probeNode, "exec")
		command, _ := mapGet(exec, "command")
		probe.Kind = model.ProbeExec
		probe.Exec = &model.ExecAction{Command: stringList(command)}
	case has(probeNode, "httpGet"):
		httpGet, _ := mapGet(probeNode, "httpGet")
		probe.Kind = model.ProbeHTTP
		probe.HTTP = &model.HTTPAction{
			Path:   scalar(httpGet, "path"),
			Port:   parsePortRef(httpGet, "port"),
			Scheme: strings.ToUpper(scalar(httpGet, "scheme")),
			Host:   scalar(httpGet, "host"),
		}
	case has(probeNode, "tcpSocket"):
		tcpSocket, _ := mapGet(probeNode, "tcpSocket")
		probe.Kind = model.ProbeTCP
		probe.TCP = &model.TCPAction{Port: parsePortRef(tcpSocket, "port")}
	case has(probeNode, "grpc"):
		c.add(source.SeverityError, source.Unsupported, "kubernetes.grpc_probe", probeNode, resource, field+"."+key+".grpc", "gRPC probes are not supported", "")
		return nil
	default:
		c.add(source.SeverityError, source.Unsupported, "kubernetes.probe", probeNode, resource, field+"."+key, "probe has no supported action", "")
		return nil
	}
	return probe
}

func (c *compiler) compileService(doc *yaml.Node) {
	name := metadata(doc, "name")
	resource := resourceID("Service", doc)
	spec, ok := mapGet(doc, "spec")
	if !ok {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.service_spec", doc, resource, "spec", "Service has no spec", "")
		return
	}
	serviceType := scalar(spec, "type")
	if serviceType == "" {
		serviceType = "ClusterIP"
	}
	switch serviceType {
	case "ClusterIP":
	case "NodePort", "LoadBalancer", "ExternalName":
		c.add(source.SeverityError, source.Unsupported, "kubernetes.service_type", spec, resource, "spec.type", "Service type "+serviceType+" is not supported", "it is not silently mapped to ClusterIP")
		return
	default:
		c.add(source.SeverityError, source.Unsupported, "kubernetes.service_type", spec, resource, "spec.type", "unknown Service type "+serviceType, "")
		return
	}
	service := model.Service{Name: name, Selector: stringMap(mapValue(spec, "selector"))}
	if len(service.Selector) == 0 {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.service_selector", spec, resource, "spec.selector", "Service has no selector", "a selectorless Service has no endpoints")
		return
	}
	service.Headless = scalar(spec, "clusterIP") == "None"
	if ports, ok := mapGet(spec, "ports"); ok {
		for _, port := range sequence(ports) {
			protocol := scalar(port, "protocol")
			if protocol == "" {
				protocol = "TCP"
			}
			if protocol != "TCP" {
				c.add(source.SeverityError, source.Unsupported, "kubernetes.service_protocol", port, resource, "spec.ports.protocol", "protocol "+protocol+" is not supported", "only TCP ClusterIP is supported")
				continue
			}
			number := atoi(scalar(port, "port"))
			compiled := model.ServicePort{Name: scalar(port, "name"), Protocol: protocol, Port: int32(number)}
			if has(port, "targetPort") {
				ref := parsePortRef(port, "targetPort")
				compiled.TargetPort = &ref
			}
			service.Ports = append(service.Ports, compiled)
		}
	}
	c.application.Services = append(c.application.Services, service)
	c.result.SourceMap.Set("service/"+name, spanValue(doc, c.opts.Path))
}

func (c *compiler) compileIngress(doc *yaml.Node) {
	name := metadata(doc, "name")
	resource := resourceID("Ingress", doc)
	spec, ok := mapGet(doc, "spec")
	if !ok {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.ingress_spec", doc, resource, "spec", "Ingress has no spec", "")
		return
	}
	tlsHosts := map[string]string{}
	if tls, ok := mapGet(spec, "tls"); ok {
		for _, entry := range sequence(tls) {
			secret := scalar(entry, "secretName")
			if hosts, ok := mapGet(entry, "hosts"); ok {
				for _, host := range sequence(hosts) {
					tlsHosts[host.Value] = secret
				}
			}
		}
	}
	rules, ok := mapGet(spec, "rules")
	if !ok {
		c.add(source.SeverityError, source.Unsupported, "kubernetes.ingress_rules", spec, resource, "spec.rules", "Ingress has no rules", "")
		return
	}
	for _, rule := range sequence(rules) {
		host := scalar(rule, "host")
		http, ok := mapGet(rule, "http")
		if !ok {
			continue
		}
		paths, _ := mapGet(http, "paths")
		for _, path := range sequence(paths) {
			pathType := scalar(path, "pathType")
			switch pathType {
			case "Exact", "Prefix":
			case "ImplementationSpecific", "":
				c.add(source.SeverityError, source.Unsupported, "kubernetes.ingress_path_type", path, resource, "spec.rules.http.paths.pathType", "pathType "+pathType+" is not supported", "controller-specific path matching is not implemented")
				continue
			default:
				c.add(source.SeverityError, source.Unsupported, "kubernetes.ingress_path_type", path, resource, "spec.rules.http.paths.pathType", "unknown pathType "+pathType, "")
				continue
			}
			backend, _ := mapGet(path, "backend")
			serviceNode, _ := mapGet(backend, "service")
			serviceName := scalar(serviceNode, "name")
			if serviceName == "" {
				c.add(source.SeverityError, source.Unsupported, "kubernetes.ingress_backend", path, resource, "spec.rules.http.paths.backend", "Ingress backend has no Service", "")
				continue
			}
			route := model.Route{
				Hostname: host,
				Path:     scalar(path, "path"),
				PathType: pathType,
				Service:  serviceName,
				Port:     ingressPort(serviceNode),
			}
			if secret, ok := tlsHosts[host]; ok && secret != "" {
				route.TLSRef = &model.SecretKeyRef{Secret: secret}
			}
			c.application.Routes = append(c.application.Routes, route)
		}
	}
	c.result.SourceMap.Set("ingress/"+name, spanValue(doc, c.opts.Path))
}

func ingressPort(serviceNode *yaml.Node) *model.PortRef {
	port, ok := mapGet(serviceNode, "port")
	if !ok {
		return nil
	}
	if number, ok := scalarValue(port, "number"); ok {
		return &model.PortRef{Number: int32(atoi(number))}
	}
	if name, ok := scalarValue(port, "name"); ok {
		return &model.PortRef{Name: name}
	}
	return nil
}

// applySecurity copies the supported security-context fields.
func (c *compiler) applySecurity(profile *model.SecurityProfile, node *yaml.Node, resource, field string) {
	if value, ok := scalarValue(node, "runAsUser"); ok {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			profile.RunAsUser = &parsed
		}
	}
	if value, ok := scalarValue(node, "runAsGroup"); ok {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			profile.RunAsGroup = &parsed
		}
	}
	if value, ok := scalarValue(node, "readOnlyRootFilesystem"); ok && value == "true" {
		profile.ReadOnlyRootFilesystem = true
	}
	if value, ok := scalarValue(node, "privileged"); ok && value == "true" {
		profile.Privileged = true
	}
	if value, ok := scalarValue(node, "seccompProfile"); ok {
		profile.SeccompProfile = value
	}
	if capabilities, ok := mapGet(node, "capabilities"); ok {
		if add, ok := mapGet(capabilities, "add"); ok {
			profile.CapabilitiesAdd = stringList(add)
		}
		if drop, ok := mapGet(capabilities, "drop"); ok {
			profile.CapabilitiesDrop = stringList(drop)
		}
	}
}

// checkFields reports unknown or unsupported fields in a mapping.
func (c *compiler) checkFields(registry map[string]supportEntry, node *yaml.Node, resource, field, _ string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		entry, known := fieldSupport(registry, key)
		if !known {
			c.add(source.SeverityError, source.Unsupported, "kubernetes.unknown_field", node.Content[i], resource, field+"."+key, "unsupported field "+key, "this field has no Grillo mapping and blocks apply")
			continue
		}
		if entry.state == source.Unsupported {
			c.add(source.SeverityError, entry.state, "kubernetes.unsupported_field", node.Content[i], resource, field+"."+key, "unsupported field "+key, entry.consequence)
		} else if entry.state == source.Degraded {
			c.downgrade("kubernetes.degraded_"+key, node.Content[i], resource, field+"."+key, "field "+key+" is degraded", entry.consequence)
		}
	}
}

func (c *compiler) keyExists(registry map[string]map[string]bool, name, key string, optional bool, node *yaml.Node, resource, field string) bool {
	keys, ok := registry[name]
	if !ok {
		if optional {
			return false
		}
		c.add(source.SeverityError, source.Unsupported, "kubernetes.ref_missing", node, resource, field, "referenced object "+name+" is not defined", "declare it in the same input")
		return false
	}
	if !keys[key] {
		if optional {
			return false
		}
		c.add(source.SeverityError, source.Unsupported, "kubernetes.ref_key_missing", node, resource, field, "key "+key+" is not present in "+name, "")
		return false
	}
	return true
}

func (c *compiler) hasVolume(name string) bool {
	for _, volume := range c.application.Volumes {
		if volume.Name == name && volume.Kind == model.VolumePVC {
			return true
		}
	}
	return false
}

func (c *compiler) add(severity source.Severity, compatibility source.Compatibility, code string, node *yaml.Node, resource, field, message, consequence string) {
	c.result.Diagnostics = append(c.result.Diagnostics, diagnostic(severity, compatibility, code, node, c.opts.Path, resource, field, message, consequence))
}

// downgrade records a degraded feature, escalating to an error without consent.
func (c *compiler) downgrade(code string, node *yaml.Node, resource, field, message, consequence string) {
	severity := source.SeverityWarning
	if !c.allow[code] {
		severity = source.SeverityError
		consequence = consequence + "; pass --allow-degraded=" + code + " to accept the downgrade"
	}
	c.add(severity, source.Degraded, code, node, resource, field, message, consequence)
}

// Helpers.

func scalar(node *yaml.Node, key string) string {
	value, _ := scalarValue(node, key)
	return value
}

func has(node *yaml.Node, key string) bool {
	_, ok := mapGet(node, key)
	return ok
}

func metadata(doc *yaml.Node, key string) string {
	meta, ok := mapGet(doc, "metadata")
	if !ok {
		return ""
	}
	return scalar(meta, key)
}

func labelMap(node *yaml.Node) map[string]string {
	if node == nil {
		return nil
	}
	labels, ok := mapGet(node, "labels")
	if !ok {
		return nil
	}
	return stringMap(labels)
}

// stringMap reads a mapping node as string key/value pairs.
func stringMap(node *yaml.Node) map[string]string {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	out := map[string]string{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		out[node.Content[i].Value] = node.Content[i+1].Value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mapValue(node *yaml.Node, key string) *yaml.Node {
	value, _ := mapGet(node, key)
	return value
}

func resourceID(kind string, doc *yaml.Node) string {
	return kind + "/" + metadata(doc, "name")
}

func stringList(node *yaml.Node) []string {
	var out []string
	for _, item := range sequence(node) {
		out = append(out, item.Value)
	}
	return out
}

func parsePortRef(node *yaml.Node, key string) model.PortRef {
	value, ok := mapGet(node, key)
	if !ok || value.Kind != yaml.ScalarNode {
		return model.PortRef{}
	}
	if number, err := strconv.Atoi(value.Value); err == nil {
		return model.PortRef{Number: int32(number)}
	}
	return model.PortRef{Name: value.Value}
}

func parseResources(node *yaml.Node) model.Resources {
	var resources model.Resources
	if requests, ok := mapGet(node, "requests"); ok {
		resources.Requests = parseResourceList(requests)
	}
	if limits, ok := mapGet(node, "limits"); ok {
		resources.Limits = parseResourceList(limits)
	}
	return resources
}

func parseResourceList(node *yaml.Node) model.ResourceList {
	var list model.ResourceList
	if cpu, ok := scalarValue(node, "cpu"); ok {
		if parsed, err := model.ParseMillicores(cpu); err == nil {
			list.CPU = parsed
		}
	}
	if memory, ok := scalarValue(node, "memory"); ok {
		if parsed, err := model.ParseBytes(memory); err == nil {
			list.Memory = parsed
		}
	}
	return list
}

func podRestartPolicy(value string) model.RestartPolicy {
	switch value {
	case "Always":
		return model.RestartAlways
	case "OnFailure":
		return model.RestartOnFailure
	case "Never":
		return model.RestartNever
	default:
		return ""
	}
}

func subset(selector, labels map[string]string) bool {
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}

func userString(profile model.SecurityProfile) string {
	if profile.RunAsUser == nil {
		return ""
	}
	if profile.RunAsGroup == nil {
		return strconv.FormatInt(*profile.RunAsUser, 10)
	}
	return strconv.FormatInt(*profile.RunAsUser, 10) + ":" + strconv.FormatInt(*profile.RunAsGroup, 10)
}

func secretVersion(data map[string][]byte) string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	hasher := sha256.New()
	for _, key := range keys {
		hasher.Write([]byte(key))
		hasher.Write([]byte{0})
		hasher.Write(data[key])
		hasher.Write([]byte{0})
	}
	return hex.EncodeToString(hasher.Sum(nil))[:16]
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func atoi(value string) int {
	parsed, _ := strconv.Atoi(value)
	return parsed
}

func sanitizeName(name string) string {
	var builder strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteByte('-')
		}
	}
	if builder.Len() == 0 {
		return "kubernetes"
	}
	return builder.String()
}
