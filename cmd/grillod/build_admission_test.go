//go:build linux

// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/albertize/grillo/internal/build"
	"github.com/albertize/grillo/internal/image"
	"github.com/albertize/grillo/internal/oci"
)

func TestNativeBuildAdmissionCancellationAndRetainedState(t *testing.T) {
	c := &core{buildCalls: make(chan struct{}, 1)}
	c.buildCalls <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Build(ctx, build.Request{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("queued build ignored cancellation", err)
	}
	<-c.buildCalls
	c.buildBlocked = errors.New("cleanup latch")
	if _, err := c.Build(context.Background(), build.Request{}, nil); err != c.buildBlocked {
		t.Fatal("cleanup latch ignored", err)
	}
	c.buildBlocked = nil
	cas, err := oci.OpenCAS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := image.Open(filepath.Join(t.TempDir(), "images"), cas)
	if err != nil {
		t.Fatal(err)
	}
	c.cas, c.images, c.puller = cas, store, &oci.Puller{CAS: cas}
	c.buildScratch = t.TempDir()
	kept := filepath.Join(c.buildScratch, "retained-workspace")
	if err := os.Mkdir(kept, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Build(context.Background(), build.Request{}, nil); err == nil || !strings.Contains(err.Error(), "interrupted native workspace") {
		t.Fatal("interrupted state ignored", err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatal("preexisting workspace changed", err)
	}
}
