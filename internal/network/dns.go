//go:build linux

// SPDX-License-Identifier: Apache-2.0

package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// DefaultTTL is the short TTL used for generated records so endpoint changes
// propagate quickly.
const DefaultTTL = 5

// SRVRecord is one SRV target for a named service port.
type SRVRecord struct {
	PortName string
	Target   string
	Port     uint16
	Priority uint16
	Weight   uint16
}

// ResolverConfig configures the guest DNS resolver.
type ResolverConfig struct {
	// Forwarder is an explicit IP:port upstream reachable only from this
	// private application namespace. Empty preserves REFUSED for outsiders.
	Forwarder string
	// ClusterDomain defaults to "cluster.local".
	ClusterDomain string
	// Namespaces are the namespaces this application serves (currently "default").
	Namespaces []string
	// Search domains are appended for short names during forwarding.
	Search []string
	// Ndots is the threshold for applying search domains (default 1).
	Ndots int
	// TTL for generated records (default DefaultTTL).
	TTL uint32
}

func (c ResolverConfig) withDefaults() ResolverConfig {
	out := c
	if out.ClusterDomain == "" {
		out.ClusterDomain = "cluster.local"
	}
	if len(out.Namespaces) == 0 {
		out.Namespaces = []string{"default"}
	}
	if out.Ndots == 0 {
		out.Ndots = 1
	}
	if out.TTL == 0 {
		out.TTL = DefaultTTL
	}
	return out
}

// Resolver answers DNS queries for the application's Services and rejects
// names outside its zone instead of forwarding them (never an open resolver).
type Resolver struct {
	mu    sync.RWMutex
	cfg   ResolverConfig
	a     map[string][]netip.Addr
	srv   map[string][]SRVRecord
	zones map[string]bool
}

// NewResolver returns an empty resolver.
func NewResolver(cfg ResolverConfig) *Resolver {
	cfg = cfg.withDefaults()
	r := &Resolver{
		cfg:   cfg,
		a:     map[string][]netip.Addr{},
		srv:   map[string][]SRVRecord{},
		zones: map[string]bool{strings.ToLower(cfg.ClusterDomain): true},
	}
	for _, ns := range cfg.Namespaces {
		ns = strings.ToLower(ns)
		r.zones[ns] = true
		r.zones[ns+".svc"] = true
		r.zones[ns+".svc."+strings.ToLower(cfg.ClusterDomain)] = true
	}
	return r
}

// UpsertService registers a Service under all Kubernetes-style names.
func (r *Resolver) UpsertService(name, namespace string, ips []string, srv []SRVRecord) error {
	name = normalizeDNSName(name)
	namespace = normalizeDNSName(namespace)
	if name == "" || namespace == "" {
		return fmt.Errorf("%w: service and namespace are required", ErrInvalid)
	}
	addrs := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		addr, err := netip.ParseAddr(ip)
		if err != nil {
			return fmt.Errorf("%w: service %s: %v", ErrInvalid, name, err)
		}
		if !addr.Is4() {
			return fmt.Errorf("%w: only IPv4 DNS records are implemented", ErrUnsupported)
		}
		addrs = append(addrs, addr)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, alias := range serviceAliases(name, namespace, r.cfg.ClusterDomain) {
		r.a[alias] = append([]netip.Addr(nil), addrs...)
		for key := range r.srv {
			if strings.HasSuffix(key, "."+alias) {
				delete(r.srv, key)
			}
		}
		if len(srv) > 0 {
			r.srv["_tcp."+alias] = append([]SRVRecord(nil), srv...)
			for _, record := range srv {
				if record.PortName != "" {
					key := "_" + record.PortName + "._tcp." + alias
					r.srv[key] = append(r.srv[key], record)
				}
			}
		}
	}
	return nil
}

// RemoveService deletes a Service's records.
func (r *Resolver) RemoveService(name, namespace string) {
	name = normalizeDNSName(name)
	namespace = normalizeDNSName(namespace)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, alias := range serviceAliases(name, namespace, r.cfg.ClusterDomain) {
		delete(r.a, alias)
		for key := range r.srv {
			if strings.HasSuffix(key, "."+alias) {
				delete(r.srv, key)
			}
		}
	}
}

func serviceAliases(name, namespace, clusterDomain string) []string {
	base := []string{
		name,
		name + "." + namespace,
		name + "." + namespace + ".svc",
		name + "." + namespace + ".svc." + strings.ToLower(clusterDomain),
	}
	return base
}

// LookupA returns IPv4 addresses for a name, if registered.
func (r *Resolver) LookupA(name string) ([]netip.Addr, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	addrs, ok := r.a[normalizeDNSName(name)]
	return addrs, ok
}

