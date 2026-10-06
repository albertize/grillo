// SPDX-License-Identifier: Apache-2.0

package reconcile

import (
	"context"
	"github.com/albertize/grillo/internal/plan"
	"testing"
)

func TestFailedFirstCreateRetainsNetworkCleanupIntent(t *testing.T) {
	executor := &fakeExecutor{permanentCall: 1}
	reconciler := newReconciler(executor)
	if _, err := reconciler.Apply(context.Background(), testApp("failed", 1, "1")); err == nil {
		t.Fatal("expected create failure")
	}
	observed, err := reconciler.Store.Load("failed")
	if err != nil {
		t.Fatal(err)
	}
	if !observed.NetworkPending || len(observed.Sandboxes) != 0 {
		t.Fatalf("cleanup intent was lost: %+v", observed)
	}
	executor.heal()
	result, err := reconciler.Down(context.Background(), "failed", false)
	if err != nil {
		t.Fatal(err)
	}
	kinds := executor.kindsSince(0)
	if result.Applied != 1 || len(kinds) != 1 || kinds[0] != plan.ActionShutdownNetwork {
		t.Fatalf("missing cleanup action: %v", kinds)
	}
	result, err = reconciler.Down(context.Background(), "failed", false)
	if err != nil || result.Applied != 0 {
		t.Fatal("cleanup was not idempotent", result, err)
	}
}
