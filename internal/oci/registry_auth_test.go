// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistryPullRefreshesRejectedCachedToken(t *testing.T) {
	image := buildTestImage(t)
	var generation atomic.Int64
	generation.Store(1)
	var issued atomic.Int64
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			issued.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": fmt.Sprintf("opaque-%d", generation.Load())})
			return
		}
		if r.Header.Get("Authorization") != fmt.Sprintf("Bearer opaque-%d", generation.Load()) {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="test"`, server.URL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// Reuse the actual distribution fixture for manifest and blob responses.
		switch r.URL.Path {
		case "/v2/library/test/manifests/latest":
			w.Header().Set("Content-Type", MediaTypeOCIIndex)
			_, _ = w.Write(image.index)
		case "/v2/library/test/manifests/" + image.manifestDigest:
			w.Header().Set("Content-Type", MediaTypeOCIManifest)
			_, _ = w.Write(image.manifest)
		case "/v2/library/test/blobs/" + image.configDigest:
			_, _ = w.Write(image.config)
		case "/v2/library/test/blobs/" + image.layerDigest:
			_, _ = w.Write(image.layer)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	host, err := urlHost(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := NewRegistryClient()
	client.Insecure = []string{host}
	cas, err := OpenCAS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	puller := &Puller{Registry: client, CAS: cas, Platform: Platform{OS: "linux", Architecture: "amd64"}}
	ref, err := ParseReference(host + "/library/test:latest")
	if err != nil {
		t.Fatal(err)
	}
	first, err := puller.Pull(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	generation.Store(2) // expiry/revocation without decoding or trusting a JWT
	second, err := puller.Pull(context.Background(), ref)
	if err != nil {
		t.Fatal("second pull failed after token rejection", err)
	}
	if first.ManifestDigest != second.ManifestDigest || issued.Load() != 2 {
		t.Fatal("unexpected image identity or token requests", issued.Load())
	}
}

func TestRegistryRejectedFreshTokenRetryBounded(t *testing.T) {
	var issued, requests atomic.Int64
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			issued.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "rejected"})
			return
		}
		requests.Add(1)
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="test"`, server.URL))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	host, err := urlHost(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := NewRegistryClient()
	client.Insecure = []string{host}
	ref, err := ParseReference(host + "/library/test:latest")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.FetchManifest(context.Background(), ref); err == nil {
		t.Fatal("unauthorized request succeeded")
	}
	if issued.Load() != 2 || requests.Load() != 3 {
		t.Fatal("unbounded authentication retries", issued.Load(), requests.Load())
	}
}

func TestRegistryTokenRefreshHonorsCancellation(t *testing.T) {
	var issued atomic.Int64
	var server *httptest.Server
	refresh := make(chan struct{})
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			if issued.Add(1) == 1 {
				_ = json.NewEncoder(w).Encode(map[string]string{"token": "stale"})
				return
			}
			close(refresh)
			<-r.Context().Done()
			return
		}
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="test"`, server.URL))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	host, err := urlHost(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := NewRegistryClient()
	client.Insecure = []string{host}
	ref, err := ParseReference(host + "/library/test:latest")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, _, err := client.FetchManifest(ctx, ref); result <- err }()
	select {
	case <-refresh:
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("refresh never started")
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("refresh ignored cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled refresh blocked")
	}
}

func TestBearerCacheKeySeparatesServicesAndFallbackScope(t *testing.T) {
	first := map[string]string{"realm": "https://registry.example/token", "service": "one", "scope": "repository:demo:pull"}
	second := map[string]string{"realm": first["realm"], "service": "two", "scope": first["scope"]}
	if bearerCacheKey(first, "") == bearerCacheKey(second, "") {
		t.Fatal("token audiences collide")
	}
	if bearerCacheKey(first, "") != bearerCacheKey(first, first["scope"]) {
		t.Fatal("effective scopes differ")
	}
}