func (r *Resolver) inZone(name string) bool {
	parts := strings.Split(name, ".")
	for i := range parts {
		if r.zones[strings.Join(parts[i:], ".")] {
			return true
		}
	}
	return false
}

func (r *Resolver) lookupSRV(name string) ([]SRVRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	records, ok := r.srv[name]
	return records, ok
}

func (r *Resolver) ttl() uint32 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cfg.TTL
}

func (r *Resolver) searchDomains() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.cfg.Search...)
}

func (r *Resolver) ndots() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cfg.Ndots
}

// nameCandidates applies search domains for short names.
func (r *Resolver) nameCandidates(name string) []string {
	candidates := []string{name}
	if strings.Count(name, ".") < r.ndots() {
		for _, domain := range r.searchDomains() {
			candidates = append(candidates, name+"."+normalizeDNSName(domain))
		}
	}
	return candidates
}

// DNSServer serves the application zone over UDP and TCP on one port.
type DNSServer struct {
	resolver *Resolver
	udp      *net.UDPConn
	tcp      net.Listener

	closeOnce   sync.Once
	wg          sync.WaitGroup
	closed      chan struct{}
	slots       chan struct{}
	mu          sync.Mutex
	connections map[net.Conn]bool
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewDNSServer binds a resolver to a UDP and TCP socket on addr.
func NewDNSServer(addr string, resolver *Resolver) (*DNSServer, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	udp, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}
	tcp, err := net.Listen("tcp", udp.LocalAddr().String())
	if err != nil {
		udp.Close()
		return nil, err
	}
	if upstream := resolver.cfg.Forwarder; upstream != "" {
		addr, err := netip.ParseAddrPort(upstream)
		if err != nil || !addr.Addr().Is4() || addr.Port() == 0 || addr.String() == udp.LocalAddr().String() {
			_ = udp.Close()
			_ = tcp.Close()
			return nil, fmt.Errorf("invalid or looping DNS forwarder")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &DNSServer{resolver: resolver, udp: udp, tcp: tcp, closed: make(chan struct{}), slots: make(chan struct{}, 64), connections: map[net.Conn]bool{}, ctx: ctx, cancel: cancel}, nil
}

// Addr returns the bound UDP address.
func (s *DNSServer) Addr() string { return s.udp.LocalAddr().String() }

// Serve runs the UDP and TCP handlers until Close or ctx cancellation.
func (s *DNSServer) Serve(ctx context.Context) error {
	go func() {
		select {
		case <-ctx.Done():
			s.Close()
		case <-s.closed:
		}
	}()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.serveUDP()
	}()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.serveTCP()
	}()
	s.wg.Wait()
	return nil
}

// Close stops the server.
func (s *DNSServer) Close() error {
	s.closeOnce.Do(func() {
		_ = s.udp.Close()
		_ = s.tcp.Close()
		close(s.closed)
		s.cancel()
		s.mu.Lock()
		for conn := range s.connections {
			_ = conn.Close()
		}
		s.mu.Unlock()
	})
	return nil
}

func (s *DNSServer) serveUDP() {
	buf := make([]byte, 4096)
	for {
		n, addr, err := s.udp.ReadFromUDP(buf)
		if err != nil {
			return
		}
		resp, err := s.respond(buf[:n])
		if err != nil {
			continue
		}
		_, _ = s.udp.WriteToUDP(resp, addr)
	}
}

func (s *DNSServer) serveTCP() {
	for {
		conn, err := s.tcp.Accept()
		if err != nil {
			return
		}
		select {
		case s.slots <- struct{}{}:
		default:
			_ = conn.Close()
			continue
		}
		s.mu.Lock()
		select {
		case <-s.closed:
			s.mu.Unlock()
			_ = conn.Close()
			<-s.slots
			return
		default:
		}
		s.connections[conn] = true
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() { s.mu.Lock(); delete(s.connections, conn); s.mu.Unlock(); <-s.slots }()
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
			for {
				var length [2]byte
				if _, err := readFull(conn, length[:]); err != nil {
					return
				}
				size := int(length[0])<<8 | int(length[1])
				if size == 0 || size > 65535 {
					return
				}
				query := make([]byte, size)
				if _, err := readFull(conn, query); err != nil {
					return
				}
				resp, err := s.respondTransport(query, true)
				if err != nil {
					return
				}
				frame := []byte{byte(len(resp) >> 8), byte(len(resp))}
				if _, err := conn.Write(append(frame, resp...)); err != nil {
					return
				}
			}
		}()
	}
}

