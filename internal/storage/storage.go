//go:build linux

// SPDX-License-Identifier: Apache-2.0

// Package storage manages host volumes: their lifecycle, ownership, leases,
// access modes, and safe deletion. It is host-side and never executes guest
// content. Bind mounts are validated and are never created or deleted.
package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"grillo.local/grillo/internal/state"

	"golang.org/x/sys/unix"
)

// Kind classifies a volume's lifecycle and security properties.
type Kind string

const (
	// KindEphemeral is destroyed with its sandbox.
	KindEphemeral Kind = "ephemeral"
	// KindManaged is a named volume owned by Grillo, persistent across restarts.
	KindManaged Kind = "managed"
	// KindPVC is a local Kubernetes-PVC-compatible managed volume.
	KindPVC Kind = "pvc"
	// KindBind is an explicit host path. Grillo never creates or deletes it.
	KindBind Kind = "bind"
)

// AccessMode mirrors Kubernetes access modes.
type AccessMode string

const (
	AccessReadWriteOnce AccessMode = "ReadWriteOnce"
	AccessReadOnlyMany  AccessMode = "ReadOnlyMany"
	AccessReadWriteMany AccessMode = "ReadWriteMany"
)

// Errors returned by the storage manager.
var (
	ErrNotFound    = errors.New("storage: volume not found")
	ErrInvalid     = errors.New("storage: invalid volume")
	ErrBusy        = errors.New("storage: volume is in use")
	ErrReadOnly    = errors.New("storage: volume is read-only")
	ErrUnsupported = errors.New("storage: unsupported operation")
	ErrNotOwned    = errors.New("storage: volume is not owned by Grillo")
)

// Owner records which application and operation created a volume.
type Owner struct {
	Application string `json:"application"`
	Operation   string `json:"operation,omitempty"`
}

// Volume is one managed, PVC, ephemeral, or bind volume.
type Volume struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Kind          Kind       `json:"kind"`
	Source        string     `json:"source,omitempty"` // bind host path
	ReadOnly      bool       `json:"readOnly"`
	AccessMode    AccessMode `json:"accessMode"`
	CapacityBytes int64      `json:"capacityBytes,omitempty"`
	Owner         Owner      `json:"owner"`
	CreatedAt     time.Time  `json:"createdAt"`
}

