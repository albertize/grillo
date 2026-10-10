//go:build linux

// SPDX-License-Identifier: Apache-2.0

// Package executor wires the IR to the sandbox backend, storage manager, and
// guest agent. It implements the reconcile SandboxController and
// VolumeController contracts so a native manifest can be applied end to end.
package executor

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/albertize/grillo/internal/guestproto"
	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/netns"
	"github.com/albertize/grillo/internal/network"
	"github.com/albertize/grillo/internal/observe"
	"github.com/albertize/grillo/internal/plan"
	"github.com/albertize/grillo/internal/sandbox"
	"github.com/albertize/grillo/internal/storage"
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
	FreshKeys    bool
	VsockPort    uint32
	VsockCIDBase uint32
	MemoryMiB    int
	VCPU         int
	Images       ImageResolver
	Logs         *observe.LogSpool
	Dial         func(ctx context.Context, cid, port uint32) (*guestproto.Client, error)

	// Secrets resolves Secret-backed environment variables. When nil, a
	// container that references a Secret fails with a clear error.
	Secrets SecretResolver

	// Networking (optional). When EnableNetwork is set, each application runs a
	// network supervisor and sandboxes receive IPAM addresses on its bridge.
	EnableNetwork bool
	QEMU          string
	NetnsBinary   string
	Pasta         string
	RuntimeDir    string
}

// Executor applies sandbox and volume actions.
type Executor struct {
	cfg Config

	mu       sync.Mutex
	desired  map[string]model.Application
	runtimes map[string]*sandboxRuntime // by backend sandbox ID
	nextCID  uint32

	launchMu      sync.Mutex
	supervisors   map[string]*netns.Helper
	ipams         map[string]*network.IPAM
	networkMu     sync.Mutex
	ingresses     map[string]map[string]*ingressServer
	ingressSlots  chan struct{}
	networks      map[string]networkSnapshot
	networkErrors map[string]error
	ports         *network.Ports
	published     map[string]map[int]*publishedPort
}

type sandboxRuntime struct {
	id         string
	app        string
	cid        uint32
	ip         string
	guest      *guestproto.Client
	guestKey   []byte
	runner     *observe.Runner
	cancel     context.CancelFunc
	containers map[string]bool
	workload   string
	ready      bool
	draining   bool
	logState   *guestLogState
	logFailed  bool // guarded by Executor.mu
}

// New returns an executor.
func New(cfg Config) (*Executor, error) {
	if cfg.Backend == nil {
		return nil, errors.New("executor: backend is required")
	}
	if cfg.Images == nil {
		return nil, errors.New("executor: image resolver is required")
	}
	if cfg.FreshKeys {
		if cfg.Dial != nil {
			return nil, errors.New("executor: custom dial cannot use fresh runtime keys")
		}
		if _, ok := cfg.Backend.(interface {
			BootConnection(sandbox.Handle) (uint32, uint32, []byte, error)
		}); !ok {
			return nil, errors.New("executor: backend cannot provide boot credentials")
		}
	}
	if cfg.VsockCIDBase == 0 {
		cfg.VsockCIDBase = 3
	}
	if cfg.MemoryMiB == 0 {
		cfg.MemoryMiB = 512
	}
	return &Executor{
		cfg:           cfg,
		desired:       map[string]model.Application{},
		runtimes:      map[string]*sandboxRuntime{},
		supervisors:   map[string]*netns.Helper{},
		ipams:         map[string]*network.IPAM{},
		nextCID:       cfg.VsockCIDBase,
		ingresses:     map[string]map[string]*ingressServer{},
		ingressSlots:  make(chan struct{}, 128),
		networks:      map[string]networkSnapshot{},
		networkErrors: map[string]error{},
		published:     map[string]map[int]*publishedPort{},
	}, nil
}

func (e *Executor) dialGuest(ctx context.Context, cid, port uint32, key []byte) (*guestproto.Client, error) {
	if e.cfg.Dial != nil {
		return e.cfg.Dial(ctx, cid, port)
	}
	conn, err := guestproto.DialVsock(ctx, cid, port)
	if err != nil {
		return nil, err
	}
	return guestproto.NewClient(ctx, conn, guestproto.HandshakeConfig{Key: key, Agent: "executor"})
}

