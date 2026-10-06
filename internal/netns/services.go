//go:build linux

// SPDX-License-Identifier: Apache-2.0

package netns

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"net/netip"
	"path/filepath"
	"sort"
	"strconv"
	"sync"

	"github.com/albertize/grillo/internal/network"
)

const ServiceSubnet = "10.78.0.0/24"
const serviceGateway = "10.78.0.1"

// ServiceUpdate is an entire application Service snapshot. Endpoint ports are
// keyed by the Service port name (or its numeric spelling), not the target name.
type ServiceUpdate struct {
	Service         network.Service    `json:"service"`
	Headless        bool               `json:"headless,omitempty"`
	Private         bool               `json:"private,omitempty"` // relay target only; no Service DNS
	PublishNotReady bool               `json:"publishNotReady,omitempty"`
	Endpoints       []network.Endpoint `json:"endpoints,omitempty"`
}

type serviceDatapath struct {
	mu         sync.Mutex
	registry   *network.ServiceRegistry
	resolver   *network.Resolver
	proxies    map[string]*network.ServiceProxy
	services   map[string]ServiceUpdate
	addresses  map[string]string
	podAliases map[string][]string
	bridge     string
	podSubnet  netip.Prefix
	command    func(context.Context, ...string) error
	slots      chan struct{}
}

func newServiceDatapath(cfg Config, resolver *network.Resolver) (*serviceDatapath, error) {
	registry, err := network.OpenServiceRegistry(filepath.Join(filepath.Dir(cfg.SocketPath), "services-"+filepath.Base(cfg.SocketPath)), ServiceSubnet, serviceGateway)
	if err != nil {
		return nil, err
	}
	prefix, err := netip.ParsePrefix(fmt.Sprintf("%s/%d", cfg.Gateway, cfg.Prefix))
	if err != nil {
		return nil, err
	}
	return &serviceDatapath{registry: registry, resolver: resolver, proxies: map[string]*network.ServiceProxy{}, services: map[string]ServiceUpdate{}, addresses: map[string]string{}, podAliases: map[string][]string{}, bridge: cfg.Bridge, podSubnet: prefix.Masked(), slots: make(chan struct{}, 128), command: func(ctx context.Context, args ...string) error { return run(ctx, "ip", args...) }}, nil
}

func portKey(p network.ServicePort) string {
	if p.Name != "" {
		return p.Name
	}
	return strconv.Itoa(p.Port)
}

func validateServiceUpdates(updates []ServiceUpdate, podSubnet netip.Prefix) error {
	if len(updates) > 200 {
		return fmt.Errorf("too many Services")
	}
	seen := map[string]bool{}
	totalPorts, totalEndpoints := 0, 0
	for _, update := range updates {
		service := update.Service
		if service.Namespace != "default" || !dnsLabel(service.Name) || seen[service.Name] {
			return fmt.Errorf("invalid or duplicate Service")
		}
		seen[service.Name] = true
		if len(service.Ports) > 32 || len(update.Endpoints) > 256 {
			return fmt.Errorf("Service limit exceeded")
		}
		totalPorts += len(service.Ports)
		totalEndpoints += len(update.Endpoints)
		if totalPorts > 256 || totalEndpoints > 2048 {
			return fmt.Errorf("application network limit exceeded")
		}
		ports := map[int]bool{}
		names := map[string]bool{}
		for _, port := range service.Ports {
			if port.Port < 1 || port.Port > 65535 || ports[port.Port] || names[portKey(port)] || (port.Protocol != "" && port.Protocol != "tcp") {
				return fmt.Errorf("invalid, duplicate or non-TCP Service port")
			}
			ports[port.Port] = true
			names[portKey(port)] = true
		}
		for _, endpoint := range update.Endpoints {
			ip, err := netip.ParseAddr(endpoint.IP)
			if err != nil || !ip.Is4() || !podSubnet.Contains(ip) || ip == podSubnet.Addr() || ip == podSubnet.Addr().Next() {
				return fmt.Errorf("endpoint outside Pod subnet")
			}
			if len(endpoint.Ports) > 32 {
				return fmt.Errorf("endpoint port limit exceeded")
			}
			for name, port := range endpoint.Ports {
				if !names[name] || port < 1 || port > 65535 {
					return fmt.Errorf("invalid endpoint port")
				}
			}
		}
	}
	return nil
}

