// SPDX-License-Identifier: Apache-2.0

// Package reconcile applies desired application IR against an executor. It owns
// planning, operation IDs, retry classification, partial-progress persistence,
// and idempotent replay. The executor performs effects; this package is pure
// orchestration and is unit-testable with a fake executor.
package reconcile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/observe"
	"github.com/albertize/grillo/internal/plan"
)

// Executor performs one plan action idempotently for an operation key.
type Executor interface {
	Apply(ctx context.Context, application string, action plan.Action, operation string) error
}

// Store persists the last observed runtime state per application.
type Store interface {
	Load(application string) (plan.Observed, error)
	Save(application string, observed plan.Observed) error
}

// StoreFunc adapts functions to Store.
type StoreFunc struct {
	LoadFunc func(application string) (plan.Observed, error)
	SaveFunc func(application string, observed plan.Observed) error
}

// Load implements Store.
func (f StoreFunc) Load(application string) (plan.Observed, error) { return f.LoadFunc(application) }

// Save implements Store.
func (f StoreFunc) Save(application string, observed plan.Observed) error {
	return f.SaveFunc(application, observed)
}

// MemoryStore is an in-memory Store, useful for tests and the daemon's cache.
type MemoryStore struct {
	mu    sync.Mutex
	state map[string]plan.Observed
}

// NewMemoryStore returns an empty store.
func NewMemoryStore() *MemoryStore { return &MemoryStore{state: map[string]plan.Observed{}} }

// Load implements Store.
func (s *MemoryStore) Load(application string) (plan.Observed, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	observed, ok := s.state[application]
	if !ok {
		return plan.Observed{Sandboxes: map[string]plan.ObservedSandbox{}, Volumes: map[string]bool{}}, nil
	}
	return cloneObserved(observed), nil
}

// Save implements Store.
func (s *MemoryStore) Save(application string, observed plan.Observed) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state[application] = cloneObserved(observed)
	return nil
}

func cloneObserved(in plan.Observed) plan.Observed {
	out := in
	out.Sandboxes = make(map[string]plan.ObservedSandbox, len(in.Sandboxes))
	for k, v := range in.Sandboxes {
		out.Sandboxes[k] = v
	}
	out.Volumes = make(map[string]bool, len(in.Volumes))
	for k, v := range in.Volumes {
		out.Volumes[k] = v
	}
	return out
}

// PermanentError marks an unrecoverable failure; it is never retried.
type PermanentError struct{ Err error }

func (e PermanentError) Error() string { return e.Err.Error() }
func (e PermanentError) Unwrap() error { return e.Err }

// Permanent wraps an error as permanent.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return PermanentError{Err: err}
}

// Reconciler applies desired state.
type Reconciler struct {
	Store    Store
	Executor Executor
	// Emit receives progress events; may be nil.
	Emit func(observe.Event)

	MaxAttempts int
	BaseBackoff time.Duration
	Sleep       func(time.Duration)

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (r *Reconciler) ensureLocks() {
	r.mu.Lock()
	if r.locks == nil {
		r.locks = map[string]*sync.Mutex{}
	}
	r.mu.Unlock()
}

func (r *Reconciler) maxAttempts() int {
	if r.MaxAttempts > 0 {
		return r.MaxAttempts
	}
	return 3
}

func (r *Reconciler) sleepFn() func(time.Duration) {
	if r.Sleep != nil {
		return r.Sleep
	}
	return time.Sleep
}

func (r *Reconciler) lockFor(application string) *sync.Mutex {
	r.ensureLocks()
	r.mu.Lock()
	defer r.mu.Unlock()
	lock, ok := r.locks[application]
	if !ok {
		lock = &sync.Mutex{}
		r.locks[application] = lock
	}
	return lock
}

// Result summarizes one apply.
type Result struct {
	Application string
	Applied     int
	Converged   bool
	Failed      *plan.Action
}

// Apply reconciles one application. Repeating it is idempotent: completed work is
// recorded in the observed state, so a replay only performs the remainder.
func (r *Reconciler) Apply(ctx context.Context, desired model.Application) (Result, error) {
	r.ensureLocks()
	application := desired.Identity.Name
	lock := r.lockFor(application)
	lock.Lock()
	defer lock.Unlock()

	observed, err := r.Store.Load(application)
	if err != nil {
		return Result{}, err
	}
	observed.Stopped = false
	if observed.Sandboxes == nil {
		observed.Sandboxes = map[string]plan.ObservedSandbox{}
	}
	if observed.Volumes == nil {
		observed.Volumes = map[string]bool{}
	}
	revision, err := plan.Revision(desired)
	if err != nil {
		return Result{}, err
	}
	routesHash, err := plan.RoutesHash(desired)
	if err != nil {
		return Result{}, err
	}
	actions, err := plan.Build(desired, observed)
	if err != nil {
		return Result{}, err
	}
	if len(actions.Actions) == 0 {
		return Result{Application: application, Converged: true}, nil
	}
	result := Result{Application: application}
	for i := range actions.Actions {
		action := actions.Actions[i]
		operation := operationID(application, revision, action)
		if err := r.executeWithRetry(ctx, application, action, operation); err != nil {
			result.Failed = &action
			_ = r.Store.Save(application, observed)
			return result, err
		}
		observed = advance(observed, action)
		result.Applied++
		if err := r.Store.Save(application, observed); err != nil {
			return result, err
		}
		r.emit(application, action)
	}
	observed.Revision = revision
	observed.RoutesHash = routesHash
	if err := r.Store.Save(application, observed); err != nil {
		return result, err
	}
	return result, nil
}

func (r *Reconciler) executeWithRetry(ctx context.Context, application string, action plan.Action, operation string) error {
	maxAttempts := r.maxAttempts()
	sleep := r.sleepFn()
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := r.Executor.Apply(ctx, application, action, operation)
		if err == nil {
			return nil
		}
		lastErr = err
		var permanent PermanentError
		if errors.As(err, &permanent) {
			return err
		}
		if attempt < maxAttempts-1 && r.BaseBackoff > 0 {
			sleep(r.BaseBackoff << attempt)
		}
	}
	return fmt.Errorf("reconcile: %s after %d attempts: %w", action.Kind, maxAttempts, lastErr)
}

