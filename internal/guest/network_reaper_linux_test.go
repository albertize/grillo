//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestNetworkCommandsHaveSingleWaitOwner(t *testing.T) {
	reaper := NewReaper()
	reaper.Run()
	defer reaper.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < 100; i++ {
		if err := runNetworkCommand(ctx, exec.Command("/bin/true"), reaper); err != nil {
			t.Fatal(err)
		}
	}
	if err := runNetworkCommand(ctx, exec.Command("/bin/false"), reaper); err == nil {
		t.Fatal("failed network helper accepted")
	}
	short, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stop()
	if err := runNetworkCommand(short, exec.Command("/bin/sleep", "10"), reaper); err == nil {
		t.Fatal("network helper ignored cancellation")
	}
}
