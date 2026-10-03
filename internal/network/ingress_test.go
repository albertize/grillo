//go:build linux

// SPDX-License-Identifier: Apache-2.0

package network

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIngressMatch(t *testing.T) {
	ingress := NewIngress([]IngressRule{{
		Host: "app.local",
		Paths: []IngressPath{
			{Path: "/", PathType: PathPrefix, Service: "frontend", Port: 3000},
			{Path: "/api", PathType: PathPrefix, Service: "backend", Port: 8080},
			{Path: "/exact", PathType: PathExact, Service: "exact", Port: 80},
		},
	}})
	cases := []struct {
		host, path string
		want       string
		found      bool
	}{
		{"app.local", "/api", "backend", true},
		{"app.local", "/api/", "backend", true},
		{"app.local", "/api/users", "backend", true},
		{"app.local", "/apix", "frontend", true}, // segment-aware: /api does not match /apix
		{"app.local", "/exact", "exact", true},   // Exact beats Prefix
		{"app.local", "/exact/", "frontend", true},
		{"app.local:8080", "/api", "backend", true}, // host port ignored
		{"other.local", "/api", "", false},
	}
	for _, tc := range cases {
		path, ok := ingress.Match(tc.host, tc.path)
		if ok != tc.found || (ok && path.Service != tc.want) {
			t.Errorf("Match(%q, %q) = %+v ok=%v, want %q found=%v", tc.host, tc.path, path, ok, tc.want, tc.found)
		}
	}
}

func TestIngressProxyForwardsAndSanitizesForwardedHeaders(t *testing.T) {
	var seenXFF string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenXFF = r.Header.Get("X-Forwarded-For")
		_, _ = fmt.Fprint(w, "backend")
	}))
	defer backend.Close()
	target := strings.TrimPrefix(backend.URL, "http://")

	ingress := NewIngress([]IngressRule{{
		Host:  "app.local",
		Paths: []IngressPath{{Path: "/api", PathType: PathPrefix, Service: "backend", Port: 8080}},
	}})
	proxy := NewIngressProxy(ingress, func(service string, port int) (string, bool) {
		if service == "backend" && port == 8080 {
			return target, true
		}
		return "", false
	})
	server := httptest.NewServer(proxy)
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/users", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "app.local"
	req.Header.Set("X-Forwarded-For", "203.0.113.7") // spoofed by the client
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "backend" {
		t.Fatalf("body = %q", body)
	}
	if strings.Contains(seenXFF, "203.0.113.7") {
		t.Fatalf("client-supplied X-Forwarded-For was trusted: %q", seenXFF)
	}
}

func TestIngressProxyTimesOut(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = fmt.Fprint(w, "late")
	}))
	defer slow.Close()
	target := strings.TrimPrefix(slow.URL, "http://")
	ingress := NewIngress([]IngressRule{{Host: "app.local", Paths: []IngressPath{{Path: "/api", PathType: PathPrefix, Service: "backend", Port: 8080}}}})
	proxy := NewIngressProxy(ingress, func(string, int) (string, bool) { return target, true }, WithResponseHeaderTimeout(200*time.Millisecond))
	server := httptest.NewServer(proxy)
	defer server.Close()

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api", nil)
	req.Host = "app.local"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("slow backend status = %d, want 502", resp.StatusCode)
	}
}

func TestIngressProxyNoMatchAndUnavailable(t *testing.T) {
	ingress := NewIngress([]IngressRule{{
		Host:  "app.local",
		Paths: []IngressPath{{Path: "/api", PathType: PathPrefix, Service: "backend", Port: 8080}},
	}})
	proxy := NewIngressProxy(ingress, func(string, int) (string, bool) { return "", false })
	server := httptest.NewServer(proxy)
	defer server.Close()

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/nope", nil)
	req.Host = "app.local"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("no-match status = %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api", nil)
	req.Host = "app.local"
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unresolved service status = %d", resp.StatusCode)
	}
}
