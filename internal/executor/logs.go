//go:build linux

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"context"
	"fmt"
	"sync"

	"github.com/albertize/grillo/internal/guestproto"
	"github.com/albertize/grillo/internal/observe"
)

type guestLogState struct {
	mu     sync.Mutex
	cursor uint64
}

// collectLogs transfers one bounded batch. The host cursor advances only after
// successful spool writes; guest retention is independent of slow API readers.
func (e *Executor) collectLogs(ctx context.Context, rt *sandboxRuntime) error {
	if e.cfg.Logs == nil {
		return nil
	}
	rt.logState.mu.Lock()
	defer rt.logState.mu.Unlock()
	result, err := rt.guest.Logs(ctx, rt.logState.cursor)
	if err != nil {
		return fmt.Errorf("executor: guest logs unavailable: %w", err)
	}
	for _, chunk := range result.Chunks {
		if !rt.containers[chunk.Container] {
			return fmt.Errorf("executor: guest log names an unknown container")
		}
	}
	if result.Gap {
		if _, err := e.cfg.Logs.Append(observe.LogRecord{Resource: rt.id, Stream: "stderr", Gap: true, Line: "[grillo: guest log retention gap; output was lost]"}); err != nil {
			return fmt.Errorf("executor: log spool write failed")
		}
		rt.logState.cursor = result.Chunks[0].Sequence - 1
	}
	for _, chunk := range result.Chunks {
		if _, err := e.cfg.Logs.Append(observe.LogRecord{Resource: rt.id, Container: chunk.Container, Stream: chunk.Stream, Line: string(chunk.Data)}); err != nil {
			return fmt.Errorf("executor: log spool write failed")
		}
		rt.logState.cursor = chunk.Sequence
	}
	return nil
}

// drainLogs is best-effort during shutdown/start failure. It does not turn
// log loss into a teardown failure and never loops over an unbounded producer.
func (e *Executor) drainLogs(ctx context.Context, rt *sandboxRuntime) {
	if e.cfg.Logs == nil {
		return
	}
	for i := 0; i < 32 && ctx.Err() == nil; i++ {
		rt.logState.mu.Lock()
		before := rt.logState.cursor
		rt.logState.mu.Unlock()
		if err := e.collectLogs(ctx, rt); err != nil {
			return
		}
		rt.logState.mu.Lock()
		after := rt.logState.cursor
		rt.logState.mu.Unlock()
		if before == after {
			return
		}
	}
}

// A missing capability fails explicitly rather than presenting an empty spool
// as evidence that a container produced no output.
func supportsLogs(client *guestproto.Client) bool { return client.Info().Supports(guestproto.CapLogs) }
