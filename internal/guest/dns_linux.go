//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"context"
	"fmt"
	"os"
	"strings"

	"grillo.local/grillo/internal/guestproto"
	"grillo.local/grillo/internal/network"
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

func writeResolvConf(search []string) error {
	var builder strings.Builder
	builder.WriteString("nameserver 127.0.0.1\n")
	if len(search) > 0 {
		builder.WriteString("search " + strings.Join(search, " ") + "\n")
	}
	builder.WriteString("options ndots:5\n")
	if err := os.MkdirAll("/etc", 0o755); err != nil {
		return err
	}
	return os.WriteFile(resolvConfPath, []byte(builder.String()), 0o644)
}
