//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"errors"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

// maxRetainedStatuses bounds the statuses kept for processes waited on after
// they were reaped, so a burst of untracked orphans cannot grow memory.
const maxRetainedStatuses = 1024

// ExitStatus is a reaped child's outcome.
type ExitStatus struct {
	PID      int
	ExitCode int
	Signal   unix.Signal
	Signaled bool
	Core     bool
}

// Reaper is the single owner of child reaping. PID 1 must wait for every child;
// routing all waits through one loop avoids the race between os/exec's Wait and
// wait4(-1) stealing each other's children. Processes started through the
// reaper are reaped by it, not by exec.Cmd.Wait, so their stdout/stderr must be
// files (not pipes) and callers must use Wait.
type Reaper struct {
	mu       sync.Mutex
	waiters  map[int]chan ExitStatus
	retained map[int]ExitStatus
	order    []int

	startOnce sync.Once
	stop      chan struct{}
	done      chan struct{}
	stopOnce  sync.Once
	ran       atomic.Bool
}

// NewReaper returns a reaper with no running loop.
func NewReaper() *Reaper {
	return &Reaper{
		waiters:  make(map[int]chan ExitStatus),
		retained: make(map[int]ExitStatus),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Run starts the wait4 loop. It is safe to call once.
func (r *Reaper) Run() {
	r.startOnce.Do(func() {
		r.ran.Store(true)
		go func() {
			defer close(r.done)
			r.loop()
		}()
	})
}

// Stop terminates the loop and waits for it to exit. It does not kill children.
func (r *Reaper) Stop() {
	if !r.ran.Load() {
		return
	}
	r.stopOnce.Do(func() { close(r.stop) })
	<-r.done
}

func (r *Reaper) loop() {
	for {
		select {
		case <-r.stop:
			return
		default:
		}
		var ws unix.WaitStatus
		pid, err := unix.Wait4(-1, &ws, unix.WNOHANG, nil)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			r.sleep()
			continue
		}
		if pid == 0 {
			// No child has changed state yet.
			r.sleep()
			continue
		}
		r.deliver(pid, statusFrom(pid, ws))
	}
}

func (r *Reaper) sleep() {
	select {
	case <-r.stop:
	case <-time.After(5 * time.Millisecond):
	}
}

// Start launches cmd and arranges for the reaper to collect it. The caller must
// call Wait to obtain the exit status; cmd.Wait must not be called. A fast
// process is retained until Wait consumes it.
func (r *Reaper) Start(cmd *exec.Cmd) error {
	return cmd.Start()
}

// Watch registers interest in a pid. It is idempotent and safe to call before or
// after the process exits.
func (r *Reaper) Watch(pid int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, done := r.retained[pid]; done {
		return
	}
	if r.waiters[pid] == nil {
		r.waiters[pid] = make(chan ExitStatus, 1)
	}
}

// Wait blocks until pid exits and returns its status.
func (r *Reaper) Wait(pid int) ExitStatus {
	r.mu.Lock()
	if st, done := r.retained[pid]; done {
		delete(r.retained, pid)
		r.mu.Unlock()
		return st
	}
	ch := r.waiters[pid]
	if ch == nil {
		ch = make(chan ExitStatus, 1)
		r.waiters[pid] = ch
	}
	r.mu.Unlock()
	return <-ch
}

func (r *Reaper) deliver(pid int, st ExitStatus) {
	r.mu.Lock()
	ch := r.waiters[pid]
	if ch != nil {
		delete(r.waiters, pid)
	}
	r.mu.Unlock()
	if ch != nil {
		ch <- st
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.retained[pid] = st
	r.order = append(r.order, pid)
	if len(r.order) > maxRetainedStatuses {
		delete(r.retained, r.order[0])
		r.order = r.order[1:]
	}
}

func statusFrom(pid int, ws unix.WaitStatus) ExitStatus {
	st := ExitStatus{PID: pid}
	if ws.Signaled() {
		st.Signaled = true
		st.Signal = ws.Signal()
		st.Core = ws.CoreDump()
		st.ExitCode = 128 + int(ws.Signal())
	} else {
		st.ExitCode = ws.ExitStatus()
	}
	return st
}
