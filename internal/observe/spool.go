//go:build linux

// SPDX-License-Identifier: Apache-2.0

package observe

import (
	"context"
	"time"

	"github.com/albertize/grillo/internal/state"
)

// MaxLogLineBytes bounds a single log record so a runaway line cannot exhaust the
// spool.
const MaxLogLineBytes = 64 << 10

// LogRecord is one line of container output.
type LogRecord struct {
	Sequence  uint64    `json:"seq"`
	Time      time.Time `json:"time"`
	Resource  string    `json:"resource"`
	Container string    `json:"container,omitempty"`
	Stream    string    `json:"stream,omitempty"` // stdout or stderr
	Line      string    `json:"line"`
}

// LogSpool is a bounded, rotated NDJSON log store with follow support.
type LogSpool struct {
	journal *state.Journal
}

// OpenLogSpool opens (or creates) a log spool in dir.
func OpenLogSpool(dir string, maxBytes int64, maxRotated int) (*LogSpool, error) {
	journal, err := state.OpenJournal(dir, state.JournalOptions{MaxBytes: maxBytes, MaxRotated: maxRotated})
	if err != nil {
		return nil, err
	}
	return &LogSpool{journal: journal}, nil
}

// Append stores a line, truncating it to MaxLogLineBytes.
func (s *LogSpool) Append(record LogRecord) (LogRecord, error) {
	line := record.Line
	if len(line) > MaxLogLineBytes {
		line = line[:MaxLogLineBytes]
	}
	event, err := s.journal.Append(state.Event{
		Kind:     "log",
		Resource: record.Resource,
		Message:  line,
		Fields:   map[string]string{"container": record.Container, "stream": record.Stream},
	})
	if err != nil {
		return LogRecord{}, err
	}
	record.Sequence = event.Sequence
	record.Time = event.Time
	record.Line = line
	return record, nil
}

// List returns retained records with sequence > since, optionally filtered by
// resource and container, capped at limit.
func (s *LogSpool) List(since uint64, limit int, resource, container string) ([]LogRecord, error) {
	events, err := s.journal.Read()
	if err != nil {
		return nil, err
	}
	out := make([]LogRecord, 0, len(events))
	for _, event := range events {
		if event.Sequence <= since || event.Kind != "log" {
			continue
		}
		if resource != "" && event.Resource != resource {
			continue
		}
		if container != "" && event.Fields["container"] != container {
			continue
		}
		out = append(out, LogRecord{
			Sequence:  event.Sequence,
			Time:      event.Time,
			Resource:  event.Resource,
			Container: event.Fields["container"],
			Stream:    event.Fields["stream"],
			Line:      event.Message,
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Follow streams records with sequence > since until ctx is canceled.
func (s *LogSpool) Follow(ctx context.Context, since uint64, resource, container string, interval time.Duration) <-chan LogRecord {
	if interval <= 0 {
		interval = DefaultFollowInterval
	}
	ch := make(chan LogRecord, 128)
	go func() {
		defer close(ch)
		last := since
		for {
			records, err := s.List(last, 0, resource, container)
			if err == nil {
				for _, record := range records {
					select {
					case ch <- record:
						last = record.Sequence
					case <-ctx.Done():
						return
					}
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
		}
	}()
	return ch
}

// Close flushes the spool.
func (s *LogSpool) Close() error { return s.journal.Close() }
