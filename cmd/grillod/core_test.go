//go:build linux

// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/executor"
	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/plan"
	"github.com/albertize/grillo/internal/reconcile"
	"github.com/albertize/grillo/internal/sandbox"
)

type noopBackend struct{ sandbox.Backend }
type recordingImages struct {
	mu   sync.Mutex
	refs []string
}

func (r *recordingImages) Resolve(_ context.Context, i model.ImageRef) (executor.Image, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refs = append(r.refs, i.Reference)
	return executor.Image{}, errors.New("test stops before VM effects")
}

type actionFunc func(context.Context, string, plan.Action, string) error

func (f actionFunc) Apply(ctx context.Context, app string, a plan.Action, op string) error {
	return f(ctx, app, a, op)
}
func reviewApp(ref string) model.Application {
	return model.Application{Identity: model.Identity{Name: "app"}, Workloads: []model.Workload{{ID: "web", Kind: model.WorkloadDeployment, Replicas: 1, Template: model.SandboxTemplate{Containers: []model.Container{{Name: "web", Image: model.ImageRef{Reference: ref}, Command: &[]string{"x"}}}}}}}
}

func TestApplySerializesDesiredPublicationAndEffects(t *testing.T) {
	images := &recordingImages{}
	e, err := executor.New(executor.Config{Backend: noopBackend{}, Images: images})
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	c := &core{exec: e, apps: map[string]bool{}, desired: map[string]model.Application{}}
	c.reconciler = &reconcile.Reconciler{Store: reconcile.NewMemoryStore(), MaxAttempts: 1, Executor: actionFunc(func(ctx context.Context, app string, a plan.Action, _ string) error {
		if a.Kind != plan.ActionCreateSandbox {
			return nil
		}
		once.Do(func() { close(entered); <-release })
		return e.Ensure(ctx, app, a.Sandbox)
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first := make(chan error, 1)
	go func() { _, err := c.Apply(ctx, reviewApp("first")); first <- err }()
	<-entered
	if c.mutationMu.TryLock() {
		c.mutationMu.Unlock()
		close(release)
		t.Fatal("effects do not hold mutation lock")
	}
	second := make(chan error, 1)
	go func() { _, err := c.Apply(ctx, reviewApp("second")); second <- err }()
	close(release)
	if <-first == nil || <-second == nil {
		t.Fatal("injected effect error lost")
	}
	images.mu.Lock()
	defer images.mu.Unlock()
	if len(images.refs) != 2 || images.refs[0] != "first" || images.refs[1] != "second" {
		t.Fatalf("mixed revisions: %v", images.refs)
	}
}

func TestNativeAPIRejectsUnsupportedSecurityBeforeEffects(t *testing.T) {
	e, err := executor.New(executor.Config{Backend: noopBackend{}, Images: &recordingImages{}})
	if err != nil {
		t.Fatal(err)
	}
	c := &core{exec: e}
	app := reviewApp("image")
	app.Workloads[0].Template.SecurityProfile.Privileged = true
	if _, err := c.Apply(context.Background(), app); err == nil {
		t.Fatal("privileged API input accepted")
	}
}

func TestRecoveryDoesNotRestartStoppedApplication(t *testing.T) {
	p, err := reconcile.OpenPersistentStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := reviewApp("image")
	if err := p.SetDesired(app); err != nil {
		t.Fatal(err)
	}
	if err := p.SetStopped("app", false); err != nil {
		t.Fatal(err)
	}
	e, err := executor.New(executor.Config{Backend: noopBackend{}, Images: &recordingImages{}})
	if err != nil {
		t.Fatal(err)
	}
	c := &core{persistent: p, exec: e, apps: map[string]bool{}, desired: map[string]model.Application{}, reconciler: &reconcile.Reconciler{Store: p, Executor: actionFunc(func(context.Context, string, plan.Action, string) error {
		t.Fatal("stopped application executed effects")
		return nil
	})}}
	if err := c.recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	names, _ := c.Applications(context.Background())
	if len(names) != 1 || names[0] != "app" {
		t.Fatalf("%v", names)
	}
}
