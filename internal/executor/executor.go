//go:build linux

// SPDX-License-Identifier: Apache-2.0

// Package executor wires the IR to the sandbox backend, storage manager, and
// guest agent. It implements the reconcile SandboxController and
// VolumeController contracts so a native manifest can be applied end to end.
package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"grillo.local/grillo/internal/guestproto"
	"grillo.local/grillo/internal/model"
	"grillo.local/grillo/internal/plan"
	"grillo.local/grillo/internal/sandbox"
	"grillo.local/grillo/internal/storage"
)

// Image is a resolved container root filesystem on the host.
type Image struct {
	HostPath string // host directory containing the rootfs
	Version  string // content identity, for cache/diagnostics
}

// ImageResolver resolves an image reference to a host directory.
type ImageResolver interface {
	Resolve(ctx context.Context, image model.ImageRef) (Image, error)
}

// Config configures the executor.
type Config struct {
	Backend      sandbox.Backend
	Volumes      *storage.Manager
	Kernel       string
	Initramfs    string
	KernelArgs   string
	GuestKey     []byte
	VsockPort    uint32
	VsockCIDBase uint32
	MemoryMiB    int
	VCPU         int
	Images       ImageResolver
	Dial         func(ctx context.Context, cid, port uint32) (*guestproto.Client, error)
}

// Executor applies sandbox and volume actions.
type Executor struct {
	cfg Config

	mu       sync.Mutex
	desired  map[string]model.Application
	runtimes map[string]*sandboxRuntime // by backend sandbox ID
	nextCID  uint32
}

type sandboxRuntime struct {
	id    string
	app   string
	cid   uint32
	guest *guestproto.Client
}

// New returns an executor.
func New(cfg Config) (*Executor, error) {
	if cfg.Backend == nil {
		return nil, errors.New("executor: backend is required")
	}
	if cfg.Images == nil {
		return nil, errors.New("executor: image resolver is required")
	}
	if cfg.Dial == nil {
		key := cfg.GuestKey
		cfg.Dial = func(ctx context.Context, cid, port uint32) (*guestproto.Client, error) {
			conn, err := guestproto.DialVsock(ctx, cid, port)
			if err != nil {
				return nil, err
			}
			return guestproto.NewClient(ctx, conn, guestproto.HandshakeConfig{Key: key, Agent: "executor"})
		}
	}
	if cfg.VsockCIDBase == 0 {
		cfg.VsockCIDBase = 3
	}
	if cfg.MemoryMiB == 0 {
		cfg.MemoryMiB = 512
	}
	return &Executor{
		cfg:      cfg,
		desired:  map[string]model.Application{},
		runtimes: map[string]*sandboxRuntime{},
		nextCID:  cfg.VsockCIDBase,
	}, nil
}

// SetDesired records the desired application so Ensure can build its specs.
func (e *Executor) SetDesired(app model.Application) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.desired[app.Identity.Name] = app
}

// PrepareVolume creates an owned volume.
func (e *Executor) PrepareVolume(ctx context.Context, application, volume string) error {
	if e.cfg.Volumes == nil {
		return nil
	}
	spec, _, err := e.volumeSpec(application, volume)
	if err != nil {
		return err
	}
	_, err = e.cfg.Volumes.Create(ctx, spec, "prepare-"+spec.ID)
	return err
}

// DeleteVolume deletes an owned volume. Bind volumes are refused by storage.
func (e *Executor) DeleteVolume(ctx context.Context, application, volume string) error {
	if e.cfg.Volumes == nil {
		return nil
	}
	return e.cfg.Volumes.Delete(ctx, volumeID(application, volume))
}

// Drain is a no-op until endpoints are attached.
func (e *Executor) Drain(context.Context, string, plan.Descriptor) error { return nil }

