//go:build !linux

// SPDX-License-Identifier: Apache-2.0

package build

import (
	"context"
	"fmt"
)

// PodmanBuilder is unavailable outside Linux.
type PodmanBuilder struct{}

// Available always fails on unsupported platforms.
func (b *PodmanBuilder) Available(context.Context) error {
	return fmt.Errorf("%w: rootless Podman builds are only supported on Linux", ErrToolMissing)
}

// Build always fails on unsupported platforms.
func (b *PodmanBuilder) Build(context.Context, Request, Progress) (Result, error) {
	return Result{}, fmt.Errorf("%w: rootless Podman builds are only supported on Linux", ErrToolMissing)
}
