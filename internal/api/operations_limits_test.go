//go:build linux

// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func awaitOperation(t *testing.T, ops *Operations, id string) Operation {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		op, ok := ops.Get(id)
		if ok && op.State != "running" {
			return op
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("operation did not finish")
	return Operation{}
}

func TestOperationsAdmissionConcurrentSaturation(t *testing.T) {
	ops := NewOperations()
	defer ops.CancelAll()
	var invoked atomic.Int32
	var wg sync.WaitGroup
	accepted := make(chan *Operation, 128)
	for range 128 {
		wg.Go(func() {
			op := ops.Start("load", "app", func(ctx context.Context) error {
				invoked.Add(1)
				<-ctx.Done()
				return ctx.Err()
			})
			if op != nil {
				accepted <- op
			}
		})
	}
	wg.Wait()
	close(accepted)
	if len(accepted) != 8 {
		t.Fatalf("accepted %d, want 8", len(accepted))
	}
	ops.mu.Lock()
	active, records := len(ops.cancels), len(ops.ops)
	ops.mu.Unlock()
	if active != 8 || records != 8 {
		t.Fatalf("active=%d records=%d", active, records)
	}
	for op := range accepted {
		if !ops.Cancel(op.ID) {
			t.Fatal("cancel refused")
		}
		if final := awaitOperation(t, ops, op.ID); final.State != "canceled" {
			t.Fatal(final.State)
		}
	}
	if invoked.Load() != 8 {
		t.Fatal("refused callbacks ran", invoked.Load())
	}
	retry := ops.Start("retry", "app", func(context.Context) error { return errors.New("injected failure") })
	if retry == nil || awaitOperation(t, ops, retry.ID).State != "failed" {
		t.Fatal("failure/retry contract")
	}
	ops.CancelAll()
	if ops.Start("late", "", func(context.Context) error { t.Error("shutdown callback ran"); return nil }) != nil {
		t.Fatal("admitted after shutdown")
	}
}

func TestOperationsBoundedHistoryAndDeadline(t *testing.T) {
	ops := NewOperations()
	defer ops.CancelAll()
	first := ops.Start("active", "", func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
	var expired string
	for i := range 300 {
		op := ops.Start("history", "", func(ctx context.Context) error {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > time.Hour {
				return errors.New("missing bounded deadline")
			}
			return nil
		})
		if op == nil {
			t.Fatal("unexpected admission refusal")
		}
		if i == 0 {
			expired = op.ID
		}
		if final := awaitOperation(t, ops, op.ID); final.State != "succeeded" {
			t.Fatal(final)
		}
	}
	if _, ok := ops.Get(expired); ok {
		t.Fatal("old finished history retained")
	}
	if op, ok := ops.Get(first.ID); !ok || op.State != "running" {
		t.Fatal("active operation evicted")
	}
	ops.mu.Lock()
	count := len(ops.ops)
	ops.mu.Unlock()
	if count != 256 {
		t.Fatal("history size", count)
	}
}

func TestAPIAdmissionRefusesWithoutMutation(t *testing.T) {
	core := &fakeCore{}
	server := NewServer(Options{Core: core})
	defer server.ops.CancelAll()
	for range 8 {
		if server.ops.Start("blocked", "", func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }) == nil {
			t.Fatal("initial admission")
		}
	}
	for _, path := range []string{"/v1/applications", "/v1/applications/app/down", "/v1/applications/app/restart"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"identity":{"name":"app"}}`))
		// Application identity is encoded as metadata by the model contract.
		if path == "/v1/applications" {
			req = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"metadata":{"name":"app"}}`))
		}
		out := httptest.NewRecorder()
		server.Handler().ServeHTTP(out, req)
		if out.Code != http.StatusTooManyRequests || strings.Contains(out.Body.String(), "operationId") {
			t.Fatalf("%s: %d %s", path, out.Code, out.Body.String())
		}
	}
	core.mu.Lock()
	defer core.mu.Unlock()
	if core.applied != 0 {
		t.Fatal("refused apply mutated core")
	}
}