// Ensure creates and starts a sandbox and its containers.
func (e *Executor) Ensure(ctx context.Context, application string, descriptor plan.Descriptor) error {
	e.mu.Lock()
	app, ok := e.desired[application]
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("executor: no desired application %q", application)
	}
	workload := findWorkload(app, descriptor.Workload)
	if workload == nil {
		return fmt.Errorf("executor: workload %q not found", descriptor.Workload)
	}
	spec, guestSpec, err := e.buildSpecs(ctx, application, app, *workload, descriptor)
	if err != nil {
		return err
	}
	if _, err := e.cfg.Backend.Create(ctx, spec, sandbox.OperationID("ensure-"+spec.ID)); err != nil {
		return err
	}
	if err := e.cfg.Backend.Start(ctx, sandbox.Handle{ID: spec.ID}); err != nil {
		return err
	}
	client, err := e.cfg.Dial(ctx, spec.VsockCID, spec.VsockPort)
	if err != nil {
		_ = e.cfg.Backend.Stop(ctx, sandbox.Handle{ID: spec.ID}, 0)
		return fmt.Errorf("executor: connect guest: %w", err)
	}
	startCtx := ctx
	if _, err := client.Start(startCtx, guestproto.StartRequest{Sandbox: &guestSpec}); err != nil {
		_ = client.Close()
		_ = e.cfg.Backend.Stop(ctx, sandbox.Handle{ID: spec.ID}, 0)
		return fmt.Errorf("executor: start containers: %w", err)
	}
	e.mu.Lock()
	e.runtimes[spec.ID] = &sandboxRuntime{id: spec.ID, app: application, cid: spec.VsockCID, guest: client}
	e.mu.Unlock()
	return nil
}

// Stop stops a sandbox's containers and the sandbox itself.
func (e *Executor) Stop(ctx context.Context, application string, descriptor plan.Descriptor) error {
	sandboxID := application + "-" + descriptor.ID
	e.mu.Lock()
	rt := e.runtimes[sandboxID]
	delete(e.runtimes, sandboxID)
	e.mu.Unlock()
	if rt != nil && rt.guest != nil {
		_, _ = rt.guest.Stop(ctx, guestproto.StopRequest{})
		_ = rt.guest.Close()
	}
	if err := e.cfg.Backend.Stop(ctx, sandbox.Handle{ID: sandboxID}, 0); err != nil {
		return err
	}
	return e.detachVolumes(ctx, application, sandboxID)
}

// DeleteSandbox stops and deletes a sandbox.
func (e *Executor) DeleteSandbox(ctx context.Context, application string, descriptor plan.Descriptor) error {
	sandboxID := application + "-" + descriptor.ID
	if err := e.Stop(ctx, application, descriptor); err != nil {
		return err
	}
	return e.cfg.Backend.Delete(ctx, sandbox.Handle{ID: sandboxID})
}

// ContainerState is the observed state of one container.
type ContainerState struct {
	Sandbox   string
	Container string
	State     string
	ExitCode  int
}

// Status aggregates container state across an application's sandboxes.
func (e *Executor) Status(ctx context.Context, application string) ([]ContainerState, error) {
	e.mu.Lock()
	var clients []*guestproto.Client
	for _, rt := range e.runtimes {
		if rt.app == application && rt.guest != nil {
			clients = append(clients, rt.guest)
		}
	}
	e.mu.Unlock()
	var states []ContainerState
	for _, client := range clients {
		status, err := client.Status(ctx)
		if err != nil {
			continue
		}
		for _, container := range status.Containers {
			states = append(states, ContainerState{Container: container.Name, State: container.State, ExitCode: container.ExitCode})
		}
	}
	return states, nil
}

// Exec runs a command in a container of an application.
func (e *Executor) Exec(ctx context.Context, application, container string, args []string, stdout, stderr io.Writer) (int, error) {
	e.mu.Lock()
	var clients []*guestproto.Client
	for _, rt := range e.runtimes {
		if rt.app == application && rt.guest != nil {
			clients = append(clients, rt.guest)
		}
	}
	e.mu.Unlock()
	for _, client := range clients {
		result, err := client.Exec(ctx, guestproto.ExecRequest{Container: container, Args: args}, stdout, stderr)
		if err != nil {
			return 0, err
		}
		return result.ExitCode, nil
	}
	return 0, fmt.Errorf("executor: no running sandbox for application %q", application)
}

