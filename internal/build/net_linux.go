//go:build linux

// SPDX-License-Identifier: Apache-2.0

package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"grillo.local/grillo/internal/netns"
	"grillo.local/grillo/internal/network"
	"grillo.local/grillo/internal/sandbox"
)

// Build network defaults, matching the application topology so builds reuse the
// same bridge name inside their own pasta namespace.
const (
	buildBridgeName    = "grillo0"
	buildBridgeGateway = "10.77.0.1"
	buildBridgePrefix  = 24
	buildBridgeSubnet  = "10.77.0.0/24"
)

// BuildNetwork gives a build guest an isolated pasta namespace with outbound
// connectivity. It is optional: a build with no RUN, or a RUN that does not need
// the network, works without it.
type BuildNetwork struct {
	// QEMU is the VMM binary the supervisor launches inside the namespace.
	QEMU string
	// NetnsBinary is the supervisor binary (cmd/grillo-netns).
	NetnsBinary string
	// Pasta is the pasta binary.
	Pasta string
	// RuntimeDir holds the supervisor socket, log, and IPAM state.
	RuntimeDir string
	// Application names the namespace; defaults to "grillo-build".
	Application string
	// Nameservers are the upstream resolvers written into the guest.
	Nameservers []string

	mu      sync.Mutex
	helper  *netns.Helper
	ipam    *network.IPAM
	leases  map[string]string
	started bool
}

func (n *BuildNetwork) application() string {
	if n.Application == "" {
		return "grillo-build"
	}
	return n.Application
}

func (n *BuildNetwork) socketPath() string {
	return filepath.Join(n.RuntimeDir, "netns-"+sanitizeName(n.application())+".sock")
}

// Prepare ensures the namespace exists and returns the guest network config.
func (n *BuildNetwork) Prepare(ctx context.Context, id string) (string, int, string, error) {
	if n.RuntimeDir == "" {
		return "", 0, "", errors.New("build: build network requires a runtime directory")
	}
	if err := n.ensure(ctx); err != nil {
		return "", 0, "", err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.leases == nil {
		n.leases = map[string]string{}
	}
	if ip, ok := n.leases[id]; ok {
		return ip, buildBridgePrefix, buildBridgeGateway, nil
	}
	if n.ipam == nil {
		opened, err := network.OpenIPAM(filepath.Join(n.RuntimeDir, "ipam-"+sanitizeName(n.application())), buildBridgeSubnet, buildBridgeGateway)
		if err != nil {
			return "", 0, "", err
		}
		n.ipam = opened
	}
	lease, err := n.ipam.Allocate(id, n.application(), "", "")
	if err != nil {
		return "", 0, "", err
	}
	n.leases[id] = lease.IP
	return lease.IP, buildBridgePrefix, buildBridgeGateway, nil
}

// Launch starts the VMM inside the namespace with a TAP for the sandbox.
func (n *BuildNetwork) Launch(ctx context.Context, spec sandbox.Spec, args []string, logPath string) (int, error) {
	if err := n.ensure(ctx); err != nil {
		return 0, err
	}
	client := &netns.Client{SocketPath: n.socketPath()}
	return client.Launch(buildTapName(spec.ID), n.QEMU, args, logPath, buildTapMAC(spec.ID))
}

// Release returns a sandbox's address lease.
func (n *BuildNetwork) Release(id string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.ipam != nil {
		_ = n.ipam.Release(id)
	}
	delete(n.leases, id)
}

// Close stops the namespace supervisor.
func (n *BuildNetwork) Close() {
	n.mu.Lock()
	helper := n.helper
	n.helper = nil
	n.mu.Unlock()
	if helper != nil {
		_ = helper.Stop(2 * time.Second)
	}
}

func (n *BuildNetwork) ensure(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.helper != nil && n.helper.Alive() {
		return nil
	}
	var logFile *os.File
	if n.RuntimeDir != "" {
		logFile, _ = os.OpenFile(filepath.Join(n.RuntimeDir, "netns-"+sanitizeName(n.application())+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	}
	helper, err := netns.StartSupervisor(ctx,
		netns.Config{SocketPath: n.socketPath(), Bridge: buildBridgeName, Gateway: buildBridgeGateway, Prefix: buildBridgePrefix, Uplink: "uplink"},
		netns.SupervisorCommand{
			Pasta: n.Pasta,
			Bin:   n.NetnsBinary,
			Args: []string{
				"--socket", n.socketPath(), "--bridge", buildBridgeName, "--gateway", buildBridgeGateway,
				"--prefix", fmt.Sprint(buildBridgePrefix), "--uplink", "uplink",
			},
		},
		logFile, 10*time.Second)
	if err != nil {
		return err
	}
	n.helper = helper
	n.started = true
	return nil
}

func buildTapName(id string) string {
	sum := sha256.Sum256([]byte("build:" + id))
	return "tap" + hex.EncodeToString(sum[:4])
}

func buildTapMAC(id string) string {
	sum := sha256.Sum256([]byte("build-mac:" + id))
	return fmt.Sprintf("02:00:%02x:%02x:%02x:%02x", sum[0], sum[1], sum[2], sum[3])
}

func sanitizeName(name string) string {
	var builder strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteByte('-')
		}
	}
	if builder.Len() == 0 {
		return "build"
	}
	return builder.String()
}