func dnsLabel(name string) bool {
	if len(name) == 0 || len(name) > 63 || name[0] == '-' || name[len(name)-1] == '-' {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// Update changes listeners only when Service ports change. Endpoint-only
// updates replace the balancer snapshot without disrupting existing streams.
func (d *serviceDatapath) Update(ctx context.Context, updates []ServiceUpdate) (map[string]string, error) {
	if err := validateServiceUpdates(updates, d.podSubnet); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	desired := map[string]bool{}
	wanted := map[string]bool{}
	result := map[string]string{}
	sort.Slice(updates, func(i, j int) bool { return updates[i].Service.Name < updates[j].Service.Name })
	for _, update := range updates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		service := update.Service
		desired[service.Name] = true
		vip, err := d.registry.Upsert(service)
		if err != nil {
			return nil, err
		}
		result[service.Name] = vip
		if d.addresses[service.Name] == "" {
			if err := d.command(ctx, "addr", "add", vip+"/32", "dev", d.bridge); err != nil {
				return nil, err
			}
			d.addresses[service.Name] = vip
		}
		endpoints := make([]network.Endpoint, len(update.Endpoints))
		copy(endpoints, update.Endpoints)
		var ips []string
		for i := range endpoints {
			if update.PublishNotReady {
				endpoints[i].Ready = true
			}
			if endpoints[i].Ready {
				ips = append(ips, endpoints[i].IP)
			}
		}
		d.registry.SetEndpoints(service.Namespace, service.Name, endpoints)
		for _, alias := range d.podAliases[service.Name] {
			d.resolver.RemoveService(alias, service.Namespace)
		}
		delete(d.podAliases, service.Name)
		var records []network.SRVRecord
		if update.Headless && !update.Private {
			for _, endpoint := range endpoints {
				if !endpoint.Ready {
					continue
				}
				sum := sha256.Sum256([]byte(endpoint.IP))
				alias := fmt.Sprintf("pod-%x.%s", sum[:8], service.Name)
				if err := d.resolver.UpsertService(alias, service.Namespace, []string{endpoint.IP}, nil); err != nil {
					return nil, err
				}
				d.podAliases[service.Name] = append(d.podAliases[service.Name], alias)
				for _, port := range service.Ports {
					if port.Name != "" {
						target := endpoint.Ports[portKey(port)]
						if target > 0 {
							records = append(records, network.SRVRecord{PortName: port.Name, Target: alias + ".default.svc.cluster.local", Port: uint16(target)})
						}
					}
				}
			}
		}
		if !update.Headless {
			ips = []string{vip}
		}
		for _, port := range service.Ports {
			if port.Name != "" && !update.Headless {
				records = append(records, network.SRVRecord{PortName: port.Name, Target: service.Name + ".default.svc.cluster.local", Port: uint16(port.Port)})
			}
			addr := net.JoinHostPort(vip, strconv.Itoa(port.Port))
			wanted[addr] = true
			if d.proxies[addr] == nil {
				proxy, err := network.NewServiceProxy(addr, network.NewBalancer(d.registry, service.Namespace, service.Name, portKey(port)), network.WithServiceConnectionPool(d.slots))
				if err != nil {
					return nil, err
				}
				d.proxies[addr] = proxy
			}
		}
		if !update.Private {
			if err := d.resolver.UpsertService(service.Name, service.Namespace, ips, records); err != nil {
				return nil, err
			}
		} else {
			d.resolver.RemoveService(service.Name, service.Namespace)
		}
		d.services[service.Name] = update
	}
	for addr, proxy := range d.proxies {
		if !wanted[addr] {
			_ = proxy.Close()
			delete(d.proxies, addr)
		}
	}
	for name, previous := range d.services {
		if desired[name] {
			continue
		}
		d.resolver.RemoveService(name, previous.Service.Namespace)
		for _, alias := range d.podAliases[name] {
			d.resolver.RemoveService(alias, previous.Service.Namespace)
		}
		delete(d.podAliases, name)
		if vip := d.addresses[name]; vip != "" {
			if err := d.command(ctx, "addr", "del", vip+"/32", "dev", d.bridge); err != nil {
				return nil, err
			}
			delete(d.addresses, name)
		}
		if err := d.registry.Remove(previous.Service.Namespace, name); err != nil {
			return nil, err
		}
		delete(d.services, name)
	}
	return result, nil
}

// Dial exposes only declared Service VIP/ports over the private Unix relay.
// There is intentionally no arbitrary namespace/host address dial operation.
func (d *serviceDatapath) Dial(ctx context.Context, address string) (net.Conn, error) {
	d.mu.Lock()
	allowed := d.proxies[address] != nil
	d.mu.Unlock()
	if !allowed {
		return nil, fmt.Errorf("target is not a declared Service port")
	}
	return (&net.Dialer{}).DialContext(ctx, "tcp", address)
}

func (d *serviceDatapath) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for addr, proxy := range d.proxies {
		_ = proxy.Close()
		delete(d.proxies, addr)
	}
}

// ServiceAddress resolves a numeric or named declared port in a snapshot.
func ServiceAddress(service network.Service, vip, port string) (string, bool) {
	for _, p := range service.Ports {
		if port == strconv.Itoa(p.Port) || port == p.Name {
			return net.JoinHostPort(vip, strconv.Itoa(p.Port)), true
		}
	}
	return "", false
}
