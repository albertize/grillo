//go:build linux

// SPDX-License-Identifier: Apache-2.0

// Command grillod is the foreground per-user service. It holds a single-instance
// lock, serves the local Unix-socket API, and reconciles desired state against
// the QEMU backend, the storage manager, and the guest agent.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/albertize/grillo/internal/api"
	"github.com/albertize/grillo/internal/backend/qemu"
	"github.com/albertize/grillo/internal/build"
	"github.com/albertize/grillo/internal/executor"
	"github.com/albertize/grillo/internal/image"
	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/observe"
	"github.com/albertize/grillo/internal/oci"
	"github.com/albertize/grillo/internal/reconcile"
	"github.com/albertize/grillo/internal/sandbox"
	"github.com/albertize/grillo/internal/secrets"
	"github.com/albertize/grillo/internal/state"
	"github.com/albertize/grillo/internal/storage"
)

const version = "0.1.0"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "grillod:", err)
		os.Exit(1)
	}
}

func run() error {
	kernel := flag.String("kernel", "experiments/artifacts/qemu/bzImage", "guest kernel")
	initramfs := flag.String("initramfs", "experiments/artifacts/t07/initramfs-agent.cpio.gz", "guest initramfs")
	keyFile := flag.String("key-file", "experiments/artifacts/t07/key", "base64 per-boot guest key")
	qemuPath := flag.String("qemu", "", "qemu binary (default: PATH)")
	virtiofsd := flag.String("virtiofsd", "", "virtiofsd binary (default: /usr/libexec/virtiofsd)")
	vsockPort := flag.Uint("vsock-port", 1024, "guest vsock port")
	vsockCIDBase := flag.Uint("vsock-cid-base", 20, "first workload guest CID (choose a nonconflicting host-wide range)")
	buildCIDBase := flag.Uint("build-vsock-cid-base", 200, "first build guest CID (choose a separate host-wide range)")
	netnsBinary := flag.String("netns-binary", "bin/grillo-netns", "network supervisor for build egress (empty disables build networking)")
	pasta := flag.String("pasta", "pasta", "pasta binary for build egress")
	flag.Parse()
	if err := validateCIDBases(*vsockCIDBase, *buildCIDBase); err != nil {
		return err
	}

	layout, err := state.NewLayout(state.DefaultConfig())
	if err != nil {
		return err
	}
	if err := layout.Prepare(); err != nil {
		return err
	}
	store, err := state.Open(layout, state.Ops{})
	if err != nil {
		return fmt.Errorf("another grillod is already running: %w", err)
	}
	defer store.Close()

	guestKey, err := readKey(*keyFile)
	if err != nil {
		return err
	}

	events, err := observe.OpenEvents(filepath.Join(layout.State, "events"), 0, 0)
	if err != nil {
		return err
	}
	defer events.Close()
	logs, err := observe.OpenLogSpool(filepath.Join(layout.State, "logs"), 0, 0)
	if err != nil {
		return err
	}
	defer logs.Close()

	// The callback is installed before the backend is opened; runtime is set
	// before any launch or recovery effects are permitted.
	var workloadRuntime *executor.Executor
	backend, err := qemu.Open(qemu.Config{
		Launch: func(ctx context.Context, spec sandbox.Spec, args []string, logPath string) (sandbox.VMM, error) {
			return workloadRuntime.LaunchSandbox(ctx, spec, args, logPath)
		},
		QEMU:      *qemuPath,
		VirtioFSD: *virtiofsd,
		Kernel:    *kernel,
		Initramfs: *initramfs,
		WorkDir:   filepath.Join(layout.Data, "backend"),
	})
	if err != nil {
		return err
	}
	volumes, err := storage.Open(filepath.Join(layout.Data, "volumes"))
	if err != nil {
		return err
	}
	cas, err := oci.OpenCAS(filepath.Join(layout.Cache, "oci"))
	if err != nil {
		return err
	}
	imagesStore, err := image.Open(filepath.Join(layout.Data, "images"), cas)
	if err != nil {
		return err
	}
	secretStore, err := secrets.Open(filepath.Join(layout.Data, "secrets"), state.Ops{})
	if err != nil {
		return err
	}
	podmanBuilder := &build.PodmanBuilder{
		CAS:      cas,
		Store:    imagesStore,
		TempDir:  filepath.Join(layout.Cache, "build"),
		Platform: oci.Platform{OS: "linux", Architecture: "amd64"},
	}
	platform := oci.Platform{OS: "linux", Architecture: "amd64"}
	puller := &oci.Puller{CAS: cas, Registry: oci.NewRegistryClient(), Platform: platform}
	qemuBinary := *qemuPath
	if qemuBinary == "" {
		qemuBinary = "qemu-system-x86_64"
	}
	guestBoot := &build.GuestBoot{
		Backend:      backend,
		Kernel:       *kernel,
		Initramfs:    *initramfs,
		KernelArgs:   "console=ttyS0 reboot=k panic=1 rdinit=/init",
		GuestKey:     guestKey,
		VsockCIDBase: uint32(*buildCIDBase),
		VsockPort:    uint32(*vsockPort),
		MemoryMiB:    512,
		ShareTag:     "build",
		ShareTarget:  "/build",
	}
	var buildNetwork *build.BuildNetwork
	if *netnsBinary != "" {
		if _, err := os.Stat(*netnsBinary); err != nil {
			fmt.Fprintf(os.Stderr, "grillod: build networking disabled: %v (build 'make build' or pass -netns-binary)\n", err)
		} else {
			buildNetwork = &build.BuildNetwork{
				QEMU:        qemuBinary,
				NetnsBinary: *netnsBinary,
				Pasta:       *pasta,
				RuntimeDir:  layout.Runtime,
				Application: "grillo-build",
				Nameservers: hostNameservers(),
			}
			defer buildNetwork.Close()
			buildBackend, err := qemu.Open(qemu.Config{
				QEMU:      *qemuPath,
				VirtioFSD: *virtiofsd,
				Kernel:    *kernel,
				Initramfs: *initramfs,
				WorkDir:   filepath.Join(layout.Data, "build-backend"),
				Launch:    buildNetwork.Launch,
			})
			if err != nil {
				return err
			}
			guestBoot.Backend = buildBackend
			guestBoot.Network = buildNetwork
			guestBoot.Nameservers = hostNameservers()
		}
	}
	images := &executor.OCIResolver{
		Puller:   &oci.Puller{CAS: cas, Registry: oci.NewRegistryClient(), Platform: oci.Platform{OS: "linux", Architecture: "amd64"}},
		CacheDir: filepath.Join(layout.Cache, "rootfs"),
		Store:    imagesStore,
		CAS:      cas,
	}
	exec, err := executor.New(executor.Config{
		Backend:       backend,
		Volumes:       volumes,
		Kernel:        *kernel,
		Initramfs:     *initramfs,
		KernelArgs:    "console=ttyS0 reboot=k panic=1 rdinit=/init",
		GuestKey:      guestKey,
		VsockPort:     uint32(*vsockPort),
		VsockCIDBase:  uint32(*vsockCIDBase),
		Images:        images,
		Secrets:       secretStore,
		EnableNetwork: true,
		QEMU:          qemuBinary,
		NetnsBinary:   *netnsBinary,
		Pasta:         *pasta,
		RuntimeDir:    layout.Runtime,
	})
	if err != nil {
		return err
	}
	workloadRuntime = exec
	persistent, err := reconcile.OpenPersistentStore(layout.State)
	if err != nil {
		return err
	}
	reconciler := &reconcile.Reconciler{
		Store:    persistent,
		Executor: &reconcile.NativeExecutor{Sandboxes: executor.SandboxController{E: exec}, Volumes: executor.VolumeController{E: exec}},
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	runtimeCore := &core{
		reconciler:   reconciler,
		persistent:   persistent,
		exec:         exec,
		images:       imagesStore,
		cas:          cas,
		puller:       puller,
		boot:         guestBoot,
		podman:       podmanBuilder,
		buildScratch: filepath.Join(layout.Cache, "native-build"),
		apps:         map[string]bool{},
		desired:      map[string]model.Application{},
	}
	// Recovery is intentionally disruptive: verified old VMMs are stopped,
	// volume leases are released, then durable non-stopped intent is replayed.
	recoverCtx, recoverCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer recoverCancel()
	if err := backend.Recover(recoverCtx); err != nil {
		return fmt.Errorf("recover backend: %w", err)
	}
	if err := volumes.Reconcile(func(string) bool { return false }); err != nil {
		return err
	}
	if err := runtimeCore.recover(recoverCtx); err != nil {
		return fmt.Errorf("recover applications: %w", err)
	}
	server := api.NewServer(api.Options{
		Core:     runtimeCore,
		Images:   runtimeCore,
		Events:   events,
		Logs:     logs,
		Version:  version,
		Shutdown: cancel,
	})
	socket := filepath.Join(layout.Runtime, "grillod.sock")
	fmt.Fprintf(os.Stderr, "grillod: serving %s (version %s)\n", socket, version)
	return server.Serve(ctx, socket)
}

// hostNameservers reads the host resolver configuration so a build guest can
// reach package repositories. It falls back to well-known public resolvers.
func hostNameservers() []string {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err == nil {
		var servers []string
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == "nameserver" {
				servers = append(servers, fields[1])
			}
		}
		if len(servers) > 0 {
			return servers
		}
	}
	return []string{"1.1.1.1", "8.8.8.8"}
}

func readKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read guest key: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("decode guest key: %w", err)
	}
	if len(key) < 16 {
		return nil, errors.New("guest key is too short")
	}
	return key, nil
}

// core adapts the reconciler and executor to the API contract.
type core struct {
	reconciler   *reconcile.Reconciler
	exec         *executor.Executor
	images       *image.Store
	cas          *oci.CAS
	puller       *oci.Puller
	boot         *build.GuestBoot
	podman       *build.PodmanBuilder
	buildScratch string

	// mutationMu covers publication, persistence and effects as one operation.
	// Serialize globally for now; correctness takes precedence over parallelism.
	mutationMu sync.Mutex
	persistent *reconcile.PersistentStore
	mu         sync.Mutex
	apps       map[string]bool
	desired    map[string]model.Application
}

func (c *core) Apply(ctx context.Context, app model.Application) (reconcile.Result, error) {
	c.mutationMu.Lock()
	defer c.mutationMu.Unlock()
	if err := ctx.Err(); err != nil {
		return reconcile.Result{}, err
	}
	if c.exec == nil {
		return reconcile.Result{}, fmt.Errorf("native executor is not configured")
	}
	if diagnostics := c.exec.ValidateApplication(app); diagnostics.HasErrors() {
		return reconcile.Result{}, fmt.Errorf("invalid application: %s", diagnostics.Errors()[0].Message)
	}
	if c.persistent != nil {
		if err := c.persistent.SetDesired(app); err != nil {
			return reconcile.Result{}, err
		}
	}
	c.exec.SetDesired(app)
	result, err := c.reconciler.Apply(ctx, app)
	if err == nil {
		c.mu.Lock()
		if c.apps == nil {
			c.apps = map[string]bool{}
		}
		if c.desired == nil {
			c.desired = map[string]model.Application{}
		}
		c.apps[app.Identity.Name] = true
		c.desired[app.Identity.Name] = app
		c.mu.Unlock()
	}
	return result, err
}