// Lease records one attachment of a volume to a sandbox.
type Lease struct {
	VolumeID  string    `json:"volumeID"`
	SandboxID string    `json:"sandboxID"`
	ReadOnly  bool      `json:"readOnly"`
	UID       int       `json:"uid"`
	GID       int       `json:"gid"`
	Operation string    `json:"operation,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Attachment is the resolved mount information for a sandbox.
type Attachment struct {
	VolumeID string
	Source   string
	ReadOnly bool
	UID      int
	GID      int
}

// Manager owns the volume store.
type Manager struct {
	dir        string
	volumesDir string
	leasesDir  string

	mu sync.Mutex
}

// Open prepares a volume store rooted at dir.
func Open(dir string) (*Manager, error) {
	m := &Manager{
		dir:        dir,
		volumesDir: filepath.Join(dir, "volumes"),
		leasesDir:  filepath.Join(dir, "leases"),
	}
	for _, sub := range []string{m.volumesDir, m.leasesDir} {
		if err := os.MkdirAll(sub, 0o700); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// ValidateBind checks that a bind source is explicit, absolute, canonical, and
// does not expose the home directory or the filesystem root.
func ValidateBind(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: bind path must be absolute", ErrInvalid)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("%w: bind path: %v", ErrInvalid, err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("%w: bind path is not a directory", ErrInvalid)
	}
	if resolved == "/" {
		return "", fmt.Errorf("%w: refusing to bind the filesystem root", ErrInvalid)
	}
	if home, err := os.UserHomeDir(); err == nil && resolved == home {
		return "", fmt.Errorf("%w: refusing to bind the home directory", ErrInvalid)
	}
	return resolved, nil
}

// Create records a volume. Repeating the same operation returns the same volume.
func (m *Manager) Create(ctx context.Context, volume Volume, op string) (Volume, error) {
	if err := m.validate(&volume, op); err != nil {
		return Volume{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, err := m.load(volume.ID); err == nil {
		if op == "" || existing.Owner.Operation == op {
			return existing, nil
		}
		return Volume{}, fmt.Errorf("%w: volume %q already exists", ErrBusy, volume.ID)
	}
	volume.Owner.Operation = op
	dir := m.volumeDir(volume.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Volume{}, err
	}
	if volume.Kind == KindBind {
		resolved, err := ValidateBind(volume.Source)
		if err != nil {
			return Volume{}, err
		}
		volume.Source = resolved
	} else {
		// Share only the data subdirectory so volume metadata never reaches a
		// workload.
		dataDir := filepath.Join(dir, "data")
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			return Volume{}, err
		}
		volume.Source = dataDir
	}
	volume.CreatedAt = time.Now().UTC()
	if err := m.save(volume); err != nil {
		return Volume{}, err
	}
	return volume, nil
}

func (m *Manager) validate(volume *Volume, op string) error {
	if volume.ID == "" || strings.ContainsAny(volume.ID, "/\\ ") {
		return fmt.Errorf("%w: invalid volume id %q", ErrInvalid, volume.ID)
	}
	if volume.Owner.Application == "" {
		return fmt.Errorf("%w: missing owner application", ErrInvalid)
	}
	switch volume.Kind {
	case KindEphemeral, KindManaged, KindPVC:
		if volume.AccessMode == "" {
			volume.AccessMode = AccessReadWriteOnce
		}
		if volume.AccessMode == AccessReadWriteMany {
			return fmt.Errorf("%w: ReadWriteMany volumes are not supported", ErrUnsupported)
		}
		if volume.CapacityBytes < 0 {
			return fmt.Errorf("%w: negative capacity", ErrInvalid)
		}
	case KindBind:
		if volume.Source == "" {
			return fmt.Errorf("%w: bind volume needs a source", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unknown volume kind %q", ErrInvalid, volume.Kind)
	}
	return nil
}

// Attach leases a volume to a sandbox and returns the resolved mount.
func (m *Manager) Attach(ctx context.Context, volumeID, sandboxID string, readOnly bool, uid, gid int, op string) (Attachment, error) {
	if sandboxID == "" {
		return Attachment{}, fmt.Errorf("%w: missing sandbox id", ErrInvalid)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	volume, err := m.load(volumeID)
	if err != nil {
		return Attachment{}, err
	}
	if volume.ReadOnly && !readOnly {
		return Attachment{}, fmt.Errorf("%w: %q", ErrReadOnly, volumeID)
	}
	leases, err := m.leases(volumeID)
	if err != nil {
		return Attachment{}, err
	}
	if err := checkAccess(volume, leases, sandboxID, readOnly); err != nil {
		return Attachment{}, err
	}
	lease := Lease{
		VolumeID:  volumeID,
		SandboxID: sandboxID,
		ReadOnly:  readOnly || volume.ReadOnly,
		UID:       uid,
		GID:       gid,
		Operation: op,
		UpdatedAt: time.Now().UTC(),
	}
	if err := m.saveLease(lease); err != nil {
		return Attachment{}, err
	}
	return Attachment{
		VolumeID: volumeID,
		Source:   volume.Source,
		ReadOnly: lease.ReadOnly,
		UID:      uid,
		GID:      gid,
	}, nil
}

func checkAccess(volume Volume, leases []Lease, sandboxID string, readOnly bool) error {
	for _, lease := range leases {
		if lease.SandboxID == sandboxID {
			continue
		}
		switch volume.AccessMode {
		case AccessReadOnlyMany:
			if !readOnly && !volume.ReadOnly {
				return fmt.Errorf("%w: ReadOnlyMany volume %q cannot be attached read-write", ErrReadOnly, volume.ID)
			}
		default: // ReadWriteOnce: one writer, or any number of read-only readers.
			if !lease.ReadOnly || !readOnly {
				return fmt.Errorf("%w: ReadWriteOnce volume %q is attached to sandbox %q", ErrBusy, volume.ID, lease.SandboxID)
			}
		}
	}
	return nil
}

// Detach releases a sandbox's lease on a volume. Detaching an absent lease
// succeeds.
func (m *Manager) Detach(ctx context.Context, volumeID, sandboxID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	err := os.Remove(m.leasePath(volumeID, sandboxID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// ReleaseSandbox detaches every volume leased to a sandbox.
func (m *Manager) ReleaseSandbox(ctx context.Context, sandboxID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries, err := os.ReadDir(m.leasesDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		leasePath := filepath.Join(m.leasesDir, entry.Name(), sandboxID+".json")
		if err := os.Remove(leasePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// Reconcile drops leases whose sandbox no longer exists, so a crashed daemon
// never leaves a permanent lock.
func (m *Manager) Reconcile(exists func(sandboxID string) bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	volumes, err := os.ReadDir(m.leasesDir)
	if err != nil {
		return err
	}
	for _, volDir := range volumes {
		leases, err := os.ReadDir(filepath.Join(m.leasesDir, volDir.Name()))
		if err != nil {
			continue
		}
		for _, leaseFile := range leases {
			sandboxID := strings.TrimSuffix(leaseFile.Name(), ".json")
			if exists(sandboxID) {
				continue
			}
			_ = os.Remove(filepath.Join(m.leasesDir, volDir.Name(), leaseFile.Name()))
		}
	}
	return nil
}

// Delete removes a managed/PVC/ephemeral volume. Bind/external volumes are never
// deleted, and a leased volume is refused.
func (m *Manager) Delete(ctx context.Context, volumeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	volume, err := m.load(volumeID)
	if err != nil {
		return err
	}
	if volume.Kind == KindBind {
		return fmt.Errorf("%w: refusing to delete bind volume %q", ErrNotOwned, volumeID)
	}
	leases, err := m.leases(volumeID)
	if err != nil {
		return err
	}
	if len(leases) > 0 {
		return fmt.Errorf("%w: volume %q has %d leases", ErrBusy, volumeID, len(leases))
	}
	if err := os.RemoveAll(m.volumeDir(volumeID)); err != nil {
		return err
	}
	return nil
}

// DeleteOwned deletes only managed/PVC volumes owned by an application. It is the
// safe `down --volumes` primitive: bind/external volumes are untouched.
func (m *Manager) DeleteOwned(ctx context.Context, application string, includeEphemeral bool) ([]string, error) {
	m.mu.Lock()
	volumes, err := m.listLocked()
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	var deleted []string
	for _, volume := range volumes {
		if volume.Owner.Application != application {
			continue
		}
		if volume.Kind == KindBind {
			continue
		}
		if volume.Kind == KindEphemeral && !includeEphemeral {
			continue
		}
		if err := m.Delete(ctx, volume.ID); err != nil {
			if errors.Is(err, ErrBusy) {
				continue
			}
			return deleted, err
		}
		deleted = append(deleted, volume.ID)
	}
	return deleted, nil
}

// List returns all volumes sorted by ID.
func (m *Manager) List() ([]Volume, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listLocked()
}

// Inspect returns a volume and its leases.
func (m *Manager) Inspect(volumeID string) (Volume, []Lease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	volume, err := m.load(volumeID)
	if err != nil {
		return Volume{}, nil, err
	}
	leases, err := m.leases(volumeID)
	return volume, leases, err
}

func (m *Manager) listLocked() ([]Volume, error) {
	entries, err := os.ReadDir(m.volumesDir)
	if err != nil {
		return nil, err
	}
	volumes := make([]Volume, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		volume, err := m.load(entry.Name())
		if err != nil {
			continue
		}
		volumes = append(volumes, volume)
	}
	sort.Slice(volumes, func(i, j int) bool { return volumes[i].ID < volumes[j].ID })
	return volumes, nil
}

func (m *Manager) volumeDir(id string) string { return filepath.Join(m.volumesDir, id) }
func (m *Manager) recordPath(id string) string {
	return filepath.Join(m.volumeDir(id), "volume.json")
}
func (m *Manager) leasePath(volumeID, sandboxID string) string {
	return filepath.Join(m.leasesDir, volumeID, sandboxID+".json")
}

func (m *Manager) load(id string) (Volume, error) {
	data, err := os.ReadFile(m.recordPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return Volume{}, fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	if err != nil {
		return Volume{}, err
	}
	var volume Volume
	if err := json.Unmarshal(data, &volume); err != nil {
		return Volume{}, fmt.Errorf("storage: decode volume %q: %w", id, err)
	}
	return volume, nil
}

func (m *Manager) save(volume Volume) error {
	data, err := json.MarshalIndent(volume, "", "  ")
	if err != nil {
		return err
	}
	return m.atomicWrite(m.volumeDir(volume.ID), "volume.json", append(data, '\n'))
}

func (m *Manager) saveLease(lease Lease) error {
	dir := filepath.Join(m.leasesDir, lease.VolumeID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(lease, "", "  ")
	if err != nil {
		return err
	}
	return m.atomicWrite(dir, lease.SandboxID+".json", append(data, '\n'))
}

func (m *Manager) leases(volumeID string) ([]Lease, error) {
	dir := filepath.Join(m.leasesDir, volumeID)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	leases := make([]Lease, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var lease Lease
		if err := json.Unmarshal(data, &lease); err == nil {
			leases = append(leases, lease)
		}
	}
	return leases, nil
}

// atomicWrite writes via state.AtomicWriteFile, which fsyncs the file and its
// directory before renaming into place.
func (m *Manager) atomicWrite(dir, name string, data []byte) error {
	lock, err := os.OpenFile(filepath.Join(dir, "."+name+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		return err
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	return state.AtomicWriteFile(dir, name, data, 0o600, state.DefaultOps())
}
