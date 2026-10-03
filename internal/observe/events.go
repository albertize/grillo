//go:build linux

// SPDX-License-Identifier: Apache-2.0

// Package observe provides the structured event stream, bounded log spool and
// follow, process resource sampling, and health-probe scheduling. Events and
// logs never carry secret values; callers must redact before emitting.
package observe

import (
	"context"
	"strings"
	"time"

	"grillo.local/grillo/internal/state"
)

// Event is a structured, sequenced record. It aliases the state journal event
// so the append-only rotation, sequence IDs, and truncation recovery are reused.
type Event = state.Event

// DefaultFollowInterval is the poll interval for Follow.
const DefaultFollowInterval = 200 * time.Millisecond

// EventStore is an append-only, rotated event stream.
type EventStore struct {
	journal *state.Journal
}

// OpenEvents opens (or creates) the event stream in dir.
func OpenEvents(dir string, maxBytes int64, maxRotated int) (*EventStore, error) {
	journal, err := state.OpenJournal(dir, state.JournalOptions{MaxBytes: maxBytes, MaxRotated: maxRotated})
	if err != nil {
		return nil, err
	}
	return &EventStore{journal: journal}, nil
}

// Emit appends an event and returns it with its assigned sequence and time.
func (s *EventStore) Emit(event Event) (Event, error) { return s.journal.Append(event) }

// Close flushes the store.
func (s *EventStore) Close() error { return s.journal.Close() }

// List returns retained events with sequence > since, optionally filtered by a
// resource prefix, capped at limit (0 means no limit).
func (s *EventStore) List(since uint64, limit int, resource string) ([]Event, error) {
	all, err := s.journal.Read()
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(all))
	for _, event := range all {
		if event.Sequence <= since {
			continue
		}
		if resource != "" && !strings.HasPrefix(event.Resource, resource) {
			continue
		}
		out = append(out, event)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Follow streams events with sequence > since until ctx is canceled. It polls
// the journal at interval; a gap larger than one is reported with a synthetic
// event with Kind "events.gap" so followers can resync.
func (s *EventStore) Follow(ctx context.Context, since uint64, interval time.Duration) <-chan Event {
	if interval <= 0 {
		interval = DefaultFollowInterval
	}
	ch := make(chan Event, 64)
	go func() {
		defer close(ch)
		last := since
		for {
			events, err := s.List(last, 0, "")
			if err == nil && len(events) > 0 {
				if events[0].Sequence > last+1 {
					gap := Event{
						Sequence: events[0].Sequence - 1,
						Time:     time.Now().UTC(),
						Kind:     "events.gap",
						Message:  "retention gap; resync required",
					}
					select {
					case ch <- gap:
					case <-ctx.Done():
						return
					}
				}
				for _, event := range events {
					select {
					case ch <- event:
						last = event.Sequence
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
