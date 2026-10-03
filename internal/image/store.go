// SPDX-License-Identifier: Apache-2.0

// Package image records the images Grillo has pulled or built and garbage
// collects unused content-addressed blobs while preserving pinned and active
// data.
package image

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"grillo.local/grillo/internal/oci"
)

// ErrNotFound is returned when a reference is not in the store.
var ErrNotFound = errors.New("image: reference not found")

// Source describes how an image record entered the store.
type Source string

const (
	SourcePull   Source = "pull"
	SourceBuild  Source = "build"
	SourceImport Source = "import"
)

type Record struct {
	Reference      string            `json:"reference"`
	ManifestDigest string            `json:"manifestDigest"`
	ConfigDigest   string            `json:"configDigest,omitempty"`
	Source         Source            `json:"source"`
	CreatedAt      time.Time         `json:"createdAt"`
	Labels         map[string]string `json:"labels,omitempty"`
	Blobs          []string          `json:"blobs"`
	LayerCount     int               `json:"layerCount"`
	Size           int64             `json:"size"`
	Pinned         bool              `json:"pinned,omitempty"`
}

// Meta carries caller-supplied record metadata.
type Meta struct {
	Reference string
	Source    Source
	Labels    map[string]string
	CreatedAt time.Time
	Pinned    bool
}

// PruneResult summarizes a garbage collection pass.
type PruneResult struct {
	RemovedRecords []Record
	RemovedBlobs   int
	FreedBytes     int64
}

// Store is a directory of image records backed by an OCI CAS.
type Store struct {
	dir string
	cas *oci.CAS

	mu sync.Mutex
}

// Open prepares an image store. The CAS may be nil, in which case import
// verification is skipped and prune only removes records.
func Open(dir string, cas *oci.CAS) (*Store, error) {
	store := &Store{dir: dir, cas: cas}
	for _, sub := range []string{store.recordsDir(), store.dir} {
		if err := os.MkdirAll(sub, 0o700); err != nil {
			return nil, err
		}
	}
	return store, nil
}

// Dir returns the store root.
func (s *Store) Dir() string { return s.dir }

func (s *Store) recordsDir() string { return filepath.Join(s.dir, "records") }
func (s *Store) pinsPath() string   { return filepath.Join(s.dir, "pins.json") }

func (s *Store) recordPath(reference string) string {
	sum := sha256.Sum256([]byte(reference))
	return filepath.Join(s.recordsDir(), hex.EncodeToString(sum[:])+".json")
}

// Import records a resolved image. When a CAS is configured every referenced
// blob must already be present, so the record never points at missing content.
func (s *Store) Import(pulled oci.PulledImage, meta Meta) (Record, error) {
	reference := meta.Reference
	if reference == "" {
		reference = pulled.Reference.String()
	}
	if reference == "" {
		return Record{}, errors.New("image: import requires a reference")
	}
	if pulled.ManifestDigest == "" {
		return Record{}, errors.New("image: import requires a manifest digest")
	}
	blobs := []string{pulled.ManifestDigest, pulled.Manifest.Config.Digest}
	var size int64 = pulled.Manifest.Config.Size
	for _, layer := range pulled.Manifest.Layers {
		blobs = append(blobs, layer.Digest)
		size += layer.Size
	}
	if s.cas != nil {
		for _, blob := range blobs {
			if !s.cas.Has(blob) {
				return Record{}, fmt.Errorf("image: blob %s missing from the CAS", blob)
			}
		}
		if data, err := os.Stat(mustPath(s.cas, pulled.ManifestDigest)); err == nil {
			size += data.Size()
		}
	}
	created := meta.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	source := meta.Source
	if source == "" {
		source = SourceImport
	}
	record := Record{
		Reference:      reference,
		ManifestDigest: pulled.ManifestDigest,
		ConfigDigest:   pulled.Manifest.Config.Digest,
		Source:         source,
		CreatedAt:      created.UTC(),
		Labels:         meta.Labels,
		Blobs:          blobs,
		LayerCount:     len(pulled.Manifest.Layers),
		Size:           size,
		Pinned:         meta.Pinned,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeRecord(s.recordPath(reference), record); err != nil {
		return Record{}, err
	}
	return record, nil
}

// Require is Get with an ErrNotFound error.
func (s *Store) Require(reference string) (Record, error) {
	record, ok, err := s.Get(reference)
	if err != nil {
		return Record{}, err
	}
	if !ok {
		return Record{}, fmt.Errorf("%w: %s", ErrNotFound, reference)
	}
	return record, nil
}

// Get looks up a record by reference.
func (s *Store) Get(reference string) (Record, bool, error) {
	data, err := os.ReadFile(s.recordPath(reference))
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, err
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, false, fmt.Errorf("image: corrupt record for %q: %w", reference, err)
	}
	return record, true, nil
}