// SetDesired records the desired application so Ensure can build its specs.
func (e *Executor) SetDesired(app model.Application) {
	e.mu.Lock()
	defer e.mu.Unlock()
	app.Routes = append([]model.Route(nil), app.Routes...)
	previous := e.desired[app.Identity.Name]
	for i := range app.Routes {
		for _, route := range previous.Routes {
			if route.Hostname == app.Routes[i].Hostname {
				app.Routes[i].Endpoint = route.Endpoint
				break
			}
		}
	}
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

// Drain excludes a sandbox from all new Service/Ingress connections.
func (e *Executor) Drain(ctx context.Context, application string, d plan.Descriptor) error {
	return e.drainEndpoints(ctx, application, d)
}

// Ensure creates and starts a sandbox and its containers.
func (e *Executor) Ensure(ctx context.Context, application string, descriptor plan.Descriptor) error {
	e.mu.Lock()
	app, ok := e.desired[application]
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("executor: no desired application %q", application)
	}
	if diagnostics := e.ValidateApplication(app); diagnostics.HasErrors() {
		return fmt.Errorf("executor: invalid application: %s", diagnostics.Errors()[0].Message)
	}
	if e.cfg.EnableNetwork {
		// Reserve actual host listeners before starting any VM. Endpoint
		// snapshots remain empty until observed startup/readiness succeeds.
		if err := e.UpdateEndpoints(ctx, application); err != nil {
			return err
		}
	}
	workload := findWorkload(app, descriptor.Workload)
	if workload == nil {
		return fmt.Errorf("executor: workload %q not found", descriptor.Workload)
	}
	if err := e.waitStartupDependencies(ctx, application, app, *workload); err != nil {
		return err
	}
	spec, guestSpec, err := e.buildSpecs(ctx, application, app, *workload, descriptor)
	if err != nil {
		return err
	}
	ip := ""
	if e.cfg.EnableNetwork {
		if allocated, allocErr := e.allocateIP(application, descriptor.ID); allocErr == nil {
			ip = allocated
		}
	}
	if _, err := e.cfg.Backend.Create(ctx, spec, sandbox.OperationID("ensure-"+spec.ID)); err != nil {
		return err
	}
	if err := e.cfg.Backend.Start(ctx, sandbox.Handle{ID: spec.ID}); err != nil {
		return err
	}
	if e.cfg.FreshKeys {
		provider, ok := e.cfg.Backend.(interface {
			BootConnection(sandbox.Handle) (uint32, uint32, []byte, error)
		})
		if !ok {
			return fmt.Errorf("executor: backend cannot provide boot credentials")
		}
		var err error
		spec.VsockCID, spec.VsockPort, spec.GuestKey, err = provider.BootConnection(sandbox.Handle{ID: spec.ID})
		if err != nil {
			return err
		}
	}
	client, err := e.dialGuest(ctx, spec.VsockCID, spec.VsockPort, spec.GuestKey)
	if err != nil {
		_ = e.cfg.Backend.Stop(ctx, sandbox.Handle{ID: spec.ID}, 0)
		return fmt.Errorf("executor: connect guest: %w", err)
	}
	if e.cfg.EnableNetwork && !client.Info().Supports(guestproto.CapApplicationDNS) {
		_ = client.Close()
		_ = e.cfg.Backend.Stop(ctx, sandbox.Handle{ID: spec.ID}, 0)
		return fmt.Errorf("executor: guest agent lacks application DNS support; rebuild the guest image")
	}
	if e.cfg.Logs != nil && !supportsLogs(client) {
		_ = client.Close()
		_ = e.cfg.Backend.Stop(ctx, sandbox.Handle{ID: spec.ID}, 0)
		return fmt.Errorf("executor: guest agent lacks log support; rebuild the guest image")
	}
	names := make(map[string]bool)
	for _, c := range allContainers(*workload) {
		names[c.Name] = true
	}
	rt := &sandboxRuntime{id: spec.ID, app: application, cid: spec.VsockCID, ip: ip, guest: client, guestKey: spec.GuestKey, containers: names, workload: workload.ID, logState: &guestLogState{}}
	if _, err := client.Start(ctx, guestproto.StartRequest{Sandbox: &guestSpec}); err != nil {
		// Preserve failing init output in the ordinary workload log channel,
		// never splice it into public operation errors. Cleanup stays bounded.
		logCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		e.drainLogs(logCtx, rt)
		stop()
		_ = client.Close()
		_ = e.cfg.Backend.Stop(ctx, sandbox.Handle{ID: spec.ID}, 0)
		return fmt.Errorf("executor: start containers: %w", err)
	}
	e.mu.Lock()
	e.runtimes[spec.ID] = rt
	e.mu.Unlock()
	if err := e.startHealth(ctx, rt, *workload); err != nil {
		return err
	}
	return e.UpdateEndpoints(ctx, application)
}

