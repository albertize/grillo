// SPDX-License-Identifier: Apache-2.0

package reconcile

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/plan"
)

type fakeSandboxes struct {
	mu     sync.Mutex
	events []string
}

func (f *fakeSandboxes) record(kind, id string) {
	f.mu.Lock()
	f.events = append(f.events, fmt.Sprintf("%s:%s", kind, id))
	f.mu.Unlock()
}
func (f *fakeSandboxes) Ensure(_ context.Context, _ string, d plan.Descriptor) error {
	f.record("ensure", d.ID)
	return nil
}
func (f *fakeSandboxes) Drain(_ context.Context, _ string, d plan.Descriptor) error {
	f.record("drain", d.ID)
	return nil
}
func (f *fakeSandboxes) Stop(_ context.Context, _ string, d plan.Descriptor) error {
	f.record("stop", d.ID)
	return nil
}
func (f *fakeSandboxes) Delete(_ context.Context, _ string, d plan.Descriptor) error {
	f.record("delete", d.ID)
	return nil
}

type fakeVolumes struct {
	mu     sync.Mutex
	events []string
}

func (f *fakeVolumes) Prepare(_ context.Context, _, volume string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, "prepare:"+volume)
	return nil
}
func (f *fakeVolumes) Delete(_ context.Context, _, volume string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, "delete:"+volume)
	return nil
}

func TestNativeExecutorDispatch(t *testing.T) {
	sandboxes := &fakeSandboxes{}
	volumes := &fakeVolumes{}
	exec := &NativeExecutor{Sandboxes: sandboxes, Volumes: volumes}
	reconciler := &Reconciler{Store: NewMemoryStore(), Executor: exec, MaxAttempts: 1}

	app := testApp("native", 2, "1")
	app.Volumes = []model.Volume{{Name: "data", Kind: model.VolumeManaged}}
	if _, err := reconciler.Apply(context.Background(), app); err != nil {
		t.Fatal(err)
	}

	sandboxes.mu.Lock()
	sandboxEvents := append([]string(nil), sandboxes.events...)
	sandboxes.mu.Unlock()
	if len(sandboxEvents) != 2 {
		t.Fatalf("sandbox events = %v", sandboxEvents)
	}
	volumes.mu.Lock()
	volumeEvents := append([]string(nil), volumes.events...)
	volumes.mu.Unlock()
	if len(volumeEvents) != 1 || volumeEvents[0] != "prepare:data" {
		t.Fatalf("volume events = %v", volumeEvents)
	}

	// Down with volumes deletes the owned managed volume.
	if _, err := reconciler.Down(context.Background(), "native", true); err != nil {
		t.Fatal(err)
	}
	volumes.mu.Lock()
	volumeEvents = append([]string(nil), volumes.events...)
	volumes.mu.Unlock()
	if volumeEvents[len(volumeEvents)-1] != "delete:data" {
		t.Fatalf("down did not delete the volume: %v", volumeEvents)
	}
}
