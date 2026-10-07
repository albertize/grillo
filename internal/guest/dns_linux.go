//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/albertize/grillo/internal/guestproto"
	"github.com/albertize/grillo/internal/network"
)

// resolvConfPath is the guest resolver configuration consumed by containers.
const resolvConfPath = "/etc/resolv.conf"

// startDNS serves the sandbox's service records on 127.0.0.1:53 and points the
// guest resolver at it. Containers share the guest network namespace, so they
// reach the resolver at localhost.
func (a *Agent) startDNS(cfg *guestproto.DNSConfig) error {
	if cfg == nil {
		return nil
	}
	if cfg.Server != "" {
		return writeResolver(cfg.Server, cfg.Search)
	}
	resolver := network.NewResolver(network.ResolverConfig{ClusterDomain: cfg.ClusterDomain, Search: cfg.Search})
	for _, record := range cfg.Records {
		if err := resolver.UpsertService(record.Name, record.Namespace, record.IPs, nil); err != nil {
			return fmt.Errorf("guest: dns record %s/%s: %w", record.Namespace, record.Name, err)
		}
	}
	server, err := network.NewDNSServer("127.0.0.1:53", resolver)
	if err != nil {
		return fmt.Errorf("guest: dns server: %w", err)
	}
	go func() { _ = server.Serve(context.Background()) }()

	a.dnsMu.Lock()
	if a.dns != nil {
		_ = a.dns.Close()
	}
	a.dns = server
	a.dnsMu.Unlock()
	return writeResolvConf(cfg.Search)
}

func (a *Agent) stopDNS() {
	a.dnsMu.Lock()
	defer a.dnsMu.Unlock()
	if a.dns != nil {
		_ = a.dns.Close()
		a.dns = nil
	}
}

// writeNameservers points the guest resolver at the given upstream servers.
// Build sandboxes use this so RUN steps can reach package repositories.
func writeNameservers(nameservers []string) error {
	if len(nameservers) == 0 {
		return nil
	}
	var builder strings.Builder
	for _, server := range nameservers {
		builder.WriteString("nameserver " + server + "\n")
	}
	builder.WriteString("options ndots:1\n")
	if err := os.MkdirAll("/etc", 0o755); err != nil {
		return err
	}
	return os.WriteFile(resolvConfPath, []byte(builder.String()), 0o644)
}

func writeResolvConf(search []string) error { return writeResolver("127.0.0.1", search) }

func resolverConfig(server string, search []string) string {
	var builder strings.Builder
	builder.WriteString("nameserver " + server + "\n")
	if len(search) > 0 {
		builder.WriteString("search " + strings.Join(search, " ") + "\n")
	}
	builder.WriteString("options ndots:5\n")
	return builder.String()
}

func writeResolver(server string, search []string) error {
	if err := os.MkdirAll("/etc", 0o755); err != nil {
		return err
	}
	return os.WriteFile(resolvConfPath, []byte(resolverConfig(server, search)), 0o644)
}

func runNetworkCommand(ctx context.Context, cmd *exec.Cmd, reaper *Reaper) error {
	if reaper != nil {
		status, _, _, err := (&Runc{Reaper: reaper}).capture(ctx, cmd)
		if err != nil {
			return err
		}
		if status.ExitCode != 0 {
			return fmt.Errorf("network helper exited %d", status.ExitCode)
		}
		return nil
	}
	// Standalone callers have no PID 1 reaper; use the bounded context.
	// exec.CommandContext sets the cancellation watcher; reconstruct argv.
	bounded := exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
	if err := bounded.Run(); err != nil {
		return err
	}
	return nil
}

// SetupNetwork configures the sandbox interface from the spec. It uses busybox
// `ip`, which the guest image provides.
func SetupNetwork(cfg *guestproto.NetworkConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return setupNetwork(ctx, cfg, nil)
}

func setupNetwork(ctx context.Context, cfg *guestproto.NetworkConfig, reaper *Reaper) error {
	if cfg == nil {
		return nil
	}
	iface := cfg.Interface
	if iface == "" {
		iface = "eth0"
	}
	commands := [][]string{
		{"link", "set", iface, "up"},
		{"addr", "add", fmt.Sprintf("%s/%d", cfg.Address, cfg.PrefixLen), "dev", iface},
	}
	if cfg.Gateway != "" {
		commands = append(commands, []string{"route", "add", "default", "via", cfg.Gateway, "dev", iface})
	}
	for _, args := range commands {
		cmd := exec.Command("/bin/busybox", append([]string{"ip"}, args...)...)
		if err := runNetworkCommand(ctx, cmd, reaper); err != nil {
			return fmt.Errorf("guest: configure network %s: %w", strings.Join(args, " "), err)
		}
	}
	return nil
}
