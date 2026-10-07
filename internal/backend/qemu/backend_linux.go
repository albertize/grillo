//go:build linux

// SPDX-License-Identifier: Apache-2.0

package qemu

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/albertize/grillo/internal/guest"
	"github.com/albertize/grillo/internal/guestproto"
	linux "github.com/albertize/grillo/internal/platform/linux"
	"github.com/albertize/grillo/internal/sandbox"
)

// Backend is the QEMU microvm implementation of sandbox.Backend.
type Backend struct {
	cfg       Config
	artifacts map[string]guest.Artifact

	mu          sync.Mutex
	sandboxes   map[string]*sandboxEntry
	byOperation map[sandbox.OperationID]string
	nextCID     uint32
}

type sandboxEntry struct {
	mu        sync.Mutex
	persisted persistedSandbox
	dir       string
	qemu      sandbox.VMM
	virtiofsd []*process
	guest     GuestConn
}

// Open creates a backend and loads any sandboxes left by a previous run.
func Open(cfg Config) (*Backend, error) {
	cfg = cfg.withDefaults()
	if cfg.WorkDir == "" {
		return nil, errors.New("qemu: WorkDir is required")
	}
	if err := ensurePrivateDir(cfg.WorkDir); err != nil {
		return nil, err
	}
	b := &Backend{
		cfg:         cfg,
		sandboxes:   make(map[string]*sandboxEntry),
		byOperation: make(map[sandbox.OperationID]string),
		nextCID:     cfg.CIDBase,
	}
	if cfg.ArtifactManifest != "" {
		manifest, err := guest.ReadManifest(cfg.ArtifactManifest)
		if err != nil {
			return nil, fmt.Errorf("qemu: invalid artifact manifest: %w", err)
		}
		b.artifacts = map[string]guest.Artifact{}
		for _, a := range manifest.Artifacts {
			b.artifacts[a.Name] = a
		}
		if _, ok := b.artifacts["kernel"]; !ok {
			return nil, fmt.Errorf("qemu: manifest lacks kernel")
		}
		if _, ok := b.artifacts["initramfs"]; !ok {
			return nil, fmt.Errorf("qemu: manifest lacks initramfs")
		}
	}
	if cfg.BootKeyOverlay && b.artifacts == nil {
		return nil, fmt.Errorf("qemu: verified artifact manifest is required")
	}
	if cfg.DialGuest == nil {
		b.cfg.DialGuest = b.dialGuest
	}
	entries, err := os.ReadDir(cfg.WorkDir)
	if err != nil {
		return nil, err
	}
	for _, dirent := range entries {
		if !dirent.IsDir() {
			continue
		}
		dir := filepath.Join(cfg.WorkDir, dirent.Name())
		ps, err := loadSandbox(dir)
		if errors.Is(err, sandbox.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("qemu: load %s: %w", dirent.Name(), err)
		}
		b.sandboxes[ps.ID] = &sandboxEntry{persisted: ps, dir: dir}
		if ps.Operation != "" {
			b.byOperation[sandbox.OperationID(ps.Operation)] = ps.ID
		}
		if ps.CID >= b.nextCID {
			b.nextCID = ps.CID + 1
		}
	}
	return b, nil
}

// Capabilities probes the host for KVM, vhost-vsock, and virtiofsd.
func (b *Backend) Capabilities(_ context.Context) (sandbox.Capabilities, error) {
	caps := sandbox.Capabilities{
		KVM:      deviceFeature("/dev/kvm", syscall.O_RDWR),
		Vsock:    deviceFeature("/dev/vhost-vsock", syscall.O_RDWR),
		VirtioFS: binaryFeature(b.cfg.VirtioFSD),
	}
	return caps, nil
}

