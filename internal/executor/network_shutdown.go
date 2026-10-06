//go:build linux

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/albertize/grillo/internal/netns"
)

// ShutdownNetwork is a journaled down action, separate from VM replacement. A
// temporary zero-Pod interval during Recreate must not destroy Service VIPs or
// change published localhost endpoints.
func (e *Executor) ShutdownNetwork(ctx context.Context, application string) error {
	if !e.cfg.EnableNetwork {
		return nil
	}
	if len(e.Sandboxes(application)) != 0 {
		return fmt.Errorf("executor: network still owns sandboxes")
	}
	if err := e.closeIngress(application); err != nil {
		return err
	}
	e.networkMu.Lock()
	defer e.networkMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client := &netns.Client{SocketPath: e.socketPath(application)}
	if err := client.PingContext(ctx); err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return nil
		}
		return err
	}
	if _, err := client.UpdateServices(ctx, nil); err != nil {
		return err
	}
	if err := client.ShutdownContext(ctx); err != nil {
		return err
	}
	e.launchMu.Lock()
	helper := e.supervisors[application]
	delete(e.supervisors, application)
	e.launchMu.Unlock()
	if helper != nil {
		if err := helper.Stop(2 * time.Second); err != nil {
			return err
		}
	}
	for {
		err := client.PingContext(ctx)
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}
