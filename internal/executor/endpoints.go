//go:build linux

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/netns"
	"github.com/albertize/grillo/internal/network"
	"github.com/albertize/grillo/internal/plan"
)

type networkSnapshot struct {
	services []netns.ServiceUpdate
	vips     map[string]string
}

type ingressServer struct {
	listener net.Listener
	server   *http.Server
	proxy    atomic.Pointer[network.IngressProxy]
	host     string
	done     chan struct{}
	paths    []network.IngressPath
}

type ingressListener struct {
	net.Listener
	pool chan struct{}
}
type ingressConn struct {
	net.Conn
	once sync.Once
	pool chan struct{}
}

func (c *ingressConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { <-c.pool })
	return err
}
func (l *ingressListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		select {
		case l.pool <- struct{}{}:
			return &ingressConn{Conn: conn, pool: l.pool}, nil
		default:
			_ = conn.Close()
		}
	}
}

func newIngressServer(host string, pool chan struct{}) (*ingressServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	server := &ingressServer{listener: listener, host: host, done: make(chan struct{})}
	server.server = &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy := server.proxy.Load()
		if proxy == nil {
			http.Error(w, "service unavailable", 503)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
		// Each logical hostname has its own reported localhost port. A browser
		// can use that fallback without installing DNS or modifying /etc/hosts.
		if r.Host == listener.Addr().String() {
			clone := r.Clone(r.Context())
			clone.Host = server.host
			r = clone
		}
		proxy.ServeHTTP(w, r)
	})}
	go func() {
		defer close(server.done)
		_ = server.server.Serve(&ingressListener{Listener: listener, pool: pool})
	}()
	return server, nil
}
func (s *ingressServer) Close() {
	_ = s.server.Close()
	<-s.done
	if proxy := s.proxy.Load(); proxy != nil {
		proxy.CloseIdleConnections()
	}
}

// UpdateEndpoints publishes current observed endpoints and configures host
// Ingress. The same method is used by plan actions and health transitions.
func (e *Executor) UpdateEndpoints(ctx context.Context, application string) error {
	if !e.cfg.EnableNetwork {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	e.networkMu.Lock()
	defer e.networkMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	app, ok := e.desired[application]
	publications, publicationErr := publications(app)
	if publicationErr != nil {
		e.mu.Unlock()
		return publicationErr
	}
	var updates []netns.ServiceUpdate
	if ok {
		for _, service := range app.Services {
			update := netns.ServiceUpdate{Service: network.Service{Name: service.Name, Namespace: app.Identity.Namespace}, Headless: service.Headless, PublishNotReady: service.PublishNotReadyAddresses}
			for _, port := range service.Ports {
				update.Service.Ports = append(update.Service.Ports, network.ServicePort{Name: port.Name, Port: int(port.Port), Protocol: strings.ToLower(port.Protocol)})
			}
			for _, rt := range e.runtimes {
				if rt.app != application || rt.draining {
					continue
				}
				workload := findWorkload(app, rt.workload)
				if workload == nil || len(service.Selector) == 0 || !selectorMatches(service.Selector, workload.Labels) {
					continue
				}
				ports := map[string]int{}
				for _, port := range service.Ports {
					ref := model.PortRef{Number: port.Port}
					if port.TargetPort != nil {
						ref = *port.TargetPort
					}
					target := int(ref.Number)
					if target == 0 {
						for _, container := range workload.Template.Containers {
							if found, ok := resolvePort(container, ref); ok {
								target = found
								break
							}
						}
					}
					if target > 0 {
						key := port.Name
						if key == "" {
							key = strconv.Itoa(int(port.Port))
						}
						ports[key] = target
					}
				}
				update.Endpoints = append(update.Endpoints, network.Endpoint{IP: rt.ip, Ready: rt.ready, Ports: ports})
			}
			sort.Slice(update.Endpoints, func(i, j int) bool { return update.Endpoints[i].IP < update.Endpoints[j].IP })
			updates = append(updates, update)
		}
	}
	for _, publication := range publications {
		update := netns.ServiceUpdate{Service: network.Service{Name: publication.service, Namespace: app.Identity.Namespace, Ports: []network.ServicePort{{Name: "port", Port: publication.target, Protocol: "tcp"}}}, Private: true}
		for _, rt := range e.runtimes {
			if rt.app == application && rt.workload == publication.workload && !rt.draining {
				update.Endpoints = append(update.Endpoints, network.Endpoint{IP: rt.ip, Ready: rt.ready, Ports: map[string]int{"port": publication.target}})
			}
		}
		updates = append(updates, update)
	}
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("executor: no desired application")
	}
	if _, err := e.ensureSupervisor(ctx, application); err != nil {
		return err
	}
	client := &netns.Client{SocketPath: e.socketPath(application)}
	vips, err := client.UpdateServices(ctx, updates)
	if err == nil {
		e.mu.Lock()
		e.networks[application] = networkSnapshot{services: updates, vips: vips}
		e.mu.Unlock()
		err = e.updatePublicationsLocked(app, publications, client)
		if err == nil {
			err = e.updateIngressLocked(application, app, client)
		}
	}
	e.mu.Lock()
	e.networkErrors[application] = err
	e.mu.Unlock()
	return err
}

