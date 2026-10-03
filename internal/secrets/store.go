// SPDX-License-Identifier: Apache-2.0

// Package secrets stores secret values separately from public state. Values are
// never placed in the IR, state snapshots, diagnostics, or DTOs; callers receive
// them only through Get. Versions use random tokens that change only when the
// value changes, so a public version cannot be used to guess a low-entropy
// secret.
package secrets

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"grillo.local/grillo/internal/model"
	"grillo.local/grillo/internal/state"
)

const indexVersion uint32 = 1

var (
	// ErrNotFound means no stored secret matches the reference.
	ErrNotFound = errors.New("secrets: not found")
	// ErrVersionMismatch means the stored version differs from the reference.
	ErrVersionMismatch = errors.New("secrets: version mismatch")
)

type indexEntry struct {
	Name      string    `json:"name"`
	ID        string    `json:"id"`
	Version   string    `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
}

type indexFile struct {
	Version uint32       `json:"version"`
	Entries []indexEntry `json:"entries"`
}

type secretFile struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Version string            `json:"version"`
	Values  map[string][]byte `json:"values"`
}

// Store is a per-user secret store directory.
type Store struct {
	dir   string
	ops   state.Ops
	index indexFile
}

// Open prepares the store directory and loads its index.
func Open(dir string, ops state.Ops) (*Store, error) {
	if ops.CreateTemp == nil {
		ops = state.DefaultOps()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("secrets: create store: %w", err)
	}
	s := &Store{dir: dir, ops: ops}
	if err := s.loadIndex(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) loadIndex() error {
	data, err := os.ReadFile(s.indexPath())
	if errors.Is(err, fs.ErrNotExist) {
		s.index = indexFile{Version: indexVersion}
		return nil
	}
	if err != nil {
		return fmt.Errorf("secrets: read index: %w", err)
	}
	if err := json.Unmarshal(data, &s.index); err != nil {
		return fmt.Errorf("secrets: decode index: %w", err)
	}
	return nil
}

func (s *Store) saveIndex() error {
	s.index.Version = indexVersion
	data, err := json.MarshalIndent(s.index, "", "  ")
	if err != nil {
		return fmt.Errorf("secrets: encode index: %w", err)
	}
	return state.AtomicWriteFile(s.dir, "index.json", append(data, '\n'), 0o600, s.ops)
}

// Put stores values for a name and returns a reference. Re-putting identical
// values reuses the existing version; changing a value creates a new random
// version.
func (s *Store) Put(name string, values map[string][]byte) (model.SecretRef, error) {
	if name == "" {
		return model.SecretRef{}, errors.New("secrets: empty name")
	}
	if entry, ok := s.findLatest(name); ok {
		existing, err := s.read(entry.ID)
		if err != nil {
			return model.SecretRef{}, err
		}
		if valuesEqual(existing.Values, values) {
			return model.SecretRef{Name: name, ID: entry.ID, Version: entry.Version}, nil
		}
	}
	id, err := randomToken()
	if err != nil {
		return model.SecretRef{}, err
	}
	version, err := randomToken()
	if err != nil {
		return model.SecretRef{}, err
	}
	if err := s.write(id, secretFile{ID: id, Name: name, Version: version, Values: cloneValues(values)}); err != nil {
		return model.SecretRef{}, err
	}
	s.index.Entries = append(s.index.Entries, indexEntry{Name: name, ID: id, Version: version, CreatedAt: time.Now().UTC()})
	if err := s.saveIndex(); err != nil {
		return model.SecretRef{}, err
	}
	return model.SecretRef{Name: name, ID: id, Version: version}, nil
}

// Get returns a copy of the values a reference points at.
func (s *Store) Get(ref model.SecretRef) (map[string][]byte, error) {
	if ref.ID == "" {
		entry, ok := s.findByNameVersion(ref.Name, ref.Version)
		if !ok {
			return nil, ErrNotFound
		}
		ref.ID = entry.ID
	}
	stored, err := s.read(ref.ID)
	if err != nil {
		return nil, err
	}
	if ref.Version != "" && stored.Version != ref.Version {
		return nil, ErrVersionMismatch
	}
	if ref.Name != "" && stored.Name != ref.Name {
		return nil, ErrNotFound
	}
	return cloneValues(stored.Values), nil
}

// Refs returns public references only (no values), sorted for determinism.
func (s *Store) Refs() []model.SecretRef {
	refs := make([]model.SecretRef, 0, len(s.index.Entries))
	for _, e := range s.index.Entries {
		refs = append(refs, model.SecretRef{Name: e.Name, ID: e.ID, Version: e.Version})
	}
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].Name != refs[j].Name {
			return refs[i].Name < refs[j].Name
		}
		return refs[i].ID < refs[j].ID
	})
	return refs
}

// Collect removes stored versions not reported as referenced and returns the
// number removed.
func (s *Store) Collect(referenced func(model.SecretRef) bool) (int, error) {
	kept := s.index.Entries[:0:0]
	removed := 0
	for _, e := range s.index.Entries {
		ref := model.SecretRef{Name: e.Name, ID: e.ID, Version: e.Version}
		if referenced(ref) {
			kept = append(kept, e)
			continue
		}
		if err := os.Remove(s.secretPath(e.ID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return removed, fmt.Errorf("secrets: remove %s: %w", e.ID, err)
		}
		removed++
	}
	s.index.Entries = kept
	if err := s.saveIndex(); err != nil {
		return removed, err
	}
	return removed, nil
}

func (s *Store) findLatest(name string) (indexEntry, bool) {
	var latest indexEntry
	var found bool
	for _, e := range s.index.Entries {
		if e.Name != name {
			continue
		}
		if !found || e.CreatedAt.After(latest.CreatedAt) {
			latest, found = e, true
		}
	}
	return latest, found
}

func (s *Store) findByNameVersion(name, version string) (indexEntry, bool) {
	for _, e := range s.index.Entries {
		if e.Name == name && (version == "" || e.Version == version) {
			return e, true
		}
	}
	// Fall back to the newest version when none matched exactly.
	if version == "" {
		return s.findLatest(name)
	}
	return indexEntry{}, false
}

func (s *Store) read(id string) (secretFile, error) {
	data, err := os.ReadFile(s.secretPath(id))
	if errors.Is(err, fs.ErrNotExist) {
		return secretFile{}, ErrNotFound
	}
	if err != nil {
		return secretFile{}, fmt.Errorf("secrets: read %s: %w", id, err)
	}
	var stored secretFile
	if err := json.Unmarshal(data, &stored); err != nil {
		return secretFile{}, fmt.Errorf("secrets: decode %s: %w", id, err)
	}
	return stored, nil
}

func (s *Store) write(id string, stored secretFile) error {
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("secrets: encode %s: %w", id, err)
	}
	return state.AtomicWriteFile(s.dir, id+".json", append(data, '\n'), 0o600, s.ops)
}

func (s *Store) indexPath() string           { return filepath.Join(s.dir, "index.json") }
func (s *Store) secretPath(id string) string { return filepath.Join(s.dir, id+".json") }

func randomToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("secrets: random token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func valuesEqual(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !bytes.Equal(v, b[k]) {
			return false
		}
	}
	return true
}

func cloneValues(values map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(values))
	for k, v := range values {
		out[k] = append([]byte(nil), v...)
	}
	return out
}
