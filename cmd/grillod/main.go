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
	"strings"
	"syscall"

	"grillo.local/grillo/internal/api"
	"grillo.local/grillo/internal/backend/qemu"
	"grillo.local/grillo/internal/executor"
	"grillo.local/grillo/internal/model"
	"grillo.local/grillo/internal/observe"
	"grillo.local/grillo/internal/oci"
	"grillo.local/grillo/internal/reconcile"
	"grillo.local/grillo/internal/state"
	"grillo.local/grillo/internal/storage"
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
	flag.Parse()

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

	backend, err := qemu.Open(qemu.Config{
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
	images := &executor.OCIResolver{
		Puller:   &oci.Puller{CAS: cas, Registry: oci.NewRegistryClient(), Platform: oci.Platform{OS: "linux", Architecture: "amd64"}},
		CacheDir: filepath.Join(layout.Cache, "rootfs"),
	}
	exec, err := executor.New(executor.Config{
		Backend:      backend,
		Volumes:      volumes,
		Kernel:       *kernel,
		Initramfs:    *initramfs,
		KernelArgs:   "console=ttyS0 reboot=k panic=1 rdinit=/init",
		GuestKey:     guestKey,
		VsockPort:    uint32(*vsockPort),
		VsockCIDBase: 20,
		Images:       images,
	})
	if err != nil {
		return err
	}
	reconciler := &reconcile.Reconciler{
		Store:    reconcile.NewMemoryStore(),
		Executor: &reconcile.NativeExecutor{Sandboxes: executor.SandboxController{E: exec}, Volumes: executor.VolumeController{E: exec}},
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	server := api.NewServer(api.Options{
		Core:     &core{reconciler: reconciler, exec: exec},
		Events:   events,
		Logs:     logs,
		Version:  version,
		Shutdown: cancel,
	})
	socket := filepath.Join(layout.Runtime, "grillod.sock")
	fmt.Fprintf(os.Stderr, "grillod: serving %s (version %s)\n", socket, version)
	return server.Serve(ctx, socket)
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
	reconciler *reconcile.Reconciler
	exec       *executor.Executor
}

func (c *core) Apply(ctx context.Context, app model.Application) (reconcile.Result, error) {
	c.exec.SetDesired(app)
	return c.reconciler.Apply(ctx, app)
}

func (c *core) Down(ctx context.Context, application string, removeVolumes bool) (reconcile.Result, error) {
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

func (c *core) ExecStream(ctx context.Context, application, container string, args []string, stdout, stderr io.Writer) (int, error) {
	return c.exec.Exec(ctx, application, container, args, stdout, stderr)
}

func (c *core) Exec(ctx context.Context, application, container string, args []string, maxOutput int64) (int, string, string, error) {
	stdout := &boundedBuffer{limit: maxOutput}
	stderr := &boundedBuffer{limit: maxOutput}
	code, err := c.exec.Exec(ctx, application, container, args, stdout, stderr)
	return code, stdout.String(), stderr.String(), err
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