// Down stops an application and releases its runtime resources. Repeated down is
// safe. When removeVolumes is true, owned managed volumes are deleted too; bind
// and external volumes are never touched (the executor enforces this).
func (r *Reconciler) Down(ctx context.Context, application string, removeVolumes bool) (Result, error) {
	r.ensureLocks()
	lock := r.lockFor(application)
	lock.Lock()
	defer lock.Unlock()

	observed, err := r.Store.Load(application)
	if err != nil {
		return Result{}, err
	}
	observed.Stopped = true
	if err := r.Store.Save(application, observed); err != nil {
		return Result{}, err
	}
	// Build a stop plan against the observed state.
	actions, err := plan.Build(model.Application{Identity: model.Identity{Name: application}}, observed)
	if err != nil {
		return Result{}, err
	}
	result := Result{Application: application}
	for _, action := range actions.Actions {
		if err := r.executeWithRetry(ctx, application, action, operationID(application, observed.Revision, action)); err != nil {
			result.Failed = &action
			return result, err
		}
		observed = advance(observed, action)
		result.Applied++
		_ = r.Store.Save(application, observed)
	}
	if removeVolumes {
		for name := range observed.Volumes {
			action := plan.Action{Kind: plan.ActionDeleteVolume, Resource: "volume/" + name, Reason: "down --volumes", Volume: name, Impact: plan.ImpactPersistent}
			if err := r.executeWithRetry(ctx, application, action, operationID(application, observed.Revision, action)); err != nil {
				result.Failed = &action
				return result, err
			}
			delete(observed.Volumes, name)
			result.Applied++
		}
	}
	observed.Revision = ""
	observed.RoutesHash = ""
	if err := r.Store.Save(application, observed); err != nil {
		return result, err
	}
	return result, nil
}

// ApplyAll reconciles several applications with a bounded worker pool.
func (r *Reconciler) ApplyAll(ctx context.Context, desired []model.Application, workers int) []error {
	if workers <= 0 {
		workers = 1
	}
	sem := make(chan struct{}, workers)
	errs := make([]error, len(desired))
	var wg sync.WaitGroup
	for i := range desired {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			_, errs[i] = r.Apply(ctx, desired[i])
		}(i)
	}
	wg.Wait()
	return errs
}

func advance(observed plan.Observed, action plan.Action) plan.Observed {
	switch action.Kind {
	case plan.ActionPrepareVolume:
		observed.Volumes[action.Volume] = true
	case plan.ActionCreateSandbox:
		observed.Sandboxes[action.Sandbox.ID] = plan.ObservedSandbox{
			Workload:     action.Sandbox.Workload,
			Index:        action.Sandbox.Index,
			TemplateHash: action.Sandbox.TemplateHash,
			State:        "running",
			Ready:        true,
		}
	case plan.ActionStopSandbox:
		if existing, ok := observed.Sandboxes[action.Sandbox.ID]; ok {
			existing.State = "stopped"
			existing.Ready = false
			observed.Sandboxes[action.Sandbox.ID] = existing
		}
	case plan.ActionDeleteSandbox:
		delete(observed.Sandboxes, action.Sandbox.ID)
	case plan.ActionDeleteVolume:
		delete(observed.Volumes, action.Volume)
	}
	return observed
}

func (r *Reconciler) emit(application string, action plan.Action) {
	if r.Emit == nil {
		return
	}
	r.Emit(observe.Event{
		Kind:     "reconcile." + string(action.Kind),
		Resource: "application/" + application,
		Message:  action.Reason,
		Reason:   string(action.Kind),
		Fields:   map[string]string{"target": action.Resource},
	})
}

// operationID is a deterministic operation key so an executor can deduplicate a
// replayed action after a crash.
func operationID(application, revision string, action plan.Action) string {
	sum := sha256.Sum256([]byte(application + "|" + revision + "|" + string(action.Kind) + "|" + action.Resource))
	return "op-" + hex.EncodeToString(sum[:8])
}