// Create records a new sandbox. Repeating the same OperationID returns the same
// sandbox, and it never creates a second sandbox for an existing ID.
func (b *Backend) Create(_ context.Context, spec sandbox.Spec, op sandbox.OperationID) (sandbox.Handle, error) {
	if err := spec.Validate(); err != nil {
		return sandbox.Handle{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if op != "" {
		if id, ok := b.byOperation[op]; ok {
			return sandbox.Handle{ID: id, Operation: op}, nil
		}
	}
	if entry, ok := b.sandboxes[spec.ID]; ok {
		if op == "" || sandbox.OperationID(entry.persisted.Operation) == op {
			return sandbox.Handle{ID: spec.ID, Operation: sandbox.OperationID(entry.persisted.Operation)}, nil
		}
		return sandbox.Handle{}, fmt.Errorf("%w: sandbox %q already exists", sandbox.ErrConflict, spec.ID)
	}
	if spec.VsockCID == 0 {
		cid, err := b.allocCIDLocked()
		if err != nil {
			return sandbox.Handle{}, err
		}
		spec.VsockCID = cid
	}
	dir := sandboxDir(b.cfg.WorkDir, spec.ID)
	if err := ensurePrivateDir(dir); err != nil {
		return sandbox.Handle{}, err
	}
	ps := persistedSandbox{ID: spec.ID, Operation: string(op), Spec: spec, CID: spec.VsockCID, State: string(sandbox.StateCreated)}
	if err := saveSandbox(dir, ps); err != nil {
		return sandbox.Handle{}, err
	}
	b.sandboxes[spec.ID] = &sandboxEntry{persisted: ps, dir: dir}
	if op != "" {
		b.byOperation[op] = spec.ID
	}
	return sandbox.Handle{ID: spec.ID, Operation: op}, nil
}

func (b *Backend) allocCIDLocked() (uint32, error) {
	used := map[uint32]bool{}
	for _, entry := range b.sandboxes {
		used[entry.persisted.CID] = true
	}
	for attempts := 0; attempts < 1<<16; attempts++ {
		cid := b.nextCID
		b.nextCID++
		if cid < b.cfg.CIDBase {
			continue
		}
		if !used[cid] {
			return cid, nil
		}
	}
	return 0, errors.New("qemu: exhausted vsock CIDs")
}

// BootConnection returns private control credentials to local consumers after
// Start. It is never a public API projection; repeating Create does not change
// a live VM's credentials or address.
func (b *Backend) BootConnection(handle sandbox.Handle) (uint32, uint32, []byte, error) {
	entry, err := b.entry(handle.ID)
	if err != nil {
		return 0, 0, nil, err
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	spec := entry.persisted.Spec
	return spec.VsockCID, spec.VsockPort, append([]byte(nil), spec.GuestKey...), nil
}

// Start boots the sandbox if it is not already running. It is idempotent.
func (b *Backend) Start(ctx context.Context, handle sandbox.Handle) error {
	entry, err := b.entry(handle.ID)
	if err != nil {
		return err
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.qemu != nil && entry.qemu.Alive() {
		return nil
	}
	spec := entry.persisted.Spec
	dir := entry.dir
	if b.cfg.BootKeyOverlay {
		spec.GuestKey = make([]byte, 32)
		if _, err := rand.Read(spec.GuestKey); err != nil {
			return fmt.Errorf("qemu: generate boot credential: %w", err)
		}
		entry.persisted.Spec.GuestKey = append([]byte(nil), spec.GuestKey...)
		if err := saveSandbox(dir, entry.persisted); err != nil {
			return err
		}
	}
	// Verification and operation-private snapshots precede every helper/VMM
	// effect. The manifest is pinned when Open runs, not re-trusted per boot.
	spec, err = b.prepareBoot(ctx, spec, dir)
	if err != nil {
		return err
	}

	vmmLog, err := os.OpenFile(vmmLogPath(dir), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer vmmLog.Close()

	var vfs []*process
	stopHelpers := func() {
		for _, p := range vfs {
			_ = p.stop(2 * time.Second)
		}
	}
	shareSockets := make(map[string]string, len(spec.Shares))
	for _, share := range spec.Shares {
		socket := shareSocket(dir, share.Tag)
		p, err := startProcess(b.cfg.VirtioFSD, vmmLog, virtiofsdArgs(share, socket)...)
		if err != nil {
			stopHelpers()
			return fmt.Errorf("qemu: start virtiofsd for %q: %w", share.Tag, err)
		}
		vfs = append(vfs, p)
		entry.persisted.VirtioFSD = processIDs(vfs)
		if err := saveSandbox(dir, entry.persisted); err != nil {
			stopHelpers()
			return err
		}
		shareSockets[share.Tag] = socket
	}
	for tag, socket := range shareSockets {
		if err := waitForPath(ctx, socket, 5*time.Second); err != nil {
			stopHelpers()
			return fmt.Errorf("qemu: virtiofsd socket for %q: %w", tag, err)
		}
	}

	entry.persisted.State = "booting"
	if err := saveSandbox(dir, entry.persisted); err != nil {
		stopHelpers()
		return err
	}
	var qemu sandbox.VMM
	if b.cfg.Launch != nil {
		launched, launchErr := b.cfg.Launch(ctx, spec, qemuArgs(spec, b.cfg, dir, serialLogPath(dir), shareSockets), vmmLogPath(dir))
		if launchErr != nil {
			stopHelpers()
			return fmt.Errorf("qemu: launch vmm: %w", launchErr)
		}
		qemu = launched
	} else {
		qemu, err = startProcess(b.cfg.QEMU, vmmLog, qemuArgs(spec, b.cfg, dir, serialLogPath(dir), shareSockets)...)
		if err != nil {
			stopHelpers()
			return fmt.Errorf("qemu: start vmm: %w", err)
		}
	}
	identity := vmmProcessID(qemu)
	if identity == nil {
		identity, err = identifyVMM(ctx, dir)
		if err != nil {
			_ = qemu.Stop(0)
			stopHelpers()
			return err
		}
	}
	entry.persisted.QEMU = identity
	if err := saveSandbox(dir, entry.persisted); err != nil {
		_ = qemu.Stop(0)
		stopHelpers()
		return err
	}
	guest, err := b.waitGuest(ctx, spec)
	if err != nil {
		_ = qemu.Stop(2 * time.Second)
		stopHelpers()
		entry.persisted.State = string(sandbox.StateFailed)
		_ = saveSandbox(dir, entry.persisted)
		return fmt.Errorf("qemu: guest handshake: %w", err)
	}
	entry.qemu = qemu
	entry.virtiofsd = vfs
	entry.guest = guest
	entry.persisted.VirtioFSD = processIDs(vfs)
	entry.persisted.State = string(sandbox.StateRunning)
	if err := saveSandbox(dir, entry.persisted); err != nil {
		return err
	}
	b.cfg.Logger("qemu: sandbox %s running cid=%d", spec.ID, spec.VsockCID)
	return nil
}

// Inspect reports the sandbox state, verifying process identity so a recycled
// PID is never mistaken for the running VMM.
func (b *Backend) Inspect(ctx context.Context, handle sandbox.Handle) (sandbox.Observation, error) {
	entry, err := b.entry(handle.ID)
	if errors.Is(err, sandbox.ErrNotFound) {
		return sandbox.Observation{State: sandbox.StateAbsent}, nil
	}
	if err != nil {
		return sandbox.Observation{}, err
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()

	obs := sandbox.Observation{}
	qemuAlive := entry.qemu != nil && entry.qemu.Alive()
	if !qemuAlive && entry.persisted.QEMU != nil {
		qemuAlive = processFromPersisted(entry.persisted.QEMU).Alive()
	}
	if entry.persisted.QEMU != nil {
		obs.PID = entry.persisted.QEMU.PID
	}
	if !qemuAlive {
		switch sandbox.State(entry.persisted.State) {
		case sandbox.StateCreated:
			obs.State = sandbox.StateCreated
		case sandbox.StateFailed:
			obs.State = sandbox.StateFailed
		default:
			obs.State = sandbox.StateStopped
			if entry.persisted.QEMU != nil {
				obs.Reason = "vmm process is gone"
			}
		}
		return obs, nil
	}
	for i := range entry.persisted.VirtioFSD {
		if !processFromPersisted(&entry.persisted.VirtioFSD[i]).Alive() {
			obs.State = sandbox.StateFailed
			obs.Reason = "shared-filesystem helper is gone"
			return obs, nil
		}
	}
	obs.State = sandbox.StateRunning
	if entry.guest != nil {
		if status, err := entry.guest.Status(ctx); err == nil {
			obs.GuestAlive = true
			obs.GuestState = status.State
			obs.GuestZombies = status.Zombies
			obs.GuestContainers = status.Containers
		} else {
			obs.Reason = "guest status: " + err.Error()
		}
	}
	return obs, nil
}

// Stop stops the guest workload and terminates the VMM and helpers. Stopping an
// absent sandbox succeeds.
func (b *Backend) Stop(ctx context.Context, handle sandbox.Handle, grace time.Duration) error {
	entry, err := b.entry(handle.ID)
	if errors.Is(err, sandbox.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.guest != nil {
		sctx, cancel := context.WithTimeout(ctx, grace)
		_, _ = entry.guest.Stop(sctx)
		cancel()
		_ = entry.guest.Close()
		entry.guest = nil
	}
	if entry.qemu != nil {
		_ = entry.qemu.Stop(grace)
		entry.qemu = nil
	} else if entry.persisted.QEMU != nil {
		stopPersisted(*entry.persisted.QEMU, grace)
	}
	for _, p := range entry.virtiofsd {
		_ = p.stop(2 * time.Second)
	}
	entry.virtiofsd = nil
	for i := range entry.persisted.VirtioFSD {
		stopPersisted(entry.persisted.VirtioFSD[i], 2*time.Second)
	}
	entry.persisted.State = string(sandbox.StateStopped)
	return saveSandbox(entry.dir, entry.persisted)
}

// Delete stops and removes a sandbox. Deleting an absent sandbox succeeds.
func (b *Backend) Delete(ctx context.Context, handle sandbox.Handle) error {
	entry, err := b.entry(handle.ID)
	if errors.Is(err, sandbox.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := b.Stop(ctx, handle, 2*time.Second); err != nil {
		return err
	}
	b.mu.Lock()
	delete(b.sandboxes, handle.ID)
	if entry.persisted.Operation != "" {
		delete(b.byOperation, sandbox.OperationID(entry.persisted.Operation))
	}
	b.mu.Unlock()
	if err := os.RemoveAll(entry.dir); err != nil {
		return fmt.Errorf("qemu: remove sandbox dir: %w", err)
	}
	return nil
}

// vmmProcessID persists a host PID only for locally started processes. An
// externally launched VMM may run in a PID namespace, so it has no host PID.
func vmmProcessID(vmm sandbox.VMM) *persistedProcess {
	if p, ok := vmm.(*process); ok {
		return processID(p)
	}
	return nil
}

func (b *Backend) entry(id string) (*sandboxEntry, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	entry, ok := b.sandboxes[id]
	if !ok {
		return nil, fmt.Errorf("%w: sandbox %q", sandbox.ErrNotFound, id)
	}
	return entry, nil
}

func (b *Backend) waitGuest(ctx context.Context, spec sandbox.Spec) (GuestConn, error) {
	deadline := time.Now().Add(b.cfg.BootTimeout)
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		guest, err := b.cfg.DialGuest(ctx, spec)
		if err == nil {
			return guest, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("guest did not handshake within %s: %w", b.cfg.BootTimeout, lastErr)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (b *Backend) dialGuest(ctx context.Context, spec sandbox.Spec) (GuestConn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := guestproto.DialVsock(dialCtx, spec.VsockCID, spec.VsockPort)
	if err != nil {
		return nil, err
	}
	client, err := guestproto.NewClient(dialCtx, conn, guestproto.HandshakeConfig{
		Sandbox: spec.GuestSandbox,
		Key:     spec.GuestKey,
		Agent:   "qemu-backend",
	})
	if err != nil {
		return nil, err
	}
	return &guestClient{client: client}, nil
}

func processIDs(procs []*process) []persistedProcess {
	out := make([]persistedProcess, 0, len(procs))
	for _, p := range procs {
		if pp := processID(p); pp != nil {
			out = append(out, *pp)
		}
	}
	return out
}

// stopPersisted terminates a process recorded by an earlier run, verifying
// identity before signalling.
func stopPersisted(pp persistedProcess, grace time.Duration) {
	id := processFromPersisted(&pp)
	if !id.Alive() {
		return
	}
	_, _ = linux.SignalGroup(id, syscall.SIGTERM)
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) && id.Alive() {
		time.Sleep(50 * time.Millisecond)
	}
	if id.Alive() {
		_, _ = linux.SignalGroup(id, syscall.SIGKILL)
	}
}

// guestClient adapts the protocol client to the backend's GuestConn.
type guestClient struct{ client *guestproto.Client }

func (g *guestClient) Status(ctx context.Context) (GuestStatus, error) {
	status, err := g.client.Status(ctx)
	if err != nil {
		return GuestStatus{}, err
	}
	return GuestStatus{State: status.State, Zombies: status.Zombies, Containers: len(status.Containers)}, nil
}

func (g *guestClient) Stop(ctx context.Context) (bool, error) {
	result, err := g.client.Stop(ctx, guestproto.StopRequest{})
	if err != nil {
		return false, err
	}
	return result.Stopped, nil
}

func (g *guestClient) Close() error { return g.client.Close() }

func deviceFeature(path string, flag int) sandbox.Feature {
	f, err := os.OpenFile(path, flag, 0)
	if err != nil {
		return sandbox.Feature{Reason: err.Error()}
	}
	_ = f.Close()
	return sandbox.Feature{Supported: true}
}

func binaryFeature(path string) sandbox.Feature {
	info, err := os.Stat(path)
	if err != nil {
		return sandbox.Feature{Reason: err.Error()}
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return sandbox.Feature{Reason: "not executable"}
	}
	return sandbox.Feature{Supported: true}
}

func waitForPath(ctx context.Context, path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s", path)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}