func (e *Executor) buildSpecs(ctx context.Context, application string, app model.Application, workload model.Workload, descriptor plan.Descriptor) (sandbox.Spec, guestproto.SandboxSpec, error) {
	sandboxID := application + "-" + descriptor.ID
	e.mu.Lock()
	cid := e.nextCID
	e.nextCID++
	e.mu.Unlock()

	spec := sandbox.Spec{
		ID:         sandboxID,
		Kernel:     e.cfg.Kernel,
		Initramfs:  e.cfg.Initramfs,
		KernelArgs: e.cfg.KernelArgs,
		VCPU:       e.cfg.VCPU,
		MemoryMiB:  e.cfg.MemoryMiB,
		VsockCID:   cid,
		VsockPort:  e.cfg.VsockPort,
		GuestKey:   e.cfg.GuestKey,
	}
	guestSpec := guestproto.SandboxSpec{ID: sandboxID, Hostname: workload.Template.Hostname}

	// Volumes referenced by this template.
	volumeTargets := map[string]string{}
	for _, name := range referencedVolumes(workload) {
		volume, ok := findVolume(app, name)
		if !ok {
			return sandbox.Spec{}, guestproto.SandboxSpec{}, fmt.Errorf("executor: volume %q not found", name)
		}
		source, readOnly, err := e.attachVolume(ctx, application, app, volume, sandboxID)
		if err != nil {
			return sandbox.Spec{}, guestproto.SandboxSpec{}, err
		}
		tag := "vol-" + name
		target := "/run/grillo/volumes/" + name
		spec.Shares = append(spec.Shares, sandbox.Share{Tag: tag, HostPath: source, ReadOnly: readOnly})
		guestSpec.Shares = append(guestSpec.Shares, guestproto.ShareSpec{Tag: tag, Target: target, ReadOnly: readOnly})
		volumeTargets[name] = target
	}

	containers := append(append([]model.Container{}, workload.Template.InitContainers...), workload.Template.Containers...)
	for i := range containers {
		container := &containers[i]
		image, err := e.cfg.Images.Resolve(ctx, container.Image)
		if err != nil {
			return sandbox.Spec{}, guestproto.SandboxSpec{}, fmt.Errorf("executor: resolve image %q: %w", container.Image.Reference, err)
		}
		tag := "rootfs-" + container.Name
		target := "/run/grillo/rootfs/" + container.Name
		spec.Shares = append(spec.Shares, sandbox.Share{Tag: tag, HostPath: image.HostPath})
		guestSpec.Shares = append(guestSpec.Shares, guestproto.ShareSpec{Tag: tag, Target: target})
		containerSpec, err := e.containerSpec(app, *container, target, volumeTargets)
		if err != nil {
			return sandbox.Spec{}, guestproto.SandboxSpec{}, err
		}
		containersCopy := containerSpec
		containersCopy.Init = i < len(workload.Template.InitContainers)
		guestSpec.Containers = append(guestSpec.Containers, containersCopy)
	}
	guestSpec.DNS = dnsConfig(app, workload)
	if guestSpec.DNS != nil {
		guestSpec.Nameservers = []string{"127.0.0.1"}
	}
	return spec, guestSpec, nil
}

// dnsConfig builds the guest resolver records for Services selecting this
// workload. In the single-guest model a service resolves to the guest itself.
func dnsConfig(app model.Application, workload model.Workload) *guestproto.DNSConfig {
	var records []guestproto.DNSRecord
	for _, service := range app.Services {
		if !selectorMatches(service.Selector, workload.Labels) {
			continue
		}
		records = append(records, guestproto.DNSRecord{Name: service.Name, Namespace: app.Identity.Namespace, IPs: []string{"127.0.0.1"}})
	}
	if len(records) == 0 {
		return nil
	}
	namespace := app.Identity.Namespace
	return &guestproto.DNSConfig{
		ClusterDomain: "cluster.local",
		Search:        []string{namespace + ".svc.cluster.local", "svc.cluster.local", "cluster.local"},
		Records:       records,
	}
}

func selectorMatches(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return true
	}
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}

