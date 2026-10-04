// SPDX-License-Identifier: Apache-2.0

package build

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/albertize/grillo/internal/guestproto"
)

// SandboxClient is the guest surface a sandbox runner needs. *guestproto.Client
// implements it.
type SandboxClient interface {
	Start(ctx context.Context, request guestproto.StartRequest) (guestproto.StartResult, error)
	Run(ctx context.Context, request guestproto.RunRequest, stdout, stderr io.Writer) (guestproto.RunResult, error)
	Close() error
}

// SandboxRunner executes RUN steps inside booted build guests. It boots one
// guest per distinct build root and reuses it for later steps against the same
// root, so a multi-stage build boots at most one guest per stage.
type SandboxRunner struct {
	// Boot boots a guest that shares rootfsDir and returns a client and a
	// cleanup function.
	Boot func(ctx context.Context, rootfsDir string) (SandboxClient, func() error, error)
	// Share is the guest share tag under which the root is mounted.
	Share string
	// User is applied to steps that do not specify one.
	User guestproto.UserSpec

	mu      sync.Mutex
	clients map[string]sandboxHandle
}

type sandboxHandle struct {
	client  SandboxClient
	cleanup func() error
}

// Run implements Runner.
func (r *SandboxRunner) Run(ctx context.Context, step RunStep, progress Progress) error {
	if r.Boot == nil {
		return fmt.Errorf("build: sandbox runner has no boot function")
	}
	client, err := r.clientFor(ctx, step.RootfsDir)
	if err != nil {
		return err
	}
	user, err := parseUserSpec(step.User)
	if err != nil {
		return err
	}
	if user.UID == 0 && user.GID == 0 && r.User.UID != 0 {
		user = r.User
	}
	var stdout, stderr io.Writer
	if progress != nil {
		stdout = progressWriter{progress}
		stderr = progressWriter{progress}
	}
	result, err := client.Run(ctx, guestproto.RunRequest{
		Share:   r.Share,
		Args:    step.Command,
		Env:     step.Env,
		WorkDir: step.WorkDir,
		User:    user,
	}, stdout, stderr)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("build: RUN %s exited with code %d", strings.Join(step.Command, " "), result.ExitCode)
	}
	return nil
}

func (r *SandboxRunner) clientFor(ctx context.Context, rootfsDir string) (SandboxClient, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if handle, ok := r.clients[rootfsDir]; ok {
		return handle.client, nil
	}
	client, cleanup, err := r.Boot(ctx, rootfsDir)
	if err != nil {
		return nil, err
	}
	if r.clients == nil {
		r.clients = map[string]sandboxHandle{}
	}
	r.clients[rootfsDir] = sandboxHandle{client: client, cleanup: cleanup}
	return client, nil
}

// Close stops every booted build guest.
func (r *SandboxRunner) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var firstErr error
	for dir, handle := range r.clients {
		if err := handle.client.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		if handle.cleanup != nil {
			if err := handle.cleanup(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		delete(r.clients, dir)
	}
	return firstErr
}

type progressWriter struct{ progress Progress }

func (w progressWriter) Write(p []byte) (int, error) {
	if w.progress != nil && len(p) > 0 {
		w.progress(strings.TrimRight(string(p), "\n"))
	}
	return len(p), nil
}

func parseUserSpec(user string) (guestproto.UserSpec, error) {
	spec := guestproto.UserSpec{}
	user = strings.TrimSpace(user)
	if user == "" {
		return spec, nil
	}
	uid, gid, found := strings.Cut(user, ":")
	parsedUID, err := strconv.Atoi(uid)
	if err != nil {
		return spec, fmt.Errorf("build: USER requires a numeric uid (got %q)", user)
	}
	spec.UID = uint32(parsedUID)
	if found {
		parsedGID, err := strconv.Atoi(gid)
		if err != nil {
			return spec, fmt.Errorf("build: USER requires a numeric gid (got %q)", user)
		}
		spec.GID = uint32(parsedGID)
	}
	return spec, nil
}
