//go:build linux

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"context"
	"fmt"
	"time"

	"github.com/albertize/grillo/internal/model"
)

// Startup gates use actual successfully started runtimes and observed probe
// readiness. They do not continuously restart consumers when dependencies change.
func (e *Executor) waitStartupDependencies(ctx context.Context, application string, app model.Application, workload model.Workload) error {
	if len(workload.DependsOn) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("executor: startup dependency gate canceled: %w", err)
		}
		satisfied, err := e.startupDependenciesSatisfied(application, app, workload)
		if err != nil {
			return err
		}
		if satisfied {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("executor: startup dependency gate did not complete: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
func (e *Executor) startupDependenciesSatisfied(application string, app model.Application, workload model.Workload) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, dep := range workload.DependsOn {
		target := findWorkload(app, dep)
		if target == nil || target.Replicas < 1 {
			return false, fmt.Errorf("executor: missing startup dependency")
		}
		condition := workload.DependencyConditions[dep]
		if condition != "" && condition != model.DependencyStarted && condition != model.DependencyHealthy {
			return false, fmt.Errorf("executor: unsupported startup dependency condition")
		}
		count := 0
		for _, rt := range e.runtimes {
			if rt.app != application || rt.workload != dep {
				continue
			}
			if rt.guest == nil || rt.draining {
				return false, nil
			}
			if condition == model.DependencyHealthy && !rt.ready {
				return false, nil
			}
			count++
		}
		if count != int(target.Replicas) {
			return false, nil
		}
	}
	return true, nil
}
