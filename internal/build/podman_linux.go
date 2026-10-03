//go:build linux

// SPDX-License-Identifier: Apache-2.0

package build

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"grillo.local/grillo/internal/image"
	"grillo.local/grillo/internal/oci"
)

// PodmanBuilder builds images with rootless Podman. Grillo passes the context,
// Dockerfile, and build arguments through unchanged, so Dockerfile and
// .dockerignore interpretation stays with the builder.
type PodmanBuilder struct {
	// Binary is the Podman executable; empty means "podman".
	Binary string
	// CAS receives the verified result blobs.
	CAS *oci.CAS
	// Store records the result.
	Store *image.Store
	// Platform is the requested platform for layout selection.
	Platform oci.Platform
	// MaxBlobBytes bounds each imported blob (<=0 uses the OCI default).
	MaxBlobBytes int64
	// TempDir is the parent for build scratch directories.
	TempDir string
	// Env, when non-nil, replaces the child environment.
	Env []string
}

func (b *PodmanBuilder) binary() string {
	if b.Binary == "" {
		return "podman"
	}
	return b.Binary
}

// Available checks that Podman exists and runs. The error is actionable: it
// distinguishes a missing tool from an installed-but-unusable one.
func (b *PodmanBuilder) Available(ctx context.Context) error {
	path, err := exec.LookPath(b.binary())
	if err != nil {
		return fmt.Errorf("%w: %q not found in PATH; install rootless Podman or choose another builder", ErrToolMissing, b.binary())
	}
	cmd := exec.CommandContext(ctx, path, "info", "--format", "{{.Host.Security.Rootless}}")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build: %q is installed but not usable (%v); configure rootless Podman storage", b.binary(), err)
	}
	return nil
}

// Build runs `podman build`, exports the result as an OCI layout, imports it
// into the CAS, and records it.
func (b *PodmanBuilder) Build(ctx context.Context, request Request, progress Progress) (Result, error) {
	if b.Store == nil {
		return Result{}, errors.New("build: PodmanBuilder requires an image store")
	}
	if b.CAS == nil {
		return Result{}, errors.New("build: PodmanBuilder requires a CAS")
	}
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	if err := b.Available(ctx); err != nil {
		return Result{}, err
	}
	reference := request.Reference
	if reference == "" {
		reference = request.Tags[0]
	}

	started := time.Now()
	prior, hadPrior, err := b.Store.Get(reference)
	if err != nil {
		return Result{}, err
	}

	workDir, err := os.MkdirTemp(b.TempDir, "grillo-build-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(workDir)
	layoutDir := filepath.Join(workDir, "oci")

	tag := "localhost/grillo-build-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	args := []string{"build", "-t", tag}
	if request.Dockerfile != "" {
		args = append(args, "--file", request.Dockerfile)
	}
	for _, key := range sortedKeys(request.BuildArgs) {
		args = append(args, "--build-arg", key+"="+request.BuildArgs[key])
	}
	for _, key := range sortedKeys(request.Labels) {
		args = append(args, "--label", key+"="+request.Labels[key])
	}
	if request.Target != "" {
		args = append(args, "--target", request.Target)
	}
	if request.Platform != "" {
		args = append(args, "--platform", request.Platform)
	}
	if request.Network != "" {
		args = append(args, "--network", request.Network)
	}
	if request.NoCache {
		args = append(args, "--no-cache")
	}
	if request.Pull {
		args = append(args, "--pull")
	}
	args = append(args, request.ContextDir)
	if err := b.run(ctx, args, progress); err != nil {
		return Result{}, err
	}

	defer func() { _ = b.run(ctx, []string{"rmi", tag}, nil) }()
	if err := b.run(ctx, []string{"save", "--format", "oci-dir", "-o", layoutDir, tag}, progress); err != nil {
		return Result{}, err
	}

	pulled, err := oci.ImportLayout(layoutDir, tag, b.CAS, b.Platform, b.MaxBlobBytes)
	if err != nil {
		return Result{}, fmt.Errorf("build: import result: %w", err)
	}
	record, err := b.Store.Import(pulled, image.Meta{
		Reference: reference,
		Source:    image.SourceBuild,
		Labels:    request.Labels,
	})
	if err != nil {
		return Result{}, err
	}
	return Result{
		Reference:      reference,
		ManifestDigest: pulled.ManifestDigest,
		Record:         record,
		Image:          pulled,
		FromCache:      hadPrior && prior.ManifestDigest == pulled.ManifestDigest,
		Duration:       time.Since(started),
	}, nil
}

func (b *PodmanBuilder) run(ctx context.Context, args []string, progress Progress) error {
	cmd := exec.CommandContext(ctx, b.binary(), args...)
	if b.Env != nil {
		cmd.Env = b.Env
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 5 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	var (
		wait sync.WaitGroup
		mu   sync.Mutex
		tail []string
	)
	collect := func(r io.Reader) {
		defer wait.Done()
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 64*1024), 1<<20)
		for scanner.Scan() {
			line := scanner.Text()
			mu.Lock()
			tail = append(tail, line)
			if len(tail) > 40 {
				tail = tail[len(tail)-40:]
			}
			mu.Unlock()
			if progress != nil {
				progress(line)
			}
		}
	}
	wait.Add(2)
	go collect(stdout)
	go collect(stderr)
	runErr := cmd.Wait()
	wait.Wait()
	if ctx.Err() != nil {
		return fmt.Errorf("build: canceled: %w", ctx.Err())
	}
	if runErr != nil {
		mu.Lock()
		context := strings.Join(tail, "\n")
		mu.Unlock()
		return fmt.Errorf("build: %s %s: %w\n%s", b.binary(), strings.Join(args, " "), runErr, context)
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
