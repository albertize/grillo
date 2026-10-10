//go:build linux

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/guestproto"
	"github.com/albertize/grillo/internal/model"
)

func TestStartupDependencyGates(t *testing.T) {
	e := &Executor{runtimes: map[string]*sandboxRuntime{}}
	app := model.Application{Workloads: []model.Workload{{ID: "db", Replicas: 2}}}
	consumer := model.Workload{ID: "app", DependsOn: []string{"db"}, DependencyConditions: map[string]model.DependencyCondition{"db": model.DependencyHealthy}}
	check := func(want bool) {
		t.Helper()
		got, err := e.startupDependenciesSatisfied("demo", app, consumer)
		if err != nil || got != want {
			t.Fatalf("gate=%v err=%v want=%v", got, err, want)
		}
	}
	check(false)
	for _, id := range []string{"db-0", "db-1"} {
		e.runtimes[id] = &sandboxRuntime{id: id, app: "demo", workload: "db", guest: &guestproto.Client{}}
	}
	check(false)
	e.runtimes["db-0"].ready = true
	check(false)
	e.runtimes["db-1"].ready = true
	check(true)
	e.runtimes["db-1"].draining = true
	check(false)
	e.runtimes["db-1"].draining = false
	e.runtimes["db-1"].app = "other"
	check(false)
	e.runtimes["db-1"].app = "demo"
	consumer.DependencyConditions = nil
	e.runtimes["db-0"].ready = false
	check(true) // started is not healthy
	consumer.DependencyConditions = map[string]model.DependencyCondition{"db": model.DependencyHealthy}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := e.waitStartupDependencies(ctx, "demo", app, consumer); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("gate ignored deadline: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if err := e.waitStartupDependencies(ctx, "demo", app, consumer); !errors.Is(err, context.Canceled) {
		t.Fatalf("gate ignored cancellation: %v", err)
	}
}
