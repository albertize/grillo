// SPDX-License-Identifier: Apache-2.0

package reconcile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/plan"
	"github.com/albertize/grillo/internal/state"
)

// ApplicationRecord atomically associates intent with progress. Down intent is
// durable before effects, including an interrupted explicit volume removal.
type ApplicationRecord struct {
	Desired       model.Application `json:"desired"`
	Observed      plan.Observed     `json:"observed"`
	Stopped       bool              `json:"stopped"`
	RemoveVolumes bool              `json:"removeVolumes,omitempty"`
}

type persistentDocument struct {
	Version      int                          `json:"version"`
	Applications map[string]ApplicationRecord `json:"applications"`
}

// PersistentStore is owned by the daemon holding the state writer lock.
type PersistentStore struct {
	mu  sync.Mutex
	dir string
	doc persistentDocument
	ops state.Ops
}

func OpenPersistentStore(dir string) (*PersistentStore, error) {
	s := &PersistentStore{dir: dir, ops: state.DefaultOps(), doc: persistentDocument{Version: 1, Applications: map[string]ApplicationRecord{}}}
	data, err := os.ReadFile(dir + "/applications.json")
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.doc); err != nil {
		return nil, fmt.Errorf("reconcile: corrupt applications: %w", err)
	}
	if s.doc.Version != 1 || s.doc.Applications == nil {
		return nil, fmt.Errorf("reconcile: unsupported application state version or missing records")
	}
	for name, r := range s.doc.Applications {
		if name != r.Desired.Identity.Name {
			return nil, fmt.Errorf("reconcile: inconsistent application identity")
		}
	}
	return s, nil
}

func (s *PersistentStore) put(name string, r ApplicationRecord) error {
	// Copy-on-write: a failed commit must not advance the in-memory snapshot.
	next := persistentDocument{Version: 1, Applications: make(map[string]ApplicationRecord, len(s.doc.Applications)+1)}
	for k, v := range s.doc.Applications {
		next.Applications[k] = v
	}
	next.Applications[name] = r
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err := state.AtomicWriteFile(s.dir, "applications.json", data, 0600, s.ops); err != nil {
		return err
	}
	s.doc = next
	return nil
}

func (s *PersistentStore) SetDesired(app model.Application) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.doc.Applications[app.Identity.Name]
	// Detach all caller-owned slices/maps.
	data, err := json.Marshal(app)
	if err != nil {
		return err
	}
	r.Desired = model.Application{}
	if err := json.Unmarshal(data, &r.Desired); err != nil {
		return err
	}
	r.Stopped, r.RemoveVolumes = false, false
	return s.put(app.Identity.Name, r)
}

func (s *PersistentStore) SetStopped(name string, remove bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.doc.Applications[name]
	if !ok {
		return nil
	}
	r.Stopped, r.RemoveVolumes = true, remove || r.RemoveVolumes
	return s.put(name, r)
}

func (s *PersistentStore) Load(name string) (plan.Observed, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneObserved(s.doc.Applications[name].Observed), nil
}

func (s *PersistentStore) Save(name string, observed plan.Observed) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.doc.Applications[name]
	if !ok {
		return fmt.Errorf("reconcile: no durable intent for %q", name)
	}
	r.Observed = cloneObserved(observed)
	return s.put(name, r)
}

func (s *PersistentStore) Records() []ApplicationRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.doc.Applications))
	for name := range s.doc.Applications {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []ApplicationRecord
	for _, name := range names {
		data, _ := json.Marshal(s.doc.Applications[name])
		var r ApplicationRecord
		_ = json.Unmarshal(data, &r)
		out = append(out, r)
	}
	return out
}
