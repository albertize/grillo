//go:build linux

// SPDX-License-Identifier: Apache-2.0

package build

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/albertize/grillo/internal/guestproto"
	"github.com/albertize/grillo/internal/sandbox"
)

// GuestBoot boots a build guest that shares a host directory and speaks the
// guest protocol. It is the production Boot function for SandboxRunner.
type GuestBoot struct {
	Backend        sandbox.Backend
	OfflineBackend sandbox.Backend
	Kernel         string
	Initramfs      string
	KernelArgs     string
	GuestKey       []byte
	FreshKeys      bool
	VsockCIDBase   uint32
	VsockPort      uint32
	MemoryMiB      int
	ShareTag       string
	ShareTarget    string
	// Network, when set, gives the build guest outbound connectivity. The
	// backend used here must have been created with Launch = Network.Launch.
	Network *BuildNetwork
	// Nameservers are upstream resolvers passed to the guest when it has no
	// cluster DNS configuration.
	Nameservers []string
	// Dial connects to the guest. It defaults to AF_VSOCK with the guest key.
	Dial func(ctx context.Context, cid, port uint32) (SandboxClient, error)

	mu   sync.Mutex
	next uint32
}

// Boot boots one build guest sharing rootfsDir.
func (g *GuestBoot) Boot(ctx context.Context, rootfsDir string) (SandboxClient, func() error, error) {
	return g.boot(ctx, rootfsDir, "", nil, true)
}

// BootBlock attaches a single raw workspace disk and a read-only seed share.
func (g *GuestBoot) BootBlock(ctx context.Context, disk, input string, seed *guestproto.BuildSeedSpec, network bool) (SandboxClient, func() error, error) {
	return g.boot(ctx, input, disk, seed, network)
}

