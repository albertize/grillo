//go:build linux

// SPDX-License-Identifier: Apache-2.0

package observe

import (
	"context"
	"grillo.local/grillo/internal/guestproto"
)

// GuestProbeClient is the subset of the guest protocol client used for probes.
type GuestProbeClient interface {
	Probe(ctx context.Context, req guestproto.ProbeRequest) (guestproto.ProbeResult, error)
}

// GuestProber runs probes inside the guest, in the correct container network
// context, by delegating to the agent over the control channel.
type GuestProber struct {
	Client GuestProbeClient
}

// Probe implements Prober.
func (g GuestProber) Probe(ctx context.Context, probe Probe) (bool, string) {
	if g.Client == nil {
		return false, "no guest client"
	}
	req := guestproto.ProbeRequest{
		Kind:      guestproto.ProbeKind(probe.Kind),
		Container: probe.Container,
		Command:   probe.Command,
		URL:       probe.URL,
		Host:      probe.Host,
		Port:      probe.Port,
		TimeoutMS: probe.timeout().Milliseconds(),
	}
	result, err := g.Client.Probe(ctx, req)
	if err != nil {
		return false, err.Error()
	}
	if result.Detail == "" && !result.Healthy {
		return false, "unhealthy"
	}
	return result.Healthy, result.Detail
}

var _ Prober = GuestProber{}
