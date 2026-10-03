// SPDX-License-Identifier: Apache-2.0

package reconcile

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"grillo.local/grillo/internal/model"
	"grillo.local/grillo/internal/plan"
)

type fakeExecutor struct {
	mu            sync.Mutex
	calls         int
	applied       []plan.ActionKind
	operations    []string
	failCall      int
	permanentCall int
	healed        bool
	active        int
	maxActive     int
	delay         time.Duration
}

func (f *fakeExecutor) Apply(ctx context.Context, _ string, action plan.Action, operation string) error {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.active++
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	f.mu.Unlock()

	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
		}
	}

	f.mu.Lock()
	f.active--
	fail := !f.healed && n == f.failCall
	permanent := !f.healed && n == f.permanentCall
	if !fail && !permanent {
		f.applied = append(f.applied, action.Kind)
		f.operations = append(f.operations, operation)
	}
	f.mu.Unlock()
	if permanent {
		return Permanent(errors.New("permanent"))
	}
	if fail {
		return errors.New("transient failure")
	}
	return nil
}

func (f *fakeExecutor) heal() {
	f.mu.Lock()
	f.healed = true
	f.mu.Unlock()
}

func (f *fakeExecutor) kindsSince(index int) []plan.ActionKind {
	f.mu.Lock()
	defer f.mu.Unlock()
	if index > len(f.applied) {
		index = len(f.applied)
	}
	return append([]plan.ActionKind(nil), f.applied[index:]...)
}

func (f *fakeExecutor) uniqueOperations() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	seen := map[string]bool{}
	for _, operation := range f.operations {
		seen[operation] = true
	}
	return len(seen)
}

func (f *fakeExecutor) appliedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.applied)
}

func testApp(name string, replicas int, tag string) model.Application {
	return model.Application{
		APIVersion: model.APIVersion,
		Kind:       model.KindApplication,
		Identity:   model.Identity{Name: name, Namespace: "default"},
		Workloads: []model.Workload{{
			ID:       "app",
			Kind:     model.WorkloadDeployment,
			Replicas: int32(replicas),
			Template: model.SandboxTemplate{
				Containers: []model.Container{{Name: "api", Image: model.ImageRef{Reference: "busybox:" + tag}}},
			},
		}},
		Services: []model.Service{{Name: "app", Selector: map[string]string{"app": "app"}, Ports: []model.ServicePort{{Port: 80}}}},
	}
}

func newReconciler(exec *fakeExecutor) *Reconciler {
	return &Reconciler{Store: NewMemoryStore(), Executor: exec, MaxAttempts: 3, Sleep: func(time.Duration) {}}
}

func TestApplyCreatesAndConverges(t *testing.T) {
	exec := &fakeExecutor{}
	rec := newReconciler(exec)
	desired := testApp("backend", 2, "1")
	result, err := rec.Apply(context.Background(), desired)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied != 3 { // 2 creates + endpoints
		t.Fatalf("applied = %d, want 3", result.Applied)
	}
	// Identical apply converges with no effects.
	before := exec.appliedCount()
	second, err := rec.Apply(context.Background(), desired)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Converged || second.Applied != 0 || exec.appliedCount() != before {
		t.Fatalf("second apply = %+v (effects %d -> %d)", second, before, exec.appliedCount())
	}
}

func TestRouteOnlyChangeNoReboot(t *testing.T) {
	exec := &fakeExecutor{}
	rec := newReconciler(exec)
	desired := testApp("backend", 2, "1")
	if _, err := rec.Apply(context.Background(), desired); err != nil {
		t.Fatal(err)
	}
	mark := exec.appliedCount()
	desired.Services[0].Ports[0].Port = 8080 // routes only
	if _, err := rec.Apply(context.Background(), desired); err != nil {
		t.Fatal(err)
	}
	for _, kind := range exec.kindsSince(mark) {
		if kind != plan.ActionUpdateEndpoints {
			t.Fatalf("route-only change applied %s", kind)
		}
	}
}