// startHealth gates Pod endpoints on every regular container's running,
// startup and readiness state. No-probe containers are observed too, so exit
// removes them rather than leaving a permanently healthy endpoint.
func (e *Executor) startHealth(initial context.Context, rt *sandboxRuntime, workload model.Workload) error {
	runner := observe.NewRunner(observe.RealClock{}, observe.GuestProber{Client: rt.guest}, nil)
	for _, container := range workload.Template.Containers {
		cfg := probeConfig(container)
		if cfg.Startup != nil || cfg.Readiness != nil || cfg.Liveness != nil {
			if err := runner.Set(container.Name, cfg); err != nil {
				return err
			}
		}
	}
	lifetime, cancel := context.WithCancel(context.Background())
	runner.OnLivenessFailure = func(container string) {
		ctx, stop := context.WithTimeout(lifetime, 5*time.Second)
		defer stop()
		_, _ = rt.guest.Restart(ctx, container)
	}
	e.mu.Lock()
	rt.runner = runner
	rt.cancel = cancel
	e.mu.Unlock()
	poll := func(ctx context.Context) (bool, error) {
		runner.Tick(ctx)
		status, err := rt.guest.Status(ctx)
		var logErr error
		if err == nil {
			logErr = e.collectLogs(ctx, rt)
		}
		ready := err == nil
		states := map[string]string{}
		if err == nil {
			for _, container := range status.Containers {
				states[container.Name] = container.State
			}
		}
		for _, container := range workload.Template.Containers {
			if states[container.Name] != "running" {
				ready = false
			}
			if health, ok := runner.Status(container.Name); ok {
				if !health.StartupDone || health.Failed || container.Probes.Readiness != nil && !health.Ready {
					ready = false
				}
			}
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.runtimes[rt.id] != rt {
			return false, err
		}
		changed := rt.ready != ready
		rt.ready = ready
		rt.logFailed = logErr != nil
		return changed, err
	}
	ctx, stop := context.WithTimeout(initial, 15*time.Second)
	_, err := poll(ctx)
	stop()
	if err != nil {
		cancel()
		return err
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-lifetime.Done():
				return
			case <-ticker.C:
				ctx, stop := context.WithTimeout(lifetime, 15*time.Second)
				changed, err := poll(ctx)
				if e.cfg.EnableNetwork && lifetime.Err() == nil {
					statusErr := err
					e.mu.Lock()
					retry := e.networkErrors[rt.app] != nil
					e.mu.Unlock()
					if !changed && !retry {
						err = (&netns.Client{SocketPath: e.socketPath(rt.app)}).PingContext(ctx)
					}
					if changed || retry {
						err = e.UpdateEndpoints(ctx, rt.app)
					}
					if statusErr != nil {
						err = fmt.Errorf("executor: guest status unavailable")
					}
					if err != nil && lifetime.Err() == nil {
						e.mu.Lock()
						e.networkErrors[rt.app] = err
						e.mu.Unlock()
					}
				}
				stop()
			}
		}
	}()
	return nil
}

// Stop stops a sandbox's containers and the sandbox itself.
func (e *Executor) Stop(ctx context.Context, application string, descriptor plan.Descriptor) error {
	sandboxID := application + "-" + descriptor.ID
	if err := e.Drain(ctx, application, descriptor); err != nil {
		return err
	}
	e.mu.Lock()
	rt := e.runtimes[sandboxID]
	delete(e.runtimes, sandboxID)
	e.mu.Unlock()
	if rt != nil && rt.guest != nil {
		if rt.cancel != nil {
			rt.cancel()
		}
		logCtx, stopLogs := context.WithTimeout(ctx, 2*time.Second)
		e.drainLogs(logCtx, rt)
		stopLogs()
		_, _ = rt.guest.Stop(ctx, guestproto.StopRequest{})
		logCtx, stopLogs = context.WithTimeout(ctx, 2*time.Second)
		e.drainLogs(logCtx, rt)
		stopLogs()
		_ = rt.guest.Close()
	}
	if err := e.cfg.Backend.Stop(ctx, sandbox.Handle{ID: sandboxID}, 0); err != nil {
		return err
	}
	e.releaseIP(application, descriptor.ID)
	if err := e.detachVolumes(ctx, application, sandboxID); err != nil {
		return err
	}
	return nil
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
	PID       int
}