// List returns the inventory in deterministic reference order.
func (s *Store) List() ([]Record, error) {
	entries, err := os.ReadDir(s.recordsDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var records []Record
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.recordsDir(), entry.Name()))
		if err != nil {
			return nil, err
		}
		var record Record
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, fmt.Errorf("image: corrupt record %s: %w", entry.Name(), err)
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Reference < records[j].Reference })
	return records, nil
}

// Pin marks or unmarks a stored record as protected from pruning.
func (s *Store) Pin(reference string, pinned bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok, err := s.Get(reference)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("image: unknown reference %q", reference)
	}
	record.Pinned = pinned
	return writeRecord(s.recordPath(reference), record)
}

// PinDigest protects a CAS blob that is not referenced by any record.
func (s *Store) PinDigest(digest string, pinned bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pins, err := s.pinnedDigests()
	if err != nil {
		return err
	}
	set := map[string]bool{}
	for _, pin := range pins {
		set[pin] = true
	}
	if pinned {
		set[digest] = true
	} else {
		delete(set, digest)
	}
	list := make([]string, 0, len(set))
	for pin := range set {
		list = append(list, pin)
	}
	sort.Strings(list)
	return writeJSON(s.pinsPath(), list)
}

func (s *Store) pinnedDigests() ([]string, error) {
	data, err := os.ReadFile(s.pinsPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var pins []string
	if err := json.Unmarshal(data, &pins); err != nil {
		return nil, fmt.Errorf("image: corrupt pins: %w", err)
	}
	return pins, nil
}

// Remove deletes a record. It never deletes blobs; run Prune for that.
func (s *Store) Remove(reference string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.recordPath(reference))
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("image: unknown reference %q", reference)
	}
	return err
}

// Prune removes records that are neither pinned nor kept, then collects every
// CAS blob not reachable from a remaining record or explicit digest pin.
func (s *Store) Prune(ctx context.Context, keep func(Record) bool) (PruneResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.List()
	if err != nil {
		return PruneResult{}, err
	}
	keepBlobs := map[string]bool{}
	var removed []Record
	for _, record := range records {
		active := record.Pinned || (keep != nil && keep(record))
		if active {
			for _, blob := range record.Blobs {
				keepBlobs[blob] = true
			}
			continue
		}
		removed = append(removed, record)
	}
	pins, err := s.pinnedDigests()
	if err != nil {
		return PruneResult{}, err
	}
	for _, pin := range pins {
		keepBlobs[pin] = true
	}

	result := PruneResult{RemovedRecords: removed}
	candidates := map[string]bool{}
	for _, record := range removed {
		if err := os.Remove(s.recordPath(record.Reference)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return PruneResult{}, err
		}
		for _, blob := range record.Blobs {
			if !keepBlobs[blob] {
				candidates[blob] = true
			}
		}
	}
	if s.cas != nil {
		for blob := range candidates {
			if path, err := s.cas.Path(blob); err == nil {
				if info, statErr := os.Stat(path); statErr == nil {
					result.FreedBytes += info.Size()
				}
			}
		}
		count, err := s.cas.Collect(func(digest string) bool { return keepBlobs[digest] })
		if err != nil {
			return PruneResult{}, err
		}
		result.RemovedBlobs = count
	}
	return result, nil
}

// Verify reports whether every blob of a record is present in the CAS.
func (s *Store) Verify(reference string) (bool, error) {
	record, ok, err := s.Get(reference)
	if err != nil || !ok {
		return false, err
	}
	if s.cas == nil {
		return true, nil
	}
	for _, blob := range record.Blobs {
		if !s.cas.Has(blob) {
			return false, nil
		}
	}
	return true, nil
}

func writeRecord(path string, record Record) error { return writeJSON(path, record) }

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func mustPath(cas *oci.CAS, digest string) string {
	path, err := cas.Path(digest)
	if err != nil {
		return ""
	}
	return path
}
