//go:build linux

// SPDX-License-Identifier: Apache-2.0

package build

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/guestproto"
	"github.com/albertize/grillo/internal/sandbox"
)

type bootContractBackend struct {
	spec                    sandbox.Spec
	creates, stops, deletes int
	stopErr                 error
	startErr                error
}

func (*bootContractBackend) Capabilities(context.Context) (sandbox.Capabilities, error) {
	return sandbox.Capabilities{}, nil
}
func (b *bootContractBackend) Create(_ context.Context, spec sandbox.Spec, op sandbox.OperationID) (sandbox.Handle, error) {
	b.creates++
	b.spec = spec
	return sandbox.Handle{ID: spec.ID, Operation: op}, spec.Validate()
}
func (b *bootContractBackend) Start(context.Context, sandbox.Handle) error { return b.startErr }
func (*bootContractBackend) Inspect(context.Context, sandbox.Handle) (sandbox.Observation, error) {
	return sandbox.Observation{}, nil
}
func (b *bootContractBackend) Stop(context.Context, sandbox.Handle, time.Duration) error {
	b.stops++
	return b.stopErr
}
func (b *bootContractBackend) Delete(context.Context, sandbox.Handle) error { b.deletes++; return nil }

type blockBootClient struct {
	*fakeSandboxClient
	capabilities []guestproto.Capability
}

func (c *blockBootClient) Info() guestproto.HandshakeInfo {
	return guestproto.HandshakeInfo{Capabilities: c.capabilities}
}

func TestBootBlockAttachmentOfflineSelectionAndOldGuestRejection(t *testing.T) {
	for _, scenario := range []string{"success", "offline", "old", "stop-failure", "start-and-stop-failure"} {
		t.Run(scenario, func(t *testing.T) {
			backend, offline := &bootContractBackend{}, &bootContractBackend{}
			client := &blockBootClient{fakeSandboxClient: &fakeSandboxClient{}, capabilities: []guestproto.Capability{guestproto.CapBuildExport, guestproto.CapBlockBuild}}
			g := &GuestBoot{Backend: backend, Kernel: "kernel", Initramfs: "initrd", GuestKey: make([]byte, 32), VsockPort: 1024, Dial: func(context.Context, uint32, uint32) (SandboxClient, error) { return client, nil }}
			if scenario == "offline" {
				g.OfflineBackend = offline
				g.Network = &BuildNetwork{}
			}
			if scenario == "old" {
				client.capabilities = nil
			}
			if scenario == "stop-failure" || scenario == "start-and-stop-failure" {
				backend.stopErr = errors.New("cannot stop")
			}
			if scenario == "start-and-stop-failure" {
				backend.startErr = errors.New("cannot start")
			}
			seed := &guestproto.BuildSeedSpec{SourceTag: "build-input", TargetTag: "build", Bytes: 1536, Entries: 1, SHA256: strings.Repeat("0", 64)}
			_, cleanup, err := g.BootBlock(context.Background(), "/owned/workspace.img", "/owned/input", seed, false)
			selected := backend
			if scenario == "offline" {
				selected = offline
				if backend.creates != 0 {
					t.Fatal("online backend used for offline build")
				}
			}
			if len(selected.spec.Disks) != 1 || selected.spec.Disks[0].Format != "raw" || len(selected.spec.Shares) != 1 || !selected.spec.Shares[0].ReadOnly || selected.spec.Shares[0].Tag != "build-input" {
				t.Fatal("block/share mapping", selected.spec)
			}
			if scenario == "old" {
				if err == nil || selected.deletes != 1 || len(client.starts) != 0 {
					t.Fatal("old guest executed block import", err)
				}
				return
			}
			if scenario == "start-and-stop-failure" {
				if err == nil || cleanup == nil || selected.deletes != 0 {
					t.Fatal("failed cleanup capability lost", err)
				}
				return
			}
			if err != nil || cleanup == nil || len(client.starts) != 1 {
				t.Fatal("boot", err)
			}
			request := client.starts[0]
			if request.Sandbox.BuildSeed != seed || request.Sandbox.Validate() != nil {
				t.Fatal("guest block seed mapping")
			}
			cleanErr := cleanup()
			if scenario == "stop-failure" {
				if cleanErr == nil || selected.deletes != 0 {
					t.Fatal("stop failure ignored", cleanErr)
				}
			} else if cleanErr != nil || selected.deletes != 1 {
				t.Fatal("cleanup", cleanErr)
			}
		})
	}
}
