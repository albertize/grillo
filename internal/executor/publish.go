//go:build linux

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/netns"
	"github.com/albertize/grillo/internal/network"
	"path/filepath"
)

type publishedPort struct {
	port           int
	owner, service string
	target         int
	proxy          *network.ServiceProxy
}

type publication struct {
	port, target             int
	owner, service, workload string
}

func publications(app model.Application) ([]publication, error) {
	var result []publication
	seen := map[int]bool{}
	for _, workload := range app.Workloads {
		for _, container := range workload.Template.Containers {
			for _, port := range container.Ports {
				if port.HostPort == 0 {
					continue
				}
				if port.HostPort < 1024 || port.HostPort > 65535 {
					return nil, fmt.Errorf("executor: host port %d is unavailable; use an unprivileged port (for example %d)", port.HostPort, int(port.HostPort)+10000)
				}
				if port.Protocol != "" && port.Protocol != "TCP" && port.Protocol != "tcp" {
					return nil, fmt.Errorf("executor: only TCP publishing is supported")
				}
				if workload.Replicas > 1 {
					return nil, fmt.Errorf("executor: fixed host ports cannot be published by multiple replicas")
				}
				if workload.Replicas <= 0 {
					continue
				}
				if seen[int(port.HostPort)] {
					return nil, fmt.Errorf("executor: duplicate host port")
				}
				seen[int(port.HostPort)] = true
				owner := fmt.Sprintf("%s/%s/%s/%d", app.Identity.Name, workload.ID, container.Name, port.ContainerPort)
				sum := sha256.Sum256([]byte(owner))
				service := fmt.Sprintf("pub-%x", sum[:12])
				result = append(result, publication{port: int(port.HostPort), target: int(port.ContainerPort), owner: owner, service: service, workload: workload.ID})
			}
		}
	}
	return result, nil
}

// updatePublicationsLocked runs before boot as well as after endpoint changes.
// Actual listening sockets, not a probe-close window, decide ownership.
func (e *Executor) updatePublicationsLocked(app model.Application, publications []publication, client *netns.Client) error {
	if e.ports == nil {
		opened, err := network.OpenPorts(filepath.Join(e.cfg.RuntimeDir, "ports"))
		if err != nil {
			return err
		}
		e.ports = opened
	}
	existing := e.published[app.Identity.Name]
	if existing == nil {
		existing = map[int]*publishedPort{}
		e.published[app.Identity.Name] = existing
	}
	wanted := map[int]bool{}
	for _, publication := range publications {
		wanted[publication.port] = true
		if current := existing[publication.port]; current != nil {
			if current.owner != publication.owner {
				return fmt.Errorf("executor: host port belongs to another target")
			}
			continue
		}
		if _, err := e.ports.Reserve("127.0.0.1", publication.port, publication.owner, app.Identity.Name, publication.target, "tcp"); err != nil {
			return err
		}
		proxy, err := network.NewServiceProxy(net.JoinHostPort("127.0.0.1", strconv.Itoa(publication.port)), nil, network.WithServiceConnectionPool(e.ingressSlots), network.WithServiceDial(func(ctx context.Context) (net.Conn, error) {
			e.mu.Lock()
			snapshot := e.networks[app.Identity.Name]
			vip := snapshot.vips[publication.service]
			e.mu.Unlock()
			if vip == "" {
				return nil, fmt.Errorf("published target unavailable")
			}
			return client.DialService(ctx, net.JoinHostPort(vip, strconv.Itoa(publication.target)))
		}))
		if err != nil {
			return errors.Join(err, e.ports.Release("127.0.0.1", publication.port))
		}
		existing[publication.port] = &publishedPort{port: publication.port, owner: publication.owner, service: publication.service, target: publication.target, proxy: proxy}
	}
	for port, current := range existing {
		if wanted[port] {
			continue
		}
		_ = current.proxy.Close()
		if err := e.ports.Release("127.0.0.1", port); err != nil {
			return err
		}
		delete(existing, port)
	}
	return nil
}