func readFull(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func (s *DNSServer) respond(query []byte) ([]byte, error) { return s.respondTransport(query, false) }

func (s *DNSServer) respondTransport(query []byte, tcp bool) ([]byte, error) {
	parser := dnsmessage.Parser{}
	header, err := parser.Start(query)
	if err != nil {
		return nil, err
	}
	if header.Response {
		return nil, fmt.Errorf("expected DNS query")
	}
	questions, err := parser.AllQuestions()
	if err != nil && !errors.Is(err, dnsmessage.ErrSectionDone) {
		return nil, err
	}
	rcode := dnsmessage.RCodeSuccess
	answers := make([]dnsmessage.Resource, 0, len(questions))
	for _, question := range questions {
		if question.Class != dnsmessage.ClassINET || header.OpCode != 0 {
			rcode = dnsmessage.RCodeRefused
			continue
		}
		resolved, code := s.resolver.answer(question, s.udp.LocalAddr())
		if code != dnsmessage.RCodeSuccess && rcode == dnsmessage.RCodeSuccess {
			rcode = code
		}
		answers = append(answers, resolved...)
	}
	if rcode == dnsmessage.RCodeRefused && len(questions) == 1 && questions[0].Class == dnsmessage.ClassINET && header.OpCode == 0 && strings.Contains(normalizeDNSName(questions[0].Name.String()), ".") && s.resolver.cfg.Forwarder != "" {
		response, err := s.forward(query, questions[0], header.ID, tcp)
		if err == nil {
			return response, nil
		}
		rcode = dnsmessage.RCodeServerFailure
	}
	builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{
		ID:                 header.ID,
		Response:           true,
		OpCode:             header.OpCode,
		RecursionDesired:   header.RecursionDesired,
		RecursionAvailable: true,
		RCode:              rcode,
	})
	builder.EnableCompression()
	if err := builder.StartQuestions(); err != nil {
		return nil, err
	}
	for _, question := range questions {
		if err := builder.Question(question); err != nil {
			return nil, err
		}
	}
	if err := builder.StartAnswers(); err != nil {
		return nil, err
	}
	for _, answer := range answers {
		switch body := answer.Body.(type) {
		case *dnsmessage.AResource:
			if err := builder.AResource(answer.Header, *body); err != nil {
				return nil, err
			}
		case *dnsmessage.SRVResource:
			if err := builder.SRVResource(answer.Header, *body); err != nil {
				return nil, err
			}
		}
	}
	return builder.Finish()
}

// answer resolves one question. It returns NXDOMAIN for names inside the managed
// zone that do not exist, and REFUSED for external names so the server is never
// an open resolver.
func (r *Resolver) answer(question dnsmessage.Question, _ net.Addr) ([]dnsmessage.Resource, dnsmessage.RCode) {
	name := normalizeDNSName(question.Name.String())
	for _, candidate := range r.nameCandidates(name) {
		if addrs, ok := r.LookupA(candidate); ok {
			if question.Type == dnsmessage.TypeA {
				return aResources(question.Name, addrs, r.ttl()), dnsmessage.RCodeSuccess
			}
			if question.Type != dnsmessage.TypeSRV {
				return nil, dnsmessage.RCodeSuccess // registered name, absent RR type
			}
		}
		if question.Type == dnsmessage.TypeSRV {
			if records, ok := r.lookupSRV(candidate); ok {
				return srvResources(question.Name, records, r.ttl()), dnsmessage.RCodeSuccess
			}
		}
	}
	if r.inZone(name) {
		return nil, dnsmessage.RCodeNameError
	}
	return nil, dnsmessage.RCodeRefused
}

func aResources(name dnsmessage.Name, addrs []netip.Addr, ttl uint32) []dnsmessage.Resource {
	resources := make([]dnsmessage.Resource, 0, len(addrs))
	for _, addr := range addrs {
		ip4 := addr.As4()
		resources = append(resources, dnsmessage.Resource{
			Header: dnsmessage.ResourceHeader{Name: name, Class: dnsmessage.ClassINET, TTL: ttl},
			Body:   &dnsmessage.AResource{A: ip4},
		})
	}
	return resources
}

func srvResources(name dnsmessage.Name, records []SRVRecord, ttl uint32) []dnsmessage.Resource {
	resources := make([]dnsmessage.Resource, 0, len(records))
	for _, record := range records {
		target, err := dnsmessage.NewName(ensureFQDN(record.Target))
		if err != nil {
			continue
		}
		resources = append(resources, dnsmessage.Resource{
			Header: dnsmessage.ResourceHeader{Name: name, Class: dnsmessage.ClassINET, TTL: ttl},
			Body: &dnsmessage.SRVResource{
				Priority: record.Priority,
				Weight:   record.Weight,
				Port:     record.Port,
				Target:   target,
			},
		})
	}
	return resources
}

func normalizeDNSName(name string) string {
	return strings.ToLower(strings.TrimSuffix(name, "."))
}

func ensureFQDN(name string) string {
	if strings.HasSuffix(name, ".") {
		return name
	}
	return name + "."
}