func (e *Executor) containerSpec(app model.Application, container model.Container, rootfs string, volumeTargets map[string]string) (guestproto.ContainerSpec, error) {
	args := containerArgs(container)
	if len(args) == 0 {
		return guestproto.ContainerSpec{}, fmt.Errorf("executor: container %q has no command", container.Name)
	}
	env, err := resolveEnv(app, container)
	if err != nil {
		return guestproto.ContainerSpec{}, err
	}
	user, err := parseUser(container.User)
	if err != nil {
		return guestproto.ContainerSpec{}, err
	}
	mounts := make([]guestproto.MountSpec, 0, len(container.Mounts))
	for _, mount := range container.Mounts {
		source, ok := volumeTargets[mount.Volume]
		if !ok {
			return guestproto.ContainerSpec{}, fmt.Errorf("executor: container %q mounts volume %q that is not attached", container.Name, mount.Volume)
		}
		mounts = append(mounts, guestproto.MountSpec{Source: source, Target: mount.MountPath, ReadOnly: mount.ReadOnly})
	}
	return guestproto.ContainerSpec{
		Name:       container.Name,
		Rootfs:     rootfs,
		Args:       args,
		Env:        env,
		WorkingDir: container.WorkingDir,
		User:       user,
		Mounts:     mounts,
		Resources:  resources(container.Resources),
	}, nil
}

func referencedVolumes(workload model.Workload) []string {
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	for _, name := range workload.Template.Volumes {
		add(name)
	}
	containers := append(append([]model.Container{}, workload.Template.InitContainers...), workload.Template.Containers...)
	for _, container := range containers {
		for _, mount := range container.Mounts {
			add(mount.Volume)
		}
	}
	return names
}

func (e *Executor) attachVolume(ctx context.Context, application string, app model.Application, volume model.Volume, sandboxID string) (string, bool, error) {
	if e.cfg.Volumes == nil {
		return "", false, fmt.Errorf("executor: storage is not configured")
	}
	spec, readOnly, err := e.volumeSpecFor(application, volume)
	if err != nil {
		return "", false, err
	}
	if _, err := e.cfg.Volumes.Create(ctx, spec, "prepare-"+spec.ID); err != nil {
		return "", false, err
	}
	attachment, err := e.cfg.Volumes.Attach(ctx, spec.ID, sandboxID, readOnly, 0, 0, "attach-"+spec.ID)
	if err != nil {
		return "", false, err
	}
	return attachment.Source, attachment.ReadOnly, nil
}

func (e *Executor) volumeSpec(application, name string) (storage.Volume, bool, error) {
	e.mu.Lock()
	app, ok := e.desired[application]
	e.mu.Unlock()
	if !ok {
		return storage.Volume{}, false, fmt.Errorf("executor: no desired application %q", application)
	}
	volume, ok := findVolume(app, name)
	if !ok {
		return storage.Volume{}, false, fmt.Errorf("executor: volume %q not found", name)
	}
	return e.volumeSpecFor(application, volume)
}

func (e *Executor) volumeSpecFor(application string, volume model.Volume) (storage.Volume, bool, error) {
	kind := storage.KindManaged
	switch volume.Kind {
	case model.VolumeBind:
		kind = storage.KindBind
	case model.VolumeEphemeral:
		kind = storage.KindEphemeral
	case model.VolumePVC:
		kind = storage.KindPVC
	}
	accessMode := storage.AccessReadWriteOnce
	if volume.AccessMode == "ReadOnlyMany" {
		accessMode = storage.AccessReadOnlyMany
	}
	return storage.Volume{
		ID:            volumeID(application, volume.Name),
		Name:          volume.Name,
		Kind:          kind,
		Source:        volume.Source,
		ReadOnly:      volume.ReadOnly,
		AccessMode:    accessMode,
		CapacityBytes: int64(volume.Capacity),
		Owner:         storage.Owner{Application: application},
	}, volume.ReadOnly, nil
}

func (e *Executor) detachVolumes(ctx context.Context, application, sandboxID string) error {
	if e.cfg.Volumes == nil {
		return nil
	}
	e.mu.Lock()
	app, ok := e.desired[application]
	e.mu.Unlock()
	if !ok {
		return nil
	}
	for _, name := range referencedVolumesForApp(app) {
		_ = e.cfg.Volumes.Detach(ctx, volumeID(application, name), sandboxID)
	}
	return nil
}

