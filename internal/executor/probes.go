//go:build linux

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"grillo.local/grillo/internal/model"
	"grillo.local/grillo/internal/observe"
)

func allContainers(workload model.Workload) []model.Container {
	return append(append([]model.Container{}, workload.Template.InitContainers...), workload.Template.Containers...)
}

// probeConfig maps an IR container's probes to the observe runner configuration.
func probeConfig(container model.Container) observe.ProbeConfig {
	return observe.ProbeConfig{
		Startup:   mapProbe(container.Probes.Startup, container),
		Readiness: mapProbe(container.Probes.Readiness, container),
		Liveness:  mapProbe(container.Probes.Liveness, container),
	}
}

func mapProbe(probe *model.Probe, container model.Container) *observe.Probe {
	if probe == nil {
		return nil
	}
	out := &observe.Probe{
		Container:        container.Name,
		InitialDelay:     seconds(probe.InitialDelaySeconds),
		Period:           seconds(probe.PeriodSeconds),
		Timeout:          seconds(probe.TimeoutSeconds),
		SuccessThreshold: int(probe.SuccessThreshold),
		FailureThreshold: int(probe.FailureThreshold),
	}
	switch probe.Kind {
	case model.ProbeExec:
		if probe.Exec == nil || len(probe.Exec.Command) == 0 {
			return nil
		}
		out.Kind = observe.ProbeExec
		out.Command = probe.Exec.Command
	case model.ProbeHTTP:
		if probe.HTTP == nil {
			return nil
		}
		port, ok := resolvePort(container, probe.HTTP.Port)
		if !ok {
			return nil
		}
		scheme := probe.HTTP.Scheme
		if scheme == "" {
			scheme = "http"
		}
		host := probe.HTTP.Host
		if host == "" {
			host = "127.0.0.1"
		}
		path := probe.HTTP.Path
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		out.Kind = observe.ProbeHTTP
		out.URL = fmt.Sprintf("%s://%s%s", scheme, net.JoinHostPort(host, strconv.Itoa(port)), path)
	case model.ProbeTCP:
		if probe.TCP == nil {
			return nil
		}
		port, ok := resolvePort(container, probe.TCP.Port)
		if !ok {
			return nil
		}
		out.Kind = observe.ProbeTCP
		out.Host = probe.TCP.Host
		if out.Host == "" {
			out.Host = "127.0.0.1"
		}
		out.Port = port
	default:
		return nil
	}
	return out
}

func resolvePort(container model.Container, ref model.PortRef) (int, bool) {
	if ref.Number > 0 {
		return int(ref.Number), true
	}
	if ref.Name != "" {
		for _, port := range container.Ports {
			if port.Name == ref.Name {
				return int(port.ContainerPort), true
			}
		}
	}
	return 0, false
}

func seconds(value int32) time.Duration {
	if value <= 0 {
		return 0
	}
	return time.Duration(value) * time.Second
}
