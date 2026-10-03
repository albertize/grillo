//go:build linux

// SPDX-License-Identifier: Apache-2.0

package observe

import (
	"context"
	"strconv"
	"time"
)

// ContainerState is one container's observed state.
type ContainerState struct {
	Name     string
	State    string
	ExitCode int
}

// StatusFunc reports current container states.
type StatusFunc func(ctx context.Context) ([]ContainerState, error)

// ExitWatcher polls container status at a bounded interval and emits an event
// only when a container's state changes, so exit observation does not flood the
// event stream or busy-poll the guest.
type ExitWatcher struct {
	Interval time.Duration
	Status   StatusFunc
	Emit     func(Event)
}

// Run watches until ctx is canceled.
func (w *ExitWatcher) Run(ctx context.Context) {
	interval := w.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	seen := map[string]string{}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if states, err := w.Status(ctx); err == nil {
			for _, state := range states {
				previous, known := seen[state.Name]
				if known && previous == state.State {
					continue
				}
				seen[state.Name] = state.State
				if state.State == "exited" || state.State == "stopped" {
					w.emit(state)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *ExitWatcher) emit(state ContainerState) {
	if w.Emit == nil {
		return
	}
	w.Emit(Event{
		Time:     time.Now().UTC(),
		Kind:     "container.exited",
		Resource: state.Name,
		Message:  "container " + state.State,
		Fields:   map[string]string{"exitCode": strconv.Itoa(state.ExitCode)},
	})
}