func referencedVolumesForApp(app model.Application) []string {
	seen := map[string]bool{}
	var names []string
	for _, workload := range app.Workloads {
		for _, name := range referencedVolumes(workload) {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names
}

func findWorkload(app model.Application, id string) *model.Workload {
	for i := range app.Workloads {
		if app.Workloads[i].ID == id {
			return &app.Workloads[i]
		}
	}
	return nil
}

func findVolume(app model.Application, name string) (model.Volume, bool) {
	for _, volume := range app.Volumes {
		if volume.Name == name {
			return volume, true
		}
	}
	return model.Volume{}, false
}

func containerArgs(container model.Container) []string {
	var args []string
	if container.Command != nil {
		args = append(args, (*container.Command)...)
	}
	if container.Args != nil {
		args = append(args, (*container.Args)...)
	}
	return args
}

func resolveEnv(app model.Application, container model.Container) ([]string, error) {
	var env []string
	for _, variable := range container.Env {
		if variable.ValueFrom == nil {
			env = append(env, variable.Name+"="+variable.Value)
			continue
		}
		if variable.ValueFrom.ConfigRef != nil {
			text, ok := configText(app, variable.ValueFrom.ConfigRef.Config, variable.ValueFrom.ConfigRef.Key)
			if !ok {
				return nil, fmt.Errorf("executor: container %q env %q references missing config %q", container.Name, variable.Name, variable.ValueFrom.ConfigRef.Config)
			}
			env = append(env, variable.Name+"="+text)
			continue
		}
		return nil, fmt.Errorf("executor: container %q env %q uses an unsupported source", container.Name, variable.Name)
	}
	return env, nil
}

func configText(app model.Application, name, key string) (string, bool) {
	for _, config := range app.Configs {
		if config.Name != name {
			continue
		}
		for _, entry := range config.Entries {
			if entry.Key == key {
				return entry.Text, true
			}
		}
	}
	return "", false
}

func parseUser(user string) (guestproto.UserSpec, error) {
	if user == "" {
		return guestproto.UserSpec{}, nil
	}
	uidPart, gidPart, _ := strings.Cut(user, ":")
	uid, err := strconv.Atoi(uidPart)
	if err != nil {
		return guestproto.UserSpec{}, fmt.Errorf("executor: invalid user %q", user)
	}
	gid := uid
	if gidPart != "" {
		gid, err = strconv.Atoi(gidPart)
		if err != nil {
			return guestproto.UserSpec{}, fmt.Errorf("executor: invalid user %q", user)
		}
	}
	return guestproto.UserSpec{UID: uint32(uid), GID: uint32(gid)}, nil
}

func resources(r model.Resources) guestproto.ResourceSpec {
	spec := guestproto.ResourceSpec{}
	if r.Limits.CPU > 0 {
		spec.CPUQuotaMicros = int64(r.Limits.CPU) * 1000
		spec.CPUPeriodMicros = 100000
	}
	if r.Limits.Memory > 0 {
		spec.MemoryBytes = int64(r.Limits.Memory)
	}
	return spec
}

func volumeID(application, name string) string {
	return application + "-" + name
}

// SandboxController adapts an Executor to reconcile.SandboxController.
type SandboxController struct{ E *Executor }

// Ensure creates and starts a sandbox.
func (s SandboxController) Ensure(ctx context.Context, application string, d plan.Descriptor) error {
	return s.E.Ensure(ctx, application, d)
}

// Drain is a no-op until endpoints are attached.
func (s SandboxController) Drain(context.Context, string, plan.Descriptor) error { return nil }

// Stop stops a sandbox.
func (s SandboxController) Stop(ctx context.Context, application string, d plan.Descriptor) error {
	return s.E.Stop(ctx, application, d)
}

// Delete removes a sandbox.
func (s SandboxController) Delete(ctx context.Context, application string, d plan.Descriptor) error {
	return s.E.DeleteSandbox(ctx, application, d)
}

// VolumeController adapts an Executor to reconcile.VolumeController.
type VolumeController struct{ E *Executor }

// Prepare creates an owned volume.
func (v VolumeController) Prepare(ctx context.Context, application, volume string) error {
	return v.E.PrepareVolume(ctx, application, volume)
}

// Delete removes an owned volume.
func (v VolumeController) Delete(ctx context.Context, application, volume string) error {
	return v.E.DeleteVolume(ctx, application, volume)
}