func (e *Executor) updateIngressLocked(application string, app model.Application, client *netns.Client) error {
	groups := map[string][]network.IngressPath{}
	for _, route := range app.Routes {
		port := 0
		for _, service := range app.Services {
			if service.Name != route.Service {
				continue
			}
			for _, p := range service.Ports {
				if route.Port != nil && (route.Port.Number == p.Port || route.Port.Name != "" && route.Port.Name == p.Name) {
					port = int(p.Port)
				}
			}
		}
		if port == 0 {
			return fmt.Errorf("executor: route has no declared Service port")
		}
		groups[route.Hostname] = append(groups[route.Hostname], network.IngressPath{Path: route.Path, PathType: network.PathType(route.PathType), Service: route.Service, Port: port})
	}
	servers := e.ingresses[application]
	if servers == nil {
		servers = map[string]*ingressServer{}
		e.ingresses[application] = servers
	}
	for host, paths := range groups {
		server := servers[host]
		if server == nil {
			created, err := newIngressServer(host, e.ingressSlots)
			if err != nil {
				return err
			}
			server = created
			servers[host] = server
		}
		if slices.Equal(server.paths, paths) {
			continue
		}
		server.paths = append([]network.IngressPath(nil), paths...)
		proxy := network.NewIngressProxy(network.NewIngress([]network.IngressRule{{Host: host, Paths: paths}}), func(service string, port int) (string, bool) {
			e.mu.Lock()
			snapshot := e.networks[application]
			e.mu.Unlock()
			for _, update := range snapshot.services {
				if update.Service.Name != service {
					continue
				}
				ready := false
				for _, endpoint := range update.Endpoints {
					if endpoint.Ready || update.PublishNotReady {
						ready = true
						break
					}
				}
				if !ready {
					return "", false
				}
				return netns.ServiceAddress(update.Service, snapshot.vips[service], strconv.Itoa(port))
			}
			return "", false
		}, network.WithIngressDialContext(func(ctx context.Context, _, address string) (net.Conn, error) {
			return client.DialService(ctx, address)
		}))
		old := server.proxy.Swap(proxy)
		if old != nil {
			old.CloseIdleConnections()
		}
	}
	for host, server := range servers {
		if _, ok := groups[host]; !ok {
			server.Close()
			delete(servers, host)
		}
	}
	e.mu.Lock()
	desired := e.desired[application]
	desired.Routes = append([]model.Route(nil), desired.Routes...)
	for i := range desired.Routes {
		if server := servers[desired.Routes[i].Hostname]; server != nil {
			desired.Routes[i].Endpoint = "http://" + server.listener.Addr().String()
		}
	}
	e.desired[application] = desired
	e.mu.Unlock()
	return nil
}

// Routes returns the actual published loopback endpoints, never private data.
func (e *Executor) Routes(application string) []model.Route {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]model.Route(nil), e.desired[application].Routes...)
}

func (e *Executor) closeIngress(application string) error {
	e.networkMu.Lock()
	defer e.networkMu.Unlock()
	for _, server := range e.ingresses[application] {
		server.Close()
	}
	delete(e.ingresses, application)
	for port, binding := range e.published[application] {
		_ = binding.proxy.Close()
		if err := e.ports.Release("127.0.0.1", port); err != nil {
			return err
		}
		delete(e.published[application], port)
	}
	delete(e.published, application)
	e.mu.Lock()
	delete(e.networks, application)
	delete(e.networkErrors, application)
	app := e.desired[application]
	app.Routes = append([]model.Route(nil), app.Routes...)
	for i := range app.Routes {
		app.Routes[i].Endpoint = ""
	}
	e.desired[application] = app
	e.mu.Unlock()
	return nil
}

func (e *Executor) drainEndpoints(ctx context.Context, application string, descriptor plan.Descriptor) error {
	e.mu.Lock()
	rt := e.runtimes[application+"-"+descriptor.ID]
	if rt != nil {
		rt.draining = true
	}
	e.mu.Unlock()
	if rt == nil {
		return nil
	}
	return e.UpdateEndpoints(ctx, application)
}