func TestTransientRetryThenSuccess(t *testing.T) {
	exec := &fakeExecutor{failCall: 1}
	rec := newReconciler(exec)
	if _, err := rec.Apply(context.Background(), testApp("backend", 1, "1")); err != nil {
		t.Fatal(err)
	}
	exec.mu.Lock()
	calls := exec.calls
	exec.mu.Unlock()
	if calls < 2 {
		t.Fatalf("transient failure was not retried (calls=%d)", calls)
	}
}

func TestPermanentErrorIsNotRetried(t *testing.T) {
	exec := &fakeExecutor{permanentCall: 1}
	rec := newReconciler(exec)
	if _, err := rec.Apply(context.Background(), testApp("backend", 1, "1")); err == nil {
		t.Fatal("expected permanent failure")
	}
	exec.mu.Lock()
	calls := exec.calls
	exec.mu.Unlock()
	if calls != 1 {
		t.Fatalf("permanent error retried (calls=%d)", calls)
	}
}

func TestCrashReplayOnlyAppliesRemainder(t *testing.T) {
	exec := &fakeExecutor{failCall: 3}
	rec := newReconciler(exec)
	rec.MaxAttempts = 1 // simulate a crash: no in-process retry
	desired := testApp("backend", 3, "1")
	if _, err := rec.Apply(context.Background(), desired); err == nil {
		t.Fatal("expected failure at action 3")
	}
	exec.heal()
	result, err := rec.Apply(context.Background(), desired)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied == 0 {
		t.Fatalf("replay applied nothing: %+v", result)
	}
	observed, _ := rec.Store.Load("backend")
	if len(observed.Sandboxes) != 3 {
		t.Fatalf("observed sandboxes = %d, want 3", len(observed.Sandboxes))
	}
	// Every action must have a unique operation ID; no work is duplicated.
	exec.mu.Lock()
	total := len(exec.operations)
	exec.mu.Unlock()
	if unique := exec.uniqueOperations(); unique != total {
		t.Fatalf("duplicate operations: %d unique of %d", unique, total)
	}
}

func TestScaleDownPreservesVolumes(t *testing.T) {
	exec := &fakeExecutor{}
	rec := newReconciler(exec)
	app := testApp("backend", 2, "1")
	app.Volumes = []model.Volume{{Name: "data", Kind: model.VolumeManaged}}
	if _, err := rec.Apply(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	scaled := testApp("backend", 1, "1")
	scaled.Volumes = app.Volumes
	result, err := rec.Apply(context.Background(), scaled)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied == 0 {
		t.Fatal("scale-down applied nothing")
	}
	observed, _ := rec.Store.Load("backend")
	if len(observed.Sandboxes) != 1 {
		t.Fatalf("observed sandboxes = %d, want 1", len(observed.Sandboxes))
	}
	if !observed.Volumes["data"] {
		t.Fatal("scale-down deleted the managed volume")
	}
}

func TestDownIsIdempotent(t *testing.T) {
	exec := &fakeExecutor{}
	rec := newReconciler(exec)
	app := testApp("backend", 2, "1")
	if _, err := rec.Apply(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Down(context.Background(), "backend", false); err != nil {
		t.Fatal(err)
	}
	before := exec.appliedCount()
	second, err := rec.Down(context.Background(), "backend", false)
	if err != nil {
		t.Fatal(err)
	}
	if second.Applied != 0 || exec.appliedCount() != before {
		t.Fatalf("repeated down applied %d actions", second.Applied)
	}
	observed, _ := rec.Store.Load("backend")
	if len(observed.Sandboxes) != 0 {
		t.Fatalf("down left sandboxes: %+v", observed.Sandboxes)
	}
}

func TestApplyAllBoundsWorkers(t *testing.T) {
	exec := &fakeExecutor{delay: 20 * time.Millisecond}
	rec := newReconciler(exec)
	apps := make([]model.Application, 0, 6)
	for i := 0; i < 6; i++ {
		apps = append(apps, testApp(fmt.Sprintf("app-%d", i), 1, "1"))
	}
	errs := rec.ApplyAll(context.Background(), apps, 2)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("app %d: %v", i, err)
		}
	}
	exec.mu.Lock()
	maxActive := exec.maxActive
	exec.mu.Unlock()
	if maxActive > 2 {
		t.Fatalf("worker bound exceeded: %d", maxActive)
	}
}
