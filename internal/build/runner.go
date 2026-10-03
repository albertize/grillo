// SPDX-License-Identifier: Apache-2.0

package build

import (
	"context"

	"grillo.local/grillo/internal/oci"
)

// RunStep is one RUN instruction executed against a shared build root.
type RunStep struct {
	// RootfsDir is the host build root shared into the sandbox.
	RootfsDir string
	// Command is the command to run. When Shell is true it is a single string
	// executed by the shell; otherwise it is an argv.
	Command []string
	Shell   bool
	Env     []string
	WorkDir string
	User    string
	// Network requests that the sandbox can reach the network (for package
	// installs). When false the sandbox should be offline.
	Network bool
}

// Runner executes RUN steps inside a sandbox that shares the build root. One
// Runner serves a whole build: implementations may boot the sandbox lazily and
// reuse it for later steps with the same root.
type Runner interface {
	Run(ctx context.Context, step RunStep, progress Progress) error
	Close() error
}

// ImageResolver resolves and unpacks base images. *oci.Puller implements it.
type ImageResolver interface {
	Pull(ctx context.Context, ref oci.Reference) (oci.PulledImage, error)
	Unpack(image oci.PulledImage, root string, opts oci.UnpackOptions) error
}
