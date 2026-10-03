// SPDX-License-Identifier: Apache-2.0

package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	"grillo.local/grillo/internal/model"
)

// Version is the on-disk state schema version, independent of the IR and API
// versions.
type Version uint32

// CurrentVersion is the schema version this build writes.
const CurrentVersion Version = 1

var (
	// ErrWriterBusy means another process holds the single-writer lock.
	ErrWriterBusy = errors.New("state: another writer holds the lock")
	// ErrFutureVersion means the state was written by a newer build.
	ErrFutureVersion = errors.New("state: state version is newer than this build")
	// ErrCorrupt means the state file could not be parsed or safely migrated.
	ErrCorrupt = errors.New("state: state is corrupt")
)

// OperationStatus is the progress of a pending or finished operation.
type OperationStatus string

const (
	OperationPending   OperationStatus = "pending"
	OperationSucceeded OperationStatus = "succeeded"
	OperationFailed    OperationStatus = "failed"
)

// Operation is an idempotent unit of work recorded with the desired state.
type Operation struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Status    OperationStatus `json:"status"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
	Message   string          `json:"message,omitempty"`
}

// Observation is the last observed state of one resource.
type Observation struct {
	Resource  string    `json:"resource"`
	Status    string    `json:"status"`
	Detail    string    `json:"detail,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Snapshot is the single application document: desired state plus pending
// operations and observations. It never contains secret values.
type Snapshot struct {
	Version      Version            `json:"version"`
	Application  *model.Application `json:"application,omitempty"`
	Operations   []Operation        `json:"operations,omitempty"`
	Observations []Observation      `json:"observations,omitempty"`
}

// Store owns the state directory and the single-writer lock.
type Store struct {
	layout Layout
	ops    Ops
	lock   *os.File
}

// Open prepares the layout and acquires the exclusive writer lock. A second
// writer for the same runtime directory returns ErrWriterBusy.
func Open(layout Layout, ops Ops) (*Store, error) {
	if ops.CreateTemp == nil {
		ops = DefaultOps()
	}
	if err := layout.Prepare(); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(layout.Runtime, "lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("state: open lock: %w", err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrWriterBusy
		}
		return nil, fmt.Errorf("state: acquire lock: %w", err)
	}
	return &Store{layout: layout, ops: ops, lock: f}, nil
}

// Close releases the writer lock. It is safe to call more than once.
func (s *Store) Close() error {
	if s.lock == nil {
		return nil
	}
	unlockErr := unix.Flock(int(s.lock.Fd()), unix.LOCK_UN)
	closeErr := s.lock.Close()
	s.lock = nil
	return errors.Join(unlockErr, closeErr)
}

// Layout returns the store's directory layout.
func (s *Store) Layout() Layout { return s.layout }

// SnapshotPath is the desired-state file path.
func (s *Store) SnapshotPath() string { return filepath.Join(s.layout.State, "state.json") }

// Load reads the snapshot. A missing file is an empty current-version snapshot;
// a future version is rejected; a legacy version is backed up and migrated.
func (s *Store) Load() (Snapshot, error) {
	data, err := os.ReadFile(s.SnapshotPath())
	if errors.Is(err, fs.ErrNotExist) {
		return Snapshot{Version: CurrentVersion}, nil
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("state: read snapshot: %w", err)
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if snap.Version > CurrentVersion {
		return Snapshot{}, fmt.Errorf("%w: found %d, current %d", ErrFutureVersion, snap.Version, CurrentVersion)
	}
	if snap.Version < CurrentVersion {
		return s.migrate(snap)
	}
	return snap, nil
}

// Commit atomically writes the snapshot with restrictive permissions.
func (s *Store) Commit(snap Snapshot) error {
	snap.Version = CurrentVersion
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("state: encode snapshot: %w", err)
	}
	data = append(data, '\n')
	if err := AtomicWriteFile(s.layout.State, "state.json", data, 0o600, s.ops); err != nil {
		return fmt.Errorf("state: commit: %w", err)
	}
	return nil
}

// migrate upgrades an older schema, backing up the original file first. Only the
// legacy unversioned schema (version 0) is understood today.
func (s *Store) migrate(snap Snapshot) (Snapshot, error) {
	if snap.Version != 0 {
		return Snapshot{}, fmt.Errorf("%w: no migration from version %d", ErrCorrupt, snap.Version)
	}
	if err := s.backup(snap.Version); err != nil {
		return Snapshot{}, err
	}
	snap.Version = CurrentVersion
	return snap, nil
}

func (s *Store) backup(from Version) error {
	data, err := os.ReadFile(s.SnapshotPath())
	if err != nil {
		return fmt.Errorf("state: backup read: %w", err)
	}
	name := fmt.Sprintf("state.v%d.bak", from)
	if err := AtomicWriteFile(s.layout.State, name, data, 0o600, s.ops); err != nil {
		return fmt.Errorf("state: backup: %w", err)
	}
	return nil
}
