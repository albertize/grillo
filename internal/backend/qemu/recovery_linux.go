//go:build linux

// SPDX-License-Identifier: Apache-2.0

package qemu

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	linux "grillo.local/grillo/internal/platform/linux"
	"grillo.local/grillo/internal/sandbox"
)

// identifyVMM obtains a host-namespace PID from the kernel, even if QEMU was
// launched in another PID namespace. No guest-supplied PID authorizes a kill.
func identifyVMM(ctx context.Context, dir string) (*persistedProcess, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		conn, err := (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(dir, "qmp.sock"))
		if err == nil {
			defer conn.Close()
			raw, err := conn.(*net.UnixConn).SyscallConn()
			if err != nil {
				return nil, err
			}
			var cred *unix.Ucred
			var sockErr error
			if err := raw.Control(func(fd uintptr) { cred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); err != nil {
				return nil, err
			}
			if sockErr != nil {
				return nil, sockErr
			}
			if cred.Uid != uint32(os.Getuid()) {
				return nil, fmt.Errorf("qemu: unexpected QMP peer UID")
			}
			id, err := linux.Identify(int(cred.Pid))
			if err != nil {
				return nil, err
			}
			return &persistedProcess{PID: id.PID, BootID: id.BootID, StartTime: id.StartTime, Executable: id.Executable}, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("qemu: cannot identify VMM: %w", ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// stopVerified distinguishes absence from ambiguous identity or read failures.
// It never silently treats permission errors or a recycled PID as a stopped VM.
func stopVerified(ctx context.Context, pp persistedProcess) error {
	id := processFromPersisted(&pp)
	current, err := linux.Identify(pp.PID)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if current.BootID != id.BootID {
		return nil
	} // previous host boot
	if current.StartTime != id.StartTime || (id.Executable != "" && current.Executable != "" && current.Executable != id.Executable) {
		return fmt.Errorf("qemu: process identity changed for PID %d; recovery blocked", pp.PID)
	}
	if !id.Alive() {
		return nil
	}
	if _, err := linux.SignalGroup(id, syscall.SIGKILL); err != nil {
		return err
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for id.Alive() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("qemu: process %d did not stop", pp.PID)
		case <-time.After(20 * time.Millisecond):
		}
	}
	return nil
}

// Recover removes verified old sandboxes before durable intent is replayed.
// This is called before serving requests, never concurrently with normal work.
func (b *Backend) Recover(ctx context.Context) error {
	b.mu.Lock()
	var ids []string
	for id := range b.sandboxes {
		ids = append(ids, id)
	}
	b.mu.Unlock()
	sort.Strings(ids)
	for _, id := range ids {
		entry, err := b.entry(id)
		if err != nil {
			return err
		}
		ps := entry.persisted
		if ps.QEMU == nil && (ps.State == string(sandbox.StateRunning) || ps.State == "booting") {
			pp, err := identifyVMM(ctx, entry.dir)
			if err != nil {
				return fmt.Errorf("qemu: sandbox %s has no verified process identity; recovery blocked: %w", id, err)
			}
			ps.QEMU = pp
		}
		if ps.QEMU != nil {
			if err := stopVerified(ctx, *ps.QEMU); err != nil {
				return err
			}
		}
		for _, pp := range ps.VirtioFSD {
			if err := stopVerified(ctx, pp); err != nil {
				return err
			}
		}
		// Deletion is authorized only after every recorded process was checked.
		if err := b.Delete(ctx, sandbox.Handle{ID: id}); err != nil {
			return err
		}
	}
	return nil
}
