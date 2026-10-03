//go:build linux

// SPDX-License-Identifier: Apache-2.0

package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"grillo.local/grillo/internal/guestproto"
	"grillo.local/grillo/internal/sandbox"
)

// GuestBoot boots a build guest that shares a host directory and speaks the
// guest protocol. It is the production Boot function for SandboxRunner.
type GuestBoot struct {
	Backend      sandbox.Backend
	Kernel       string
	Initramfs    string
	KernelArgs   string
	GuestKey     []byte
	VsockCIDBase uint32
	VsockPort    uint32
	MemoryMiB    int
	ShareTag     string
	ShareTarget  string
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
	if g.Backend == nil {
		return nil, nil, fmt.Errorf("build: guest boot requires a backend")
	}
	if g.Kernel == "" || g.Initramfs == "" {
		return nil, nil, fmt.Errorf("build: guest boot requires a kernel and initramfs")
	}
	if len(g.GuestKey) < 16 {
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
	guestSpec := &guestproto.SandboxSpec{
		ID:     id,
		Build:  true,
		Shares: []guestproto.ShareSpec{{Tag: tag, Target: target}},
	}
	cleanup := func() error {
		background := context.Background()
		_ = g.Backend.Stop(background, sandbox.Handle{ID: id}, 5*time.Second)
		_ = g.Backend.Delete(background, sandbox.Handle{ID: id})
		if g.Network != nil {
			g.Network.Release(id)
		}
		return nil
	}
	if g.Network != nil {
		ip, prefix, gateway, err := g.Network.Prepare(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		spec.Application = g.Network.application()
		guestSpec.Network = &guestproto.NetworkConfig{Interface: "eth0", Address: ip, PrefixLen: prefix, Gateway: gateway}
		guestSpec.Nameservers = g.Nameservers
	}
	if _, err := g.Backend.Create(ctx, spec, sandbox.OperationID("build-"+id)); err != nil {
		return nil, nil, err
	}
	if err := g.Backend.Start(ctx, sandbox.Handle{ID: id}); err != nil {
		_ = cleanup()
		return nil, nil, err
	}
	dial := g.Dial
	if dial == nil {
		key := g.GuestKey
		dial = func(ctx context.Context, cid, port uint32) (SandboxClient, error) {
			conn, err := guestproto.DialVsock(ctx, cid, port)
			if err != nil {
				return nil, err
			}
			return guestproto.NewClient(ctx, conn, guestproto.HandshakeConfig{Key: key, Agent: "builder"})
		}
	}
	client, err := dial(ctx, cid, g.VsockPort)
	if err != nil {
		_ = cleanup()
		return nil, nil, err
	}
	if _, err := client.Start(ctx, guestproto.StartRequest{Sandbox: guestSpec}); err != nil {
		_ = client.Close()
		_ = cleanup()
		return nil, nil, err
	}
	return client, func() error {
		_ = client.Close()
		return cleanup()
	}, nil
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:4])
}
