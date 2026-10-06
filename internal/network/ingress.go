//go:build linux

// SPDX-License-Identifier: Apache-2.0

package network

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"
)

// PathType is the Ingress path match type.
type PathType string

const (
	// PathExact matches the request path exactly.
	PathExact PathType = "Exact"
	// PathPrefix matches on path segments, not raw string prefixes.
	PathPrefix PathType = "Prefix"
)

// IngressPath is one path rule.
type IngressPath struct {
	Path     string
	PathType PathType
	Service  string
	Port     int
}

// IngressRule maps a host to paths.
type IngressRule struct {
	Host  string
	Paths []IngressPath
}

// Ingress matches requests to Service backends.
type Ingress struct {
	rules []IngressRule
}

// NewIngress builds an Ingress from rules.
func NewIngress(rules []IngressRule) *Ingress {
	return &Ingress{rules: rules}
}

// Match returns the most specific path rule for a host and request path. Exact
// matches win over Prefix matches, and the longest Prefix wins.
func (in *Ingress) Match(host, requestPath string) (IngressPath, bool) {
	host = strings.ToLower(hostOnly(host))
	var best IngressPath
	found := false
	for _, rule := range in.rules {
		if rule.Host != "" && strings.ToLower(rule.Host) != host {
			continue
		}
		for _, path := range rule.Paths {
			matched := false
			switch path.PathType {
			case PathExact:
				matched = requestPath == path.Path
			default: // PathPrefix
				matched = prefixMatches(path.Path, requestPath)
			}
			if !matched {
				continue
			}
			if !found || betterMatch(path, best) {
				best = path
				found = true
			}
		}
	}
	return best, found
}

func betterMatch(candidate, current IngressPath) bool {
	// Exact beats Prefix; otherwise the longer path wins.
	if candidate.PathType == PathExact && current.PathType != PathExact {
		return true
	}
	if current.PathType == PathExact && candidate.PathType != PathExact {
		return false
	}
	return len(candidate.Path) > len(current.Path)
}

// prefixMatches implements segment-aware prefix matching: /api matches /api and
// /api/x but not /apix.
func prefixMatches(rulePath, requestPath string) bool {
	if rulePath == "" || rulePath == "/" {
		return true
	}
	trimmed := strings.TrimSuffix(rulePath, "/")
	if requestPath == trimmed {
		return true
	}
	return strings.HasPrefix(requestPath, trimmed+"/")
}

func hostOnly(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

type ingressTargetKey struct{}

// IngressProxy is a reverse proxy that routes matched Ingress paths to Service
// backends. It only resolves Services, so it can never expose the management API,
// and it overwrites untrusted forwarded headers.
type IngressProxy struct {
	ingress               *Ingress
	resolve               func(service string, port int) (string, bool)
	proxy                 *httputil.ReverseProxy
	responseHeaderTimeout time.Duration
	dial                  func(context.Context, string, string) (net.Conn, error)
}

// IngressOption customizes the proxy.
type IngressOption func(*IngressProxy)

// WithResponseHeaderTimeout bounds how long the proxy waits for a backend to
// start responding.
func WithResponseHeaderTimeout(d time.Duration) IngressOption {
	return func(p *IngressProxy) { p.responseHeaderTimeout = d }
}

// WithIngressDialContext keeps namespace access in the runtime adapter. The
// caller's dialer must restrict targets to declared application Services.
func WithIngressDialContext(dial func(context.Context, string, string) (net.Conn, error)) IngressOption {
	return func(p *IngressProxy) { p.dial = dial }
}

// NewIngressProxy builds a reverse proxy. resolve returns the backend host:port
// for a Service and port.
func NewIngressProxy(ingress *Ingress, resolve func(service string, port int) (string, bool), opts ...IngressOption) *IngressProxy {
	p := &IngressProxy{ingress: ingress, resolve: resolve, responseHeaderTimeout: 15 * time.Second}
	for _, opt := range opts {
		opt(p)
	}
	if p.dial == nil {
		p.dial = (&net.Dialer{Timeout: 5 * time.Second}).DialContext
	}
	p.proxy = &httputil.ReverseProxy{
		Rewrite: p.rewrite,
		Transport: &http.Transport{
			DialContext:           p.dial,
			ResponseHeaderTimeout: p.responseHeaderTimeout,
			IdleConnTimeout:       30 * time.Second,
			MaxIdleConnsPerHost:   16,
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "bad gateway", http.StatusBadGateway)
		},
	}
	return p
}

// CloseIdleConnections releases cached relay connections on update/teardown.
func (p *IngressProxy) CloseIdleConnections() {
	if transport, ok := p.proxy.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
}

// ServeHTTP matches and forwards a request.
func (p *IngressProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path, ok := p.ingress.Match(r.Host, r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	target, ok := p.resolve(path.Service, path.Port)
	if !ok {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx := context.WithValue(r.Context(), ingressTargetKey{}, target)
	p.proxy.ServeHTTP(w, r.WithContext(ctx))
}

func (p *IngressProxy) rewrite(pr *httputil.ProxyRequest) {
	target, _ := pr.In.Context().Value(ingressTargetKey{}).(string)
	pr.Out.URL.Scheme = "http"
	pr.Out.URL.Host = target
	pr.Out.Host = pr.In.Host
	// Drop any client-supplied forwarding headers before setting our own.
	pr.Out.Header.Del("X-Forwarded-For")
	pr.Out.Header.Del("X-Forwarded-Host")
	pr.Out.Header.Del("X-Forwarded-Proto")
	pr.SetXForwarded()
}
