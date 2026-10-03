//go:build linux

// SPDX-License-Identifier: Apache-2.0

package observe

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEventStoreEmitListFollow(t *testing.T) {
	store, err := OpenEvents(t.TempDir(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i := 0; i < 3; i++ {
		if _, err := store.Emit(Event{Kind: "pod.started", Resource: "pod/app-0", Message: "started"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Emit(Event{Kind: "service.updated", Resource: "service/api"}); err != nil {
		t.Fatal(err)
	}
	all, err := store.List(0, 0, "")
	if err != nil || len(all) != 4 {
		t.Fatalf("list = %d err=%v", len(all), err)
	}
	if all[0].Sequence != 1 || all[3].Sequence != 4 {
		t.Fatalf("sequences = %d..%d", all[0].Sequence, all[3].Sequence)
	}
	filtered, err := store.List(0, 0, "pod/")
	if err != nil || len(filtered) != 3 {
		t.Fatalf("filtered = %d err=%v", len(filtered), err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := store.Follow(ctx, 4, 20*time.Millisecond)
	if _, err := store.Emit(Event{Kind: "pod.stopped", Resource: "pod/app-0"}); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-stream:
		if event.Sequence != 5 || event.Kind != "pod.stopped" {
			t.Fatalf("followed event = %+v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("follow did not deliver the event")
	}
}

func TestEventStoreFollowGap(t *testing.T) {
	dir := t.TempDir()
	// A tiny journal so rotation drops old events and creates a gap.
	store, err := OpenEvents(dir, 200, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i := 0; i < 20; i++ {
		if _, err := store.Emit(Event{Kind: "x", Resource: "pod/app", Message: strings.Repeat("y", 40)}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := store.Follow(ctx, 0, 20*time.Millisecond)
	select {
	case event := <-stream:
		if event.Kind != "events.gap" {
			t.Fatalf("first followed event = %+v, want gap", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("follow did not report a gap")
	}
}

func TestLogSpoolAppendListAndTruncate(t *testing.T) {
	spool, err := OpenLogSpool(t.TempDir(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	if _, err := spool.Append(LogRecord{Resource: "pod/app-0", Container: "api", Stream: "stdout", Line: "hello\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := spool.Append(LogRecord{Resource: "pod/app-0", Container: "sidecar", Stream: "stderr", Line: "warn\n"}); err != nil {
		t.Fatal(err)
	}
	records, err := spool.List(0, 0, "pod/app-0", "api")
	if err != nil || len(records) != 1 || records[0].Line != "hello\n" || records[0].Stream != "stdout" {
		t.Fatalf("records = %+v err=%v", records, err)
	}
	long := strings.Repeat("z", MaxLogLineBytes+100)
	stored, err := spool.Append(LogRecord{Resource: "pod/app-0", Container: "api", Line: long})
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Line) != MaxLogLineBytes {
		t.Fatalf("long line length = %d, want %d", len(stored.Line), MaxLogLineBytes)
	}
}

func TestLogSpoolFollow(t *testing.T) {
	spool, _ := OpenLogSpool(t.TempDir(), 0, 0)
	defer spool.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := spool.Follow(ctx, 0, "pod/app-0", "", 20*time.Millisecond)
	if _, err := spool.Append(LogRecord{Resource: "pod/app-0", Container: "api", Line: "line\n"}); err != nil {
		t.Fatal(err)
	}
	select {
	case record := <-stream:
		if record.Line != "line\n" {
			t.Fatalf("followed record = %+v", record)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("follow did not deliver the log line")
	}
}

func TestSampleProcess(t *testing.T) {
	snapshot, err := SampleProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.RSSBytes <= 0 || snapshot.Threads <= 0 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestSamplerComputesCPUPercent(t *testing.T) {
	clock := &manualClock{now: time.Unix(0, 0)}
	sampler := NewSampler(clock)
	first := sampler.Sample([]int{os.Getpid()})
	if len(first) != 1 {
		t.Fatalf("first sample = %+v", first)
	}
	clock.advance(time.Second)
	second := sampler.Sample([]int{os.Getpid()})
	if len(second) != 1 || second[0].CPUPercent < 0 {
		t.Fatalf("second sample = %+v", second)
	}
	// A dead pid is skipped, not an error.
	if samples := sampler.Sample([]int{1 << 30}); len(samples) != 0 {
		t.Fatalf("dead pid produced samples: %+v", samples)
	}
}

func TestExitWatcherEmitsOnTransitionOnly(t *testing.T) {
	var mu sync.Mutex
	states := []ContainerState{{Name: "api", State: "running"}}
	var events []Event
	watcher := &ExitWatcher{
		Interval: 10 * time.Millisecond,
		Status: func(context.Context) ([]ContainerState, error) {
			mu.Lock()
			defer mu.Unlock()
			return append([]ContainerState(nil), states...), nil
		},
		Emit: func(e Event) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, e)
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { watcher.Run(ctx); close(done) }()

	time.Sleep(40 * time.Millisecond) // running: no exit event
	mu.Lock()
	states = []ContainerState{{Name: "api", State: "exited", ExitCode: 7}}
	mu.Unlock()
	time.Sleep(60 * time.Millisecond) // transition: one event
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 {
		t.Fatalf("expected exactly one exit event, got %+v", events)
	}
	if events[0].Kind != "container.exited" || events[0].Fields["exitCode"] != "7" {
		t.Fatalf("event = %+v", events[0])
	}
}

func TestEventStoreSlowConsumerDoesNotBlockProducer(t *testing.T) {
	store, err := OpenEvents(t.TempDir(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Start a follower but never read from it, so its buffer fills.
	_ = store.Follow(ctx, 0, 10*time.Millisecond)
	start := time.Now()
	for i := 0; i < 2000; i++ {
		if _, err := store.Emit(Event{Kind: "pod.sample", Resource: "pod/app"}); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("emitting blocked on a slow consumer: %v", elapsed)
	}
}

func TestEventCarriesSourceMapping(t *testing.T) {
	store, _ := OpenEvents(t.TempDir(), 0, 0)
	defer store.Close()
	if _, err := store.Emit(Event{Kind: "pod.created", Resource: "pod/app", Source: "compose.yaml:12"}); err != nil {
		t.Fatal(err)
	}
	events, err := store.List(0, 0, "")
	if err != nil || len(events) != 1 || events[0].Source != "compose.yaml:12" {
		t.Fatalf("events = %+v err=%v", events, err)
	}
}