// Status aggregates container state across an application's sandboxes.
func (e *Executor) Status(ctx context.Context, application string) ([]ContainerState, error) {
	e.mu.Lock()
	if err := e.networkErrors[application]; err != nil {
		e.mu.Unlock()
		return nil, err
	}
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
			return nil, fmt.Errorf("executor: guest status unavailable: %w", err)
		}
		for _, container := range status.Containers {
			states = append(states, ContainerState{Container: container.Name, State: container.State, ExitCode: container.ExitCode, PID: container.PID})
		}
	}
	return states, nil
}

// Restart restarts every running container of an application.
func (e *Executor) Restart(ctx context.Context, application string) error {
	e.mu.Lock()
	var clients []*guestproto.Client
	for _, rt := range e.runtimes {
		if rt.app == application && rt.guest != nil {
			clients = append(clients, rt.guest)
		}
	}
	e.mu.Unlock()
	for _, client := range clients {
		status, err := client.Status(ctx)
		if err != nil {
			continue
		}
		for _, container := range status.Containers {
			if container.State == "running" {
				_, _ = client.Restart(ctx, container.Name)
			}
		}
	}
	return nil
}

// Exec runs a command in a container of an application.
func (e *Executor) Exec(ctx context.Context, application, container string, args []string, stdout, stderr io.Writer) (int, error) {
	return e.ExecAttached(ctx, application, container, guestproto.ExecRequest{Args: args}, nil, stdout, stderr)
}

func (e *Executor) ExecAttached(ctx context.Context, application, container string, request guestproto.ExecRequest, input <-chan guestproto.Frame, stdout, stderr io.Writer) (int, error) {
	// A bare name is allowed only when unique. sandbox-ID/container selects a
	// specific replica without changing the API payload shape.
	key, name, qualified := strings.Cut(container, "/")
	if !qualified {
		name = container
	}
	e.mu.Lock()
	var matches []string
	var cid uint32
	var authKey []byte
	for id, rt := range e.runtimes {
		if rt.app == application && rt.guest != nil && rt.containers[name] && (!qualified || id == key) {
			matches = append(matches, id)
			cid = rt.cid
			authKey = rt.guestKey
		}
	}
	e.mu.Unlock()
	sort.Strings(matches)
	if len(matches) == 0 {
		return 0, fmt.Errorf("executor: no running target %q in application %q", container, application)
	}
	if len(matches) != 1 {
		return 0, fmt.Errorf("executor: ambiguous container %q; use sandbox-ID/container (sandboxes: %s)", container, strings.Join(matches, ", "))
	}
	// Exec owns a dedicated authenticated connection. Cancellation closes it,
	// canceling the guest request without leaving unread replies on the shared
	// health/status channel or blocking probes behind a long-running command.
	client, err := e.dialGuest(ctx, cid, e.cfg.VsockPort, authKey)
	if err != nil {
		return 0, fmt.Errorf("executor: exec channel unavailable: %w", err)
	}
	defer client.Close()
	if request.Stdin && !client.Info().Supports(guestproto.CapStdin) || request.TTY && (!client.Info().Supports(guestproto.CapTTY) || !client.Info().Supports(guestproto.CapResize)) {
		return 0, fmt.Errorf("executor: interactive guest capabilities unavailable; rebuild the guest")
	}
	request.Container = name
	result, err := client.ExecAttached(ctx, request, input, stdout, stderr)
	return result.ExitCode, err
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
	if e.cfg.FreshKeys {
		spec.GuestKey = make([]byte, 32)
		if _, err := rand.Read(spec.GuestKey); err != nil {
			return sandbox.Spec{}, guestproto.SandboxSpec{}, fmt.Errorf("executor: generate boot key: %w", err)
		}
	}
	guestIP := ""
	if e.cfg.EnableNetwork {
		ip, err := e.allocateIP(application, descriptor.ID)
		if err != nil {
			return sandbox.Spec{}, guestproto.SandboxSpec{}, err
		}
		guestIP = ip
		spec.Application = application
	}
	guestSpec := guestproto.SandboxSpec{ID: sandboxID, Hostname: workload.Template.Hostname}
	if guestIP != "" {
		guestSpec.Network = &guestproto.NetworkConfig{Interface: "eth0", Address: guestIP, PrefixLen: bridgePrefix, Gateway: bridgeGateway}
	}

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
		spec.Shares = append(spec.Shares, sandbox.Share{Tag: tag, HostPath: image.HostPath, ReadOnly: true})
		guestSpec.Shares = append(guestSpec.Shares, guestproto.ShareSpec{Tag: tag, Target: target, ReadOnly: true})
		if container.SecurityProfile == nil {
			container.SecurityProfile = &workload.Template.SecurityProfile
		}
		containerSpec, err := e.containerSpec(app, *container, target, volumeTargets)
		if err != nil {
			return sandbox.Spec{}, guestproto.SandboxSpec{}, err
		}
		containerSpec.PrivateRoot = true
		containersCopy := containerSpec
		containersCopy.Init = i < len(workload.Template.InitContainers)
		guestSpec.Containers = append(guestSpec.Containers, containersCopy)
	}
	guestSpec.DNS = e.dnsConfig(app, workload)
	if guestSpec.DNS != nil {
		guestSpec.Nameservers = []string{"127.0.0.1"}
	}
	return spec, guestSpec, nil
}

