//go:build linux

// SPDX-License-Identifier: Apache-2.0

package build

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/albertize/grillo/internal/guestproto"
)

// BlockRunner opts native RUN into a fresh fixed-geometry ext4 disk per step.
// Host COPY happens only after guest cleanup and validated snapshot adoption.
type BlockRunner struct {
	Formatter  string
	ImageBytes int64
	Boot       func(context.Context, string, string, *guestproto.BuildSeedSpec, bool) (SandboxClient, func() error, error)
	mu         sync.Mutex
	pending    []func() error
	retained   bool
}

func (r *BlockRunner) PreserveScratch() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.retained }

func (r *BlockRunner) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var remaining []func() error
	var result error
	for _, cleanup := range r.pending {
		if err := cleanup(); err != nil {
			remaining = append(remaining, cleanup)
			result = errors.Join(result, err)
		}
	}
	r.pending = remaining
	return result
}

func (r *BlockRunner) Run(ctx context.Context, step RunStep, progress Progress) (err error) {
	if step.budget == nil || step.owners == nil || step.rootMetadata == nil || r.Boot == nil {
		return errors.New("build: block RUN requires native-owned workspace/budget and block boot")
	}
	if r.ImageBytes < 16<<20 || r.ImageBytes%4096 != 0 || !filepath.IsAbs(r.Formatter) {
		return errors.New("build: invalid block workspace geometry or formatter path")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	version := exec.CommandContext(ctx, r.Formatter, "-V")
	version.Env = []string{"PATH=/usr/sbin:/usr/bin", "LC_ALL=C"}
	var output formatterOutput
	version.Stdout, version.Stderr = &output, &output
	if err := version.Run(); err != nil || !strings.HasPrefix(output.text, "mke2fs 1.47.3 ") {
		return errors.New("build: reviewed mke2fs 1.47.3 required")
	}
	if err := step.budget.reserve("block-run/input/root.tar", r.ImageBytes); err != nil {
		return err
	}
	work, err := os.MkdirTemp(filepath.Dir(step.RootfsDir), "block-run-")
	if err != nil {
		return err
	}
	preserve := false
	defer func() {
		if !preserve {
			err = errors.Join(err, os.RemoveAll(work))
		}
	}()
	input := filepath.Join(work, "input")
	if err := os.Mkdir(input, 0700); err != nil {
		return err
	}
	seed, err := writeBlockSeed(ctx, filepath.Join(input, "root.tar"), step)
	if err != nil {
		return err
	}
	disk := filepath.Join(work, "workspace.img")
	f, err := os.OpenFile(disk, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	if err := f.Truncate(r.ImageBytes); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	format := exec.CommandContext(ctx, r.Formatter, "-F", "-q", "-t", "ext4", "-b", "4096", "-m", "0", "-O", "^has_journal", "-E", "lazy_itable_init=0,nodiscard,root_owner=0:0,root_perms=0755", disk)
	format.Env = version.Env
	output.text = ""
	format.Stdout, format.Stderr = &output, &output
	if err := format.Run(); err != nil {
		return fmt.Errorf("build: format block workspace: %w", err)
	}
	if err := verifyWorkspaceDisk(disk, r.ImageBytes); err != nil {
		return err
	}
	client, cleanup, bootErr := r.Boot(ctx, disk, input, seed, step.Network)
	cleaned := false
	finish := func() error {
		if cleaned {
			return nil
		}
		if cleanup == nil {
			return errors.New("build: block boot did not provide owned cleanup")
		}
		if err := cleanup(); err != nil {
			return err
		}
		if client != nil {
			_ = client.Close()
		}
		cleaned = true
		return nil
	}
	defer func() {
		if cleanup == nil && bootErr != nil {
			return
		}
		if cleanErr := finish(); cleanErr != nil {
			preserve = true
			r.mu.Lock()
			r.retained = true
			r.pending = append(r.pending, finish)
			r.mu.Unlock()
			err = errors.Join(err, fmt.Errorf("build: guest cleanup failed; owned workspace retained at %s: %w", work, cleanErr))
		}
	}()
	if bootErr != nil {
		return fmt.Errorf("build: boot block workspace: %w", bootErr)
	}
	if client == nil || cleanup == nil {
		preserve = true
		r.mu.Lock()
		r.retained = true
		r.mu.Unlock()
		return errors.New("build: invalid block boot ownership contract; workspace retained")
	}
	exporter, ok := client.(snapshotExporter)
	if !ok {
		return errors.New("build: guest client cannot export block snapshot")
	}
	user, err := parseUserSpec(step.User)
	if err != nil {
		return err
	}
	var stdout io.Writer
	stdout = &blockRunOutput{ctx: ctx, remaining: 4 << 20, progress: progress}
	result, err := client.Run(ctx, guestproto.RunRequest{Share: "build", Args: step.Command, Env: step.Env, WorkDir: step.WorkDir, User: user}, stdout, stdout)
	if err != nil {
		_ = client.Close()
		return fmt.Errorf("build: execute block RUN: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("build: block RUN exited with code %d", result.ExitCode)
	}
	root, owners, metadata, err := exportBuildSnapshotWithMetadata(ctx, exporter, "build", work, step.budget)
	if err != nil {
		return fmt.Errorf("build: export block workspace: %w", err)
	}
	if metadata == nil {
		return errors.New("build: block export lacks root metadata; rebuild guest")
	}
	if err := finish(); err != nil {
		return err
	}
	if err := verifyWorkspaceDisk(disk, r.ImageBytes); err != nil {
		return err
	}
	previous := filepath.Join(work, "previous-root")
	if err := os.Rename(step.RootfsDir, previous); err != nil {
		return err
	}
	if err := os.Rename(root, step.RootfsDir); err != nil {
		if restoreErr := os.Rename(previous, step.RootfsDir); restoreErr != nil {
			preserve = true
			r.mu.Lock()
			r.retained = true
			r.mu.Unlock()
			return errors.Join(err, restoreErr)
		}
		return err
	}
	clear(step.owners)
	for name, owner := range owners {
		step.owners[name] = owner
	}
	*step.rootMetadata = *metadata
	return nil
}

func writeBlockSeed(ctx context.Context, name string, step RunStep) (*guestproto.BuildSeedSpec, error) {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	hash := sha256.New()
	w := &workspaceWriter{budget: step.budget, writer: io.MultiWriter(f, hash), remaining: guestproto.MaxExportBytes}
	// A root header precedes the full tree. The following writer's own trailer
	// closes this same tar stream; this header writer must not append a trailer.
	tw := tar.NewWriter(w)
	root := step.rootMetadata
	if err := tw.WriteHeader(&tar.Header{Name: ".", Typeflag: tar.TypeDir, Mode: int64(root.Mode), Uid: int(root.UID), Gid: int(root.GID)}); err != nil {
		return nil, err
	}
	snapshot, err := snapshotTreeContext(ctx, step.RootfsDir, guestproto.MaxExportEntries-1)
	if err != nil {
		return nil, err
	}
	for name := range snapshot {
		for _, part := range strings.Split(name, "/") {
			if strings.HasPrefix(part, ".wh.") {
				return nil, errors.New("build: reserved whiteout in block seed")
			}
		}
	}
	count, err := writeDiffLayerContext(ctx, nil, step.RootfsDir, step.owners, w)
	if err != nil {
		return nil, err
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return &guestproto.BuildSeedSpec{SourceTag: "build-input", TargetTag: "build", Bytes: info.Size(), Entries: int64(count + 1), SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func verifyWorkspaceDisk(name string, capacity int64) error {
	f, err := os.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != capacity || info.Mode().Perm()&0077 != 0 {
		return errors.New("build: workspace disk geometry/privacy changed")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("build: workspace disk ownership/links changed")
	}
	var super [64]byte
	if _, err := f.ReadAt(super[:], 1024); err != nil {
		return err
	}
	if binary.LittleEndian.Uint16(super[56:58]) != 0xef53 {
		return errors.New("build: invalid workspace ext4 superblock")
	}
	return f.Sync()
}

type formatterOutput struct{ text string }

func (o *formatterOutput) Write(p []byte) (int, error) {
	if len(o.text)+len(p) > 4096 {
		return 0, errors.New("build: formatter output limit")
	}
	o.text += string(p)
	return len(p), nil
}

type blockRunOutput struct {
	ctx       context.Context
	remaining int64
	progress  Progress
}

func (w *blockRunOutput) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > w.remaining {
		return 0, errors.New("build: RUN aggregate output exceeds 4 MiB")
	}
	w.remaining -= int64(len(p))
	if w.progress != nil {
		w.progress(strings.TrimRight(string(p), "\n"))
	}
	return len(p), nil
}
