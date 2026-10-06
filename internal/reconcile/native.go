// SPDX-License-Identifier: Apache-2.0

package reconcile

import (
	"context"
	"fmt"

	"github.com/albertize/grillo/internal/plan"
)

// SandboxController executes sandbox lifecycle actions for one application. The
// daemon wires it to the real sandbox backend and guest client; tests use a fake.
type SandboxController interface {
	Ensure(ctx context.Context, application string, descriptor plan.Descriptor) error
	Drain(ctx context.Context, application string, descriptor plan.Descriptor) error
	Stop(ctx context.Context, application string, descriptor plan.Descriptor) error
	Delete(ctx context.Context, application string, descriptor plan.Descriptor) error
}

// EndpointController owns application discovery/route lifecycle. Fake sandbox
// controllers may omit it; the production adapter must implement it.
type EndpointController interface {
	UpdateEndpoints(context.Context, string) error
	ShutdownNetwork(context.Context, string) error
}

// VolumeController prepares and deletes application volumes. Bind and external
// volumes must never be deleted by Delete.
type VolumeController interface {
	Prepare(ctx context.Context, application, volume string) error
	Delete(ctx context.Context, application, volume string) error
}

// NativeExecutor dispatches plan actions to the sandbox and volume controllers.
type NativeExecutor struct {
	Sandboxes SandboxController
	Volumes   VolumeController
}

// Apply implements Executor.
func (e *NativeExecutor) Apply(ctx context.Context, application string, action plan.Action, _ string) error {
	switch action.Kind {
	case plan.ActionPrepareVolume:
		if e.Volumes == nil {
			return nil
		}
		return e.Volumes.Prepare(ctx, application, action.Volume)
	case plan.ActionDeleteVolume:
		if e.Volumes == nil {
			return nil
		}
		return e.Volumes.Delete(ctx, application, action.Volume)
	case plan.ActionCreateSandbox:
		if e.Sandboxes == nil {
			return fmt.Errorf("reconcile: no sandbox controller configured")
		}
		return e.Sandboxes.Ensure(ctx, application, action.Sandbox)
	case plan.ActionDrain:
		if e.Sandboxes == nil {
			return nil
		}
		return e.Sandboxes.Drain(ctx, application, action.Sandbox)
	case plan.ActionStopSandbox:
		if e.Sandboxes == nil {
			return nil
		}
		return e.Sandboxes.Stop(ctx, application, action.Sandbox)
	case plan.ActionDeleteSandbox:
		if e.Sandboxes == nil {
			return nil
		}
		return e.Sandboxes.Delete(ctx, application, action.Sandbox)
	case plan.ActionUpdateEndpoints:
		if controller, ok := e.Sandboxes.(EndpointController); ok {
			return controller.UpdateEndpoints(ctx, application)
		}
		return nil
	case plan.ActionShutdownNetwork:
		if controller, ok := e.Sandboxes.(EndpointController); ok {
			return controller.ShutdownNetwork(ctx, application)
		}
		return nil
	case plan.ActionPullImage:
		// Ensure resolves and verifies container images before boot.
		return nil
	default:
		return fmt.Errorf("reconcile: unknown action %q", action.Kind)
	}
}
