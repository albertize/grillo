//go:build linux

// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"grillo.local/grillo/internal/plan"
)

// recover is called before the API accepts requests, after verified backend
// cleanup. It deliberately recreates VMs; it does not promise seamless adoption.
func (c *core) recover(ctx context.Context) error {
	for _, record := range c.persistent.Records() {
		app := record.Desired
		observed := record.Observed
		observed.Sandboxes = map[string]plan.ObservedSandbox{}
		observed.Revision, observed.RoutesHash = "", ""
		if err := c.persistent.Save(app.Identity.Name, observed); err != nil {
			return err
		}
		c.exec.SetDesired(app)
		c.mu.Lock()
		c.apps[app.Identity.Name] = true
		c.desired[app.Identity.Name] = app
		c.mu.Unlock()
		if record.Stopped {
			if _, err := c.Down(ctx, app.Identity.Name, record.RemoveVolumes); err != nil {
				return err
			}
		} else {
			if _, err := c.Apply(ctx, app); err != nil {
				return err
			}
		}
	}
	return nil
}