func (c *core) Applications(context.Context) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	names := make([]string, 0, len(c.apps))
	for name := range c.apps {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (c *core) Restart(ctx context.Context, application string) error {
	c.mutationMu.Lock()
	defer c.mutationMu.Unlock()
	return c.exec.Restart(ctx, application)
}

func (c *core) Down(ctx context.Context, application string, removeVolumes bool) (reconcile.Result, error) {
	c.mutationMu.Lock()
	defer c.mutationMu.Unlock()
	if c.persistent != nil {
		if err := c.persistent.SetStopped(application, removeVolumes); err != nil {
			return reconcile.Result{}, err
		}
	}
	return c.reconciler.Down(ctx, application, removeVolumes)
}

func (c *core) Status(ctx context.Context, application string) ([]api.ContainerStatus, error) {
	states, err := c.exec.Status(ctx, application)
	if err != nil {
		return nil, err
	}
	out := make([]api.ContainerStatus, 0, len(states))
	for _, state := range states {
		out = append(out, api.ContainerStatus{Container: state.Container, State: state.State, ExitCode: state.ExitCode})
	}
	return out, nil
}

func (c *core) View(ctx context.Context, application string) (observe.ApplicationView, error) {
	return c.exec.View(ctx, application)
}

func (c *core) Routes(ctx context.Context, application string) ([]api.RouteStatus, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var result []api.RouteStatus
	for _, route := range c.exec.Routes(application) {
		result = append(result, api.RouteStatus{Hostname: route.Hostname, Path: route.Path, PathType: route.PathType, Endpoint: route.Endpoint})
	}
	return result, nil
}

func (c *core) ExecStream(ctx context.Context, application, container string, args []string, stdout, stderr io.Writer) (int, error) {
	return c.exec.Exec(ctx, application, container, args, stdout, stderr)
}

func (c *core) Exec(ctx context.Context, application, container string, args []string, maxOutput int64) (int, string, string, error) {
	stdout := &boundedBuffer{limit: maxOutput}
	stderr := &boundedBuffer{limit: maxOutput}
	code, err := c.exec.Exec(ctx, application, container, args, stdout, stderr)
	return code, stdout.String(), stderr.String(), err
}

// Images implements api.ImageManager.

func (c *core) Images(context.Context) ([]image.Record, error) {
	if c.images == nil {
		return nil, errors.New("image inventory is not configured")
	}
	return c.images.List()
}

func (c *core) InspectImage(ctx context.Context, reference string) (image.Record, error) {
	if c.images == nil {
		return image.Record{}, errors.New("image inventory is not configured")
	}
	return c.images.Require(reference)
}

func (c *core) PinImage(ctx context.Context, reference string, pinned bool) error {
	if c.images == nil {
		return errors.New("image inventory is not configured")
	}
	if strings.HasPrefix(reference, "sha256:") {
		return c.images.PinDigest(reference, pinned)
	}
	return c.images.Pin(reference, pinned)
}

func (c *core) PruneImages(ctx context.Context, keep []string) (image.PruneResult, error) {
	if c.images == nil {
		return image.PruneResult{}, errors.New("image inventory is not configured")
	}
	keepSet := map[string]bool{}
	for _, reference := range keep {
		keepSet[reference] = true
	}
	c.mu.Lock()
	for _, app := range c.desired {
		for _, workload := range app.Workloads {
			for _, container := range workload.Template.Containers {
				if container.Image.Reference != "" {
					keepSet[container.Image.Reference] = true
				}
			}
		}
	}
	c.mu.Unlock()
	return c.images.Prune(ctx, func(record image.Record) bool { return keepSet[record.Reference] })
}

func (c *core) Build(ctx context.Context, request build.Request, progress func(string)) (build.Result, error) {
	switch request.Builder {
	case "podman":
		if c.podman == nil {
			return build.Result{}, errors.New("build: the podman backend is not configured")
		}
		return c.podman.Build(ctx, request, build.Progress(progress))
	case "", "native":
		if c.images == nil || c.cas == nil || c.puller == nil {
			return build.Result{}, errors.New("build: the native builder is not configured")
		}
		if err := os.MkdirAll(c.buildScratch, 0o700); err != nil {
			return build.Result{}, err
		}
		runner := &build.SandboxRunner{Share: "build"}
		if c.boot != nil {
			runner.Boot = c.boot.Boot
		}
		native := &build.NativeBuilder{
			Store:      c.images,
			CAS:        c.cas,
			Images:     c.puller,
			Runner:     runner,
			ScratchDir: c.buildScratch,
			Platform:   oci.Platform{OS: "linux", Architecture: "amd64"},
		}
		return native.Build(ctx, request, build.Progress(progress))
	default:
		return build.Result{}, fmt.Errorf("build: unknown builder %q", request.Builder)
	}
}

type boundedBuffer struct {
	limit int64
	buf   []byte
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		b.limit = 1 << 20
	}
	if int64(len(b.buf)) < b.limit {
		remaining := b.limit - int64(len(b.buf))
		if int64(len(p)) > remaining {
			p = p[:remaining]
		}
		b.buf = append(b.buf, p...)
	}
	return len(p), nil
}

func (b *boundedBuffer) String() string { return string(b.buf) }