func (g *GuestBoot) boot(ctx context.Context, rootfsDir, disk string, seed *guestproto.BuildSeedSpec, network bool) (SandboxClient, func() error, error) {
	backend := g.Backend
	if disk != "" && !network {
		if g.OfflineBackend != nil {
			backend = g.OfflineBackend
		} else if g.Network != nil {
			return nil, nil, errors.New("build: offline block backend unavailable")
		}
	}
	if backend == nil {
		return nil, nil, fmt.Errorf("build: guest boot requires a backend")
	}
	if g.FreshKeys {
		if _, ok := backend.(interface {
			BootConnection(sandbox.Handle) (uint32, uint32, []byte, error)
		}); !ok {
			return nil, nil, fmt.Errorf("build: backend cannot provide boot credentials")
		}
	}
	if g.Kernel == "" || g.Initramfs == "" {
		return nil, nil, fmt.Errorf("build: guest boot requires a kernel and initramfs")
	}
	if !g.FreshKeys && len(g.GuestKey) < 16 {
		return nil, nil, fmt.Errorf("build: guest boot requires a guest key")
	}
	tag := g.ShareTag
	if tag == "" {
		tag = "build"
	}
	target := g.ShareTarget
	if target == "" {
		target = "/build"
	}
	g.mu.Lock()
	if g.next == 0 {
		g.next = g.VsockCIDBase
	}
	if g.next == 0 {
		g.next = 200
	}
	cid := g.next
	g.next++
	g.mu.Unlock()

	id := fmt.Sprintf("grillo-build-%s-%d", shortHash(rootfsDir), cid)
	memory := g.MemoryMiB
	if memory == 0 {
		memory = 512
	}
	spec := sandbox.Spec{
		ID:          id,
		Application: "grillo-build",
		Kernel:      g.Kernel,
		Initramfs:   g.Initramfs,
		KernelArgs:  g.KernelArgs,
		VCPU:        1,
		MemoryMiB:   memory,
		Shares:      []sandbox.Share{{Tag: tag, HostPath: rootfsDir}},
		VsockCID:    cid,
		VsockPort:   g.VsockPort,
		GuestKey:    g.GuestKey,
	}
	if g.FreshKeys {
		if g.Dial != nil {
			return nil, nil, fmt.Errorf("build: custom dial cannot use fresh runtime keys")
		}
		spec.GuestKey = make([]byte, 32)
		if _, err := rand.Read(spec.GuestKey); err != nil {
			return nil, nil, err
		}
	}
	guestSpec := &guestproto.SandboxSpec{
		ID:     id,
		Build:  true,
		Shares: []guestproto.ShareSpec{{Tag: tag, Target: target}},
	}
	if disk != "" {
		if seed == nil {
			return nil, nil, errors.New("build: missing block seed")
		}
		spec.Shares = []sandbox.Share{{Tag: seed.SourceTag, HostPath: rootfsDir, ReadOnly: true}}
		spec.Disks = []sandbox.Disk{{Path: disk, Format: "raw"}}
		guestSpec.Shares = []guestproto.ShareSpec{{Tag: seed.SourceTag, Target: "/build-input", ReadOnly: true}, {Tag: seed.TargetTag, Target: target, Source: "/dev/vda", FSType: "ext4"}}
		guestSpec.BuildSeed = seed
	}
	cleanup := func() error {
		background, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := backend.Stop(background, sandbox.Handle{ID: id}, 5*time.Second); err != nil {
			return fmt.Errorf("build: stop guest: %w", err)
		}
		if err := backend.Delete(background, sandbox.Handle{ID: id}); err != nil {
			return fmt.Errorf("build: delete guest: %w", err)
		}
		if g.Network != nil {
			g.Network.Release(id)
		}
		return nil
	}
	if g.Network != nil && network {
		ip, prefix, gateway, err := g.Network.Prepare(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		spec.Application = g.Network.application()
		guestSpec.Network = &guestproto.NetworkConfig{Interface: "eth0", Address: ip, PrefixLen: prefix, Gateway: gateway}
		guestSpec.Nameservers = g.Nameservers
	}
	if _, err := backend.Create(ctx, spec, sandbox.OperationID("build-"+id)); err != nil {
		if g.Network != nil && network {
			g.Network.Release(id)
		}
		return nil, nil, err
	}
	if err := backend.Start(ctx, sandbox.Handle{ID: id}); err != nil {
		cleanErr := cleanup()
		if cleanErr != nil {
			return nil, cleanup, errors.Join(err, cleanErr)
		}
		return nil, nil, err
	}
	if g.FreshKeys {
		provider, ok := backend.(interface {
			BootConnection(sandbox.Handle) (uint32, uint32, []byte, error)
		})
		if !ok {
			err := errors.New("build: backend cannot provide boot credentials")
			cleanErr := cleanup()
			if cleanErr != nil {
				return nil, cleanup, errors.Join(err, cleanErr)
			}
			return nil, nil, err
		}
		var err error
		spec.VsockCID, spec.VsockPort, spec.GuestKey, err = provider.BootConnection(sandbox.Handle{ID: id})
		if err != nil {
			cleanErr := cleanup()
			if cleanErr != nil {
				return nil, cleanup, errors.Join(err, cleanErr)
			}
			return nil, nil, err
		}
	}
	dial := g.Dial
	if dial == nil {
		key := spec.GuestKey
		dial = func(ctx context.Context, cid, port uint32) (SandboxClient, error) {
			conn, err := guestproto.DialVsock(ctx, cid, port)
			if err != nil {
				return nil, err
			}
			return guestproto.NewClient(ctx, conn, guestproto.HandshakeConfig{Key: key, Agent: "builder"})
		}
	}
	client, err := dial(ctx, spec.VsockCID, spec.VsockPort)
	if err != nil {
		cleanErr := cleanup()
		if cleanErr != nil {
			return nil, cleanup, errors.Join(err, cleanErr)
		}
		return nil, nil, err
	}
	if disk != "" {
		informed, ok := client.(interface {
			Info() guestproto.HandshakeInfo
		})
		if !ok || !informed.Info().Supports(guestproto.CapBlockBuild) || !informed.Info().Supports(guestproto.CapBuildExport) {
			_ = client.Close()
			err := errors.New("build: guest lacks block-build/build-export; rebuild guest artifacts")
			cleanErr := cleanup()
			if cleanErr != nil {
				return nil, cleanup, errors.Join(err, cleanErr)
			}
			return nil, nil, err
		}
	}
	if _, err := client.Start(ctx, guestproto.StartRequest{Sandbox: guestSpec}); err != nil {
		_ = client.Close()
		cleanErr := cleanup()
		if cleanErr != nil {
			return nil, cleanup, errors.Join(err, cleanErr)
		}
		return nil, nil, err
	}
	return client, cleanup, nil
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:4])
}
