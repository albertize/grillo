//go:build linux

// SPDX-License-Identifier: Apache-2.0

package network

import (
	"fmt"
	"io"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ServicePort is one declared service port.
type ServicePort struct {
	Name     string
	Port     int
	Protocol string // "tcp" only; a TCP proxy cannot serve UDP
}

// Service is a logical service with a stable VIP.
type Service struct {
	Name      string
	Namespace string
	Ports     []ServicePort
}

// Endpoint is one ready target of a Service.
type Endpoint struct {
	IP    string
	Ready bool
	Ports map[string]int // named port -> resolved target port
}

// Target returns the ip:port for a named port ("http") or a numeric string.
func (e Endpoint) Target(portName string) string {
	if len(e.Ports) == 0 {
		return ""
	}
	if portName == "" {
		// Pick a deterministic port.
		names := make([]string, 0, len(e.Ports))
		for name := range e.Ports {
			names = append(names, name)
		}
		sort.Strings(names)
		return net.JoinHostPort(e.IP, fmt.Sprintf("%d", e.Ports[names[0]]))
	}
	if port, ok := e.Ports[portName]; ok {
		return net.JoinHostPort(e.IP, fmt.Sprintf("%d", port))
	}
	if _, err := fmt.Sscanf(portName, "%d", new(int)); err == nil {
		return net.JoinHostPort(e.IP, portName)
	}
	return ""
}

// ServiceRegistry tracks Services, their VIPs, and their endpoints.
type ServiceRegistry struct {
	mu        sync.RWMutex
	services  map[string]Service
	endpoints map[string][]Endpoint
	vips      map[string]string
	vipPool   *IPAM
}

// OpenServiceRegistry creates a registry with a dedicated VIP pool. VIPs are
// allocated separately from sandbox addresses.
func OpenServiceRegistry(vipDir, vipSubnet, vipGateway string) (*ServiceRegistry, error) {
	pool, err := OpenIPAM(vipDir, vipSubnet, vipGateway)
	if err != nil {
		return nil, err
	}
	return &ServiceRegistry{
		services:  map[string]Service{},
		endpoints: map[string][]Endpoint{},
		vips:      map[string]string{},
		vipPool:   pool,
	}, nil
}

// Upsert adds or replaces a Service and allocates its VIP once.
func (r *ServiceRegistry) Upsert(service Service) (string, error) {
	if service.Name == "" || service.Namespace == "" {
		return "", fmt.Errorf("%w: service and namespace are required", ErrInvalid)
	}
	for _, port := range service.Ports {
		if port.Protocol != "" && port.Protocol != "tcp" {
			return "", fmt.Errorf("%w: UDP services are not supported by the TCP proxy", ErrUnsupported)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := serviceKey(service.Namespace, service.Name)
	r.services[key] = service
	if vip, ok := r.vips[key]; ok {
		return vip, nil
	}
	lease, err := r.vipPool.Allocate(key, "service", "", "")
	if err != nil {
		return "", err
	}
	r.vips[key] = lease.IP
	return lease.IP, nil
}

// VIP returns a Service's virtual IP.
func (r *ServiceRegistry) VIP(namespace, name string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	vip, ok := r.vips[serviceKey(namespace, name)]
	return vip, ok
}

// SetEndpoints replaces a Service's endpoints.
func (r *ServiceRegistry) SetEndpoints(namespace, name string, endpoints []Endpoint) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := serviceKey(namespace, name)
	cp := make([]Endpoint, len(endpoints))
	copy(cp, endpoints)
	r.endpoints[key] = cp
}

// Endpoints returns all endpoints, ready or not.
func (r *ServiceRegistry) Endpoints(namespace, name string) []Endpoint {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Endpoint(nil), r.endpoints[serviceKey(namespace, name)]...)
}

// ReadyEndpoints returns only ready endpoints.
func (r *ServiceRegistry) ReadyEndpoints(namespace, name string) []Endpoint {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var ready []Endpoint
	for _, ep := range r.endpoints[serviceKey(namespace, name)] {
		if ep.Ready {
			ready = append(ready, ep)
		}
	}
	return ready
}

func serviceKey(namespace, name string) string { return namespace + "/" + name }

// Balancer round-robins TCP connections across a Service's ready endpoints.
type Balancer struct {
	registry  *ServiceRegistry
	namespace string
	name      string
	portName  string
	counter   atomic.Uint64
}

// NewBalancer returns a round-robin balancer for a Service port.
func NewBalancer(registry *ServiceRegistry, namespace, name, portName string) *Balancer {
	return &Balancer{registry: registry, namespace: namespace, name: name, portName: portName}
}

// Next returns the next ready target, skipping endpoints without the port.
func (b *Balancer) Next() (string, bool) {
	endpoints := b.registry.ReadyEndpoints(b.namespace, b.name)
	if len(endpoints) == 0 {
		return "", false
	}
	start := int(b.counter.Add(1)-1) % len(endpoints)
	for i := 0; i < len(endpoints); i++ {
		endpoint := endpoints[(start+i)%len(endpoints)]
		if target := endpoint.Target(b.portName); target != "" {
			return target, true
		}
	}
	return "", false
}

// ServiceProxy forwards TCP connections to a Service's ready endpoints. It
// balances round-robin, retries the next endpoint on a dial failure, and drains
// in-flight connections on Close.
type ServiceProxy struct {
	listener  net.Listener
	balancer  *Balancer
	closed    chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// NewServiceProxy listens on listenAddr and forwards to the balancer.
func NewServiceProxy(listenAddr string, balancer *Balancer) (*ServiceProxy, error) {
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, err
	}
	p := &ServiceProxy{listener: listener, balancer: balancer, closed: make(chan struct{})}
	go p.serve()
	return p, nil
}

// Addr returns the bound address.
func (p *ServiceProxy) Addr() string { return p.listener.Addr().String() }

// Close stops accepting and waits for in-flight connections to drain.
func (p *ServiceProxy) Close() error {
	var err error
	p.closeOnce.Do(func() {
		close(p.closed)
		err = p.listener.Close()
	})
	p.wg.Wait()
	return err
}

func (p *ServiceProxy) serve() {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			return
		}
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.handle(conn)
		}()
	}
}

func (p *ServiceProxy) handle(client net.Conn) {
	defer client.Close()
	upstream := p.dialUpstream()
	if upstream == nil {
		return
	}
	defer upstream.Close()
	done := make(chan struct{}, 2)
	copyConn := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if tcp, ok := dst.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		done <- struct{}{}
	}
	go copyConn(upstream, client)
	go copyConn(client, upstream)
	select {
	case <-done:
	case <-p.closed:
		_ = client.SetDeadline(time.Now())
		_ = upstream.SetDeadline(time.Now())
	}
}

func (p *ServiceProxy) dialUpstream() net.Conn {
	dialer := net.Dialer{Timeout: 5 * time.Second}
	// Try as many endpoints as the balancer reports before giving up.
	for attempt := 0; attempt < 8; attempt++ {
		target, ok := p.balancer.Next()
		if !ok {
			return nil
		}
		conn, err := dialer.Dial("tcp", target)
		if err == nil {
			return conn
		}
	}
	return nil
}