// dnsConfig builds the guest resolver records for Services selecting this
// workload. With networking each service resolves to its sandboxes' addresses;
// without it, a service resolves to the guest itself.
func (e *Executor) dnsConfig(app model.Application, workload model.Workload) *guestproto.DNSConfig {
	if e.cfg.EnableNetwork {
		return &guestproto.DNSConfig{Server: bridgeGateway, ClusterDomain: "cluster.local", Search: []string{app.Identity.Namespace + ".svc.cluster.local", "svc.cluster.local", "cluster.local"}}
	}
	var records []guestproto.DNSRecord
	for _, service := range app.Services {
		ips := []string{"127.0.0.1"}
		if e.cfg.EnableNetwork {
			if resolved := e.serviceIPs(app, service.Selector); len(resolved) > 0 {
				ips = resolved
			}
		}
		records = append(records, guestproto.DNSRecord{Name: service.Name, Namespace: app.Identity.Namespace, IPs: ips})
	}
	_ = workload
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
	env, err := e.resolveEnv(app, container)
	if err != nil {
		return guestproto.ContainerSpec{}, err
	}
	user, readOnly, err := containerSecurity(container)
	if err != nil {
		return guestproto.ContainerSpec{}, err
	}
	limits, err := resources(container.Resources)
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
		Name:                   container.Name,
		Rootfs:                 rootfs,
		Args:                   args,
		Env:                    env,
		WorkingDir:             container.WorkingDir,
		User:                   user,
		Mounts:                 mounts,
		Resources:              limits,
		ReadOnlyRootFilesystem: readOnly,
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
	if volume.Kind == model.VolumeEphemeral {
		// emptyDir belongs to one Pod, not to its replicated template. Every
		// container in that sandbox still receives the same source.
		spec.ID = ephemeralVolumeID(application, volume.Name, sandboxID)
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
	source := volume.Source
	if kind == storage.KindBind && source != "" && !filepath.IsAbs(source) {
		e.mu.Lock()
		app, ok := e.desired[application]
		e.mu.Unlock()
		if ok && app.Source != nil && app.Source.Path != "" {
			source = filepath.Clean(filepath.Join(filepath.Dir(app.Source.Path), source))
		}
	}
	return storage.Volume{
		ID:            volumeID(application, volume.Name),
		Name:          volume.Name,
		Kind:          kind,
		Source:        source,
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
	// Consult owned persisted volumes rather than the new desired template:
	// an update may remove a volume that the previous sandbox still leased.
	volumes, err := e.cfg.Volumes.List()
	if err != nil {
		return err
	}
	for _, volume := range volumes {
		if volume.Owner.Application != application {
			continue
		}
		if err := e.cfg.Volumes.Detach(ctx, volume.ID, sandboxID); err != nil {
			return err
		}
		if volume.Kind == storage.KindEphemeral && volume.ID == ephemeralVolumeID(application, volume.Name, sandboxID) {
			if err := e.cfg.Volumes.Delete(ctx, volume.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// Quoted components avoid collisions between application, volume and sandbox
// names; the digest is an identity, not a secret fingerprint.
func ephemeralVolumeID(application, name, sandboxID string) string {
	return fmt.Sprintf("ephemeral-%x", sha256.Sum256([]byte(fmt.Sprintf("%q:%q:%q", application, name, sandboxID))))
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

// SecretResolver reads secret values by reference.
type SecretResolver interface {
	Get(ref model.SecretRef) (map[string][]byte, error)
}

func (e *Executor) resolveEnv(app model.Application, container model.Container) ([]string, error) {
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
		if variable.ValueFrom.SecretRef != nil {
			if e.cfg.Secrets == nil {
				return nil, fmt.Errorf("executor: container %q env %q references secret %q but no secret store is configured", container.Name, variable.Name, variable.ValueFrom.SecretRef.Secret)
			}
			ref, ok := secretRefFor(app, variable.ValueFrom.SecretRef.Secret)
			if !ok {
				return nil, fmt.Errorf("executor: container %q env %q references undeclared secret %q", container.Name, variable.Name, variable.ValueFrom.SecretRef.Secret)
			}
			values, err := e.cfg.Secrets.Get(ref)
			if err != nil {
				return nil, fmt.Errorf("executor: container %q env %q: %w", container.Name, variable.Name, err)
			}
			value, ok := values[variable.ValueFrom.SecretRef.Key]
			if !ok {
				return nil, fmt.Errorf("executor: container %q env %q references missing key %q in secret %q", container.Name, variable.Name, variable.ValueFrom.SecretRef.Key, ref.Name)
			}
			env = append(env, variable.Name+"="+string(value))
			continue
		}
		return nil, fmt.Errorf("executor: container %q env %q uses an unsupported source", container.Name, variable.Name)
	}
	return env, nil
}

func secretRefFor(app model.Application, name string) (model.SecretRef, bool) {
	for _, ref := range app.Secrets {
		if ref.Name == name {
			return ref, true
		}
	}
	return model.SecretRef{}, false
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
	uid, err := strconv.ParseUint(uidPart, 10, 32)
	if err != nil {
		return guestproto.UserSpec{}, fmt.Errorf("executor: invalid user %q", user)
	}
	gid := uid
	if gidPart != "" {
		gid, err = strconv.ParseUint(gidPart, 10, 32)
		if err != nil {
			return guestproto.UserSpec{}, fmt.Errorf("executor: invalid user %q", user)
		}
	}
	return guestproto.UserSpec{UID: uint32(uid), GID: uint32(gid)}, nil
}

func resources(r model.Resources) (guestproto.ResourceSpec, error) {
	spec := guestproto.ResourceSpec{}
	if r.Limits.CPU < 0 || r.Limits.Memory < 0 || int64(r.Limits.CPU) > math.MaxInt64/1000 {
		return spec, fmt.Errorf("executor: invalid or overflowing resource limit")
	}
	if r.Limits.CPU > 0 {
		// A one-second period preserves single-millicore precision while
		// meeting Linux CFS's minimum quota of 1000 microseconds.
		spec.CPUPeriodMicros = 1000000
		spec.CPUQuotaMicros = int64(r.Limits.CPU) * (int64(spec.CPUPeriodMicros) / 1000)
	}
	if r.Limits.Memory > 0 {
		spec.MemoryBytes = int64(r.Limits.Memory)
	}
	return spec, nil
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

// Drain excludes the sandbox from endpoint discovery before teardown.
func (s SandboxController) Drain(ctx context.Context, app string, d plan.Descriptor) error {
	return s.E.Drain(ctx, app, d)
}

// UpdateEndpoints publishes the current application network snapshot.
func (s SandboxController) UpdateEndpoints(ctx context.Context, app string) error {
	return s.E.UpdateEndpoints(ctx, app)
}
func (s SandboxController) ShutdownNetwork(ctx context.Context, app string) error {
	return s.E.ShutdownNetwork(ctx, app)
}

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
