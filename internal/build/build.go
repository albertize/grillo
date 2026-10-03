// SPDX-License-Identifier: Apache-2.0

// Package build integrates Grillo with an external OCI image builder. Grillo
// never interprets a Dockerfile or .dockerignore itself; that work is delegated
// to the builder, which keeps the integration modular and avoids reimplementing
// BuildKit.
package build

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"grillo.local/grillo/internal/image"
	"grillo.local/grillo/internal/oci"
)

// Build errors.
var (
	ErrToolMissing = errors.New("build: required tool is not installed")
	ErrNoContext   = errors.New("build: context directory is missing")
	ErrNoReference = errors.New("build: a result reference is required")
)

// Request describes a single build.
type Request struct {
	// ContextDir is the build context. Dockerfile and .dockerignore inside it
	// are interpreted by the builder, never by Grillo.
	ContextDir string `json:"contextDir"`
	// Dockerfile is the containerfile path, relative to the context unless
	// absolute. Empty uses the builder default.
	Dockerfile string `json:"dockerfile,omitempty"`
	// Reference is the tag the result is recorded under.
	Reference string `json:"reference"`
	// Tags are additional tags to publish.
	Tags []string `json:"tags,omitempty"`
	// BuildArgs are builder build arguments.
	BuildArgs map[string]string `json:"buildArgs,omitempty"`
	// Labels are OCI labels on the result.
	Labels map[string]string `json:"labels,omitempty"`
	// Target selects a multi-stage build target.
	Target string `json:"target,omitempty"`
	// Platform is the requested platform, e.g. linux/amd64.
	Platform string `json:"platform,omitempty"`
	// NoCache disables the builder cache.
	NoCache bool `json:"noCache,omitempty"`
	// Pull forces the builder to refresh base images.
	Pull bool `json:"pull,omitempty"`
	// Network selects the build network mode, e.g. "none" for hermetic builds.
	Network string `json:"network,omitempty"`
}

// Validate reports whether the request is well formed.
func (r Request) Validate() error {
	if r.ContextDir == "" {
		return ErrNoContext
	}
	info, err := os.Stat(r.ContextDir)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrNoContext, r.ContextDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrNoContext, r.ContextDir)
	}
	if r.Reference == "" && len(r.Tags) == 0 {
		return ErrNoReference
	}
	return nil
}

// Result describes a completed build.
type Result struct {
	Reference      string
	ManifestDigest string
	Record         image.Record
	// Image is the imported, verified image, ready to unpack.
	Image     oci.PulledImage
	FromCache bool
	Duration  time.Duration
}

// Progress receives human-readable builder output lines.
type Progress func(line string)

// Builder builds an OCI image and imports it into the CAS and image store.
type Builder interface {
	// Available reports whether the builder can run, with an actionable error.
	Available(ctx context.Context) error
	// Build executes a build. It must be safe to cancel through ctx.
	Build(ctx context.Context, request Request, progress Progress) (Result, error)
}
