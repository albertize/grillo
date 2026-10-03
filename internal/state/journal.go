// SPDX-License-Identifier: Apache-2.0

package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultMaxJournalBytes = 4 << 20 // 4 MiB per file
	defaultMaxRotated      = 3
)

// Event is one append-only journal record with a persistent sequence ID.
type Event struct {
	Sequence uint64            `json:"seq"`
	Time     time.Time         `json:"time"`
	Kind     string            `json:"kind"`
	Resource string            `json:"resource,omitempty"`
	Source   string            `json:"source,omitempty"`
	Message  string            `json:"message,omitempty"`
	Reason   string            `json:"reason,omitempty"`
	Fields   map[string]string `json:"fields,omitempty"`
}

// JournalOptions bounds the journal. Zero values use the defaults.
type JournalOptions struct {
	MaxBytes   int64
	MaxRotated int
}

// Journal is an append-only NDJSON event log with rotation.
type Journal struct {
	dir        string
	maxBytes   int64
	maxRotated int
	nextSeq    uint64
	file       *os.File
}

// OpenJournal opens (creating if needed) the journal in dir.
func OpenJournal(dir string, opts JournalOptions) (*Journal, error) {
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = defaultMaxJournalBytes
	}
	if opts.MaxRotated <= 0 {
		opts.MaxRotated = defaultMaxRotated
	}
	if err := ensurePrivateDir(dir, os.Getuid()); err != nil {
		return nil, err
	}
	j := &Journal{dir: dir, maxBytes: opts.MaxBytes, maxRotated: opts.MaxRotated}
	max, err := j.maxSequence()
	if err != nil {
		return nil, err
	}
	j.nextSeq = max + 1
	if err := j.open(); err != nil {
		return nil, err
	}
	return j, nil
}

// OpenJournal opens the journal inside the store's state directory.
func (s *Store) OpenJournal(opts JournalOptions) (*Journal, error) {
	return OpenJournal(s.layout.State, opts)
}

// Path returns the current journal file path.
func (j *Journal) Path() string { return j.currentPath() }

// Close flushes and closes the current file. It is safe to call twice.
func (j *Journal) Close() error {
	if j.file == nil {
		return nil
	}
	err := j.file.Close()
	j.file = nil
	return err
}

// Append writes one event, assigning the next persistent sequence ID. The file
// is rotated before writing if it would exceed MaxBytes.
func (j *Journal) Append(e Event) (Event, error) {
	if e.Sequence == 0 {
		e.Sequence = j.nextSeq
	}
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	data, err := json.Marshal(e)
	if err != nil {
		return Event{}, fmt.Errorf("state: encode event: %w", err)
	}
	data = append(data, '\n')
	if info, statErr := j.file.Stat(); statErr == nil && info.Size()+int64(len(data)) > j.maxBytes {
		if err := j.rotate(); err != nil {
			return Event{}, err
		}
	}
	if _, err := j.file.Write(data); err != nil {
		return Event{}, fmt.Errorf("state: append event: %w", err)
	}
	j.nextSeq = e.Sequence + 1
	return e, nil
}

// Read returns all retained events ordered by sequence. A truncated final line
// is recovered by ignoring it; corruption in the middle is reported.
func (j *Journal) Read() ([]Event, error) {
	var events []Event
	for _, path := range j.filesOldestFirst() {
		fileEvents, err := readEventFile(path)
		if err != nil {
			return nil, err
		}
		events = append(events, fileEvents...)
	}
	sort.SliceStable(events, func(a, b int) bool { return events[a].Sequence < events[b].Sequence })
	return events, nil
}

func readEventFile(path string) ([]Event, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("state: read journal: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	truncatedLast := len(lines) > 0 && lines[len(lines)-1] != ""
	var events []Event
	for i, line := range lines {
		if line == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			if truncatedLast && i == len(lines)-1 {
				break // a partial final write is recoverable
			}
			return nil, fmt.Errorf("%w: bad journal line %d in %s", ErrCorrupt, i+1, path)
		}
		events = append(events, e)
	}
	return events, nil
}

func (j *Journal) open() error {
	f, err := os.OpenFile(j.currentPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("state: open journal: %w", err)
	}
	j.file = f
	return nil
}

func (j *Journal) rotate() error {
	if err := j.file.Close(); err != nil {
		return err
	}
	j.file = nil
	// Shift events.1 -> events.2, etc., oldest last, then move current to .1.
	for i := j.maxRotated - 1; i >= 1; i-- {
		from := j.rotatedPath(i)
		to := j.rotatedPath(i + 1)
		if _, err := os.Stat(from); err == nil {
			if err := os.Rename(from, to); err != nil {
				return fmt.Errorf("state: rotate journal: %w", err)
			}
		}
	}
	if err := os.Rename(j.currentPath(), j.rotatedPath(1)); err != nil {
		return fmt.Errorf("state: rotate journal: %w", err)
	}
	if err := os.Remove(j.rotatedPath(j.maxRotated + 1)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("state: prune journal: %w", err)
	}
	return j.open()
}

func (j *Journal) maxSequence() (uint64, error) {
	var max uint64
	for _, path := range j.filesOldestFirst() {
		events, err := readEventFile(path)
		if err != nil {
			return 0, err
		}
		for _, e := range events {
			if e.Sequence > max {
				max = e.Sequence
			}
		}
	}
	return max, nil
}

func (j *Journal) filesOldestFirst() []string {
	paths := make([]string, 0, j.maxRotated+1)
	for i := j.maxRotated; i >= 1; i-- {
		paths = append(paths, j.rotatedPath(i))
	}
	return append(paths, j.currentPath())
}

func (j *Journal) currentPath() string { return filepath.Join(j.dir, "events.ndjson") }

func (j *Journal) rotatedPath(n int) string {
	return filepath.Join(j.dir, fmt.Sprintf("events.%d.ndjson", n))
}
