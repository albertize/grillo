// SPDX-License-Identifier: Apache-2.0

package guestproto

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestQueuedCallKeepsDeadlineAndCloseInterruptsActiveCall(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	client := newPair(t, HandlerFunc(func(ctx context.Context, _ Message, _ *Stream) (any, *Error) {
		close(started)
		select {
		case <-release:
			return StatusResult{}, nil
		case <-ctx.Done():
			return nil, Errorf(CodeCanceled, "cancelled")
		}
	}))
	defer close(release)
	done := make(chan error, 1)
	go func() { _, err := client.Status(context.Background()); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first call did not start")
	}
	short, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	queued := make(chan error, 1)
	go func() { _, err := client.Status(short); queued <- err }()
	select {
	case err := <-queued:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("queued error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued call ignored deadline")
	}
	closed := make(chan struct{})
	go func() { _ = client.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close waited for active request")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("closed call reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("active request survived Close")
	}
}
