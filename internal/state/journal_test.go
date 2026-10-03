// SPDX-License-Identifier: Apache-2.0

package state

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestJournalAppendRead(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir, JournalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	e1, err := j.Append(Event{Kind: "a", Resource: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	e2, err := j.Append(Event{Kind: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if e1.Sequence != 1 || e2.Sequence != 2 {
		t.Fatalf("sequence = %d, %d; want 1, 2", e1.Sequence, e2.Sequence)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	events, err := j.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Kind != "a" || events[1].Kind != "b" {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestJournalTruncatedLastLineRecovered(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir, JournalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Append(Event{Kind: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Append(Event{Kind: "b"}); err != nil {
		t.Fatal(err)
	}
	j.Close()

	f, err := os.OpenFile(j.Path(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"seq":3,"kind":"partial`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	events, err := j.Read()
	if err != nil {
		t.Fatalf("truncated last line must be recoverable: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
}

func TestJournalMiddleCorruptionReported(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir, JournalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Append(Event{Kind: "a"}); err != nil {
		t.Fatal(err)
	}
	j.Close()

	f, err := os.OpenFile(j.Path(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("garbage\n{\"seq\":3,\"kind\":\"c\"}\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if _, err := j.Read(); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Read error = %v, want ErrCorrupt", err)
	}
}

func TestJournalRotationBoundedAndSequenceContinues(t *testing.T) {
	dir := t.TempDir()
	opts := JournalOptions{MaxBytes: 150, MaxRotated: 2}
	j, err := OpenJournal(dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	var lastSeq uint64
	for i := 0; i < 20; i++ {
		e, err := j.Append(Event{Kind: "k", Resource: fmt.Sprintf("r%d", i), Message: "0123456789"})
		if err != nil {
			t.Fatal(err)
		}
		if e.Sequence != uint64(i+1) {
			t.Fatalf("sequence = %d, want %d", e.Sequence, i+1)
		}
		lastSeq = e.Sequence
	}
	j.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "events") {
			files++
		}
	}
	if files > opts.MaxRotated+1 {
		t.Fatalf("journal files = %d, want <= %d", files, opts.MaxRotated+1)
	}

	reopened, err := OpenJournal(dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	e, err := reopened.Append(Event{Kind: "after"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Sequence != lastSeq+1 {
		t.Fatalf("sequence after reopen = %d, want %d", e.Sequence, lastSeq+1)
	}
	reopened.Close()

	events, err := reopened.Read()
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(events); i++ {
		if events[i].Sequence <= events[i-1].Sequence {
			t.Fatalf("sequences not monotonic: %+v", events)
		}
	}
}
