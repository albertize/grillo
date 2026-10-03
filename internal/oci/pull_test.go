// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// testImage is a small but complete OCI image with one uncompressed layer.
type testImage struct {
	layer          []byte
	layerDigest    string
	config         []byte
	configDigest   string
	manifest       []byte
	manifestDigest string
	index          []byte
}

func buildTestImage(t *testing.T) testImage {
	t.Helper()
	layer := buildTar(t,
		tarEntry{name: "bin", typeflag: tar.TypeDir, mode: 0o755},
		tarEntry{name: "bin/app", typeflag: tar.TypeReg, mode: 0o755, body: []byte("#!/bin/sh\necho hi\n")},
	)
	layerDigest := "sha256:" + sha256Hex(layer)
	config := fmt.Sprintf(`{"architecture":"amd64","os":"linux","config":{"Cmd":["/bin/app"],"Env":["A=1"],"WorkingDir":"/srv"},"rootfs":{"type":"layers","diff_ids":["%s"]}}`, layerDigest)
	configDigest := "sha256:" + sha256Hex([]byte(config))
	manifest := fmt.Sprintf(`{"schemaVersion":2,"mediaType":"%s","config":{"mediaType":"%s","digest":"%s","size":%d},"layers":[{"mediaType":"%s","digest":"%s","size":%d}]}`,
		MediaTypeOCIManifest, MediaTypeOCIConfig, configDigest, len(config), MediaTypeOCILayer, layerDigest, len(layer))
	manifestDigest := "sha256:" + sha256Hex([]byte(manifest))
	index := fmt.Sprintf(`{"schemaVersion":2,"mediaType":"%s","manifests":[{"mediaType":"%s","digest":"%s","size":%d,"platform":{"os":"linux","architecture":"amd64"}}]}`,
		MediaTypeOCIIndex, MediaTypeOCIManifest, manifestDigest, len(manifest))
	return testImage{
		layer: layer, layerDigest: layerDigest,
		config: []byte(config), configDigest: configDigest,
		manifest: []byte(manifest), manifestDigest: manifestDigest,
		index: []byte(index),
	}
}

// testRegistry serves a testImage over the distribution API with bearer auth.
type testRegistry struct {
	server      *httptest.Server
	image       testImage
	requireAuth bool

	mu          sync.Mutex
	blobRequest int
}

func newTestRegistry(t *testing.T, image testImage, requireAuth bool) *testRegistry {
	t.Helper()
	r := &testRegistry{image: image, requireAuth: requireAuth}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "test-token"})
	})
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, req *http.Request) {
		if r.requireAuth && req.Header.Get("Authorization") != "Bearer test-token" {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="test"`, r.server.URL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case strings.Contains(req.URL.Path, "/manifests/latest"):
			w.Header().Set("Content-Type", MediaTypeOCIIndex)
			_, _ = w.Write(r.image.index)
		case strings.Contains(req.URL.Path, "/manifests/"):
			w.Header().Set("Content-Type", MediaTypeOCIManifest)
			_, _ = w.Write(r.image.manifest)
		case strings.Contains(req.URL.Path, "/blobs/"):
			digest := req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:]
			var body []byte
			switch digest {
			case r.image.configDigest:
				body = r.image.config
			case r.image.layerDigest:
				body = r.image.layer
			default:
				http.NotFound(w, req)
				return
			}
			r.mu.Lock()
			r.blobRequest++
			r.mu.Unlock()
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
			_, _ = w.Write(body)
		default:
			http.NotFound(w, req)
		}
	})
	r.server = httptest.NewServer(mux)
	t.Cleanup(r.server.Close)
	return r
}

func (r *testRegistry) host(t *testing.T) string {
	t.Helper()
	host, err := urlHost(r.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return host
}

func (r *testRegistry) client(t *testing.T) *RegistryClient {
	t.Helper()
	c := NewRegistryClient()
	c.Insecure = []string{r.host(t)}
	return c
}

func newPuller(t *testing.T, registry *testRegistry) *Puller {
	t.Helper()
	cas, err := OpenCAS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Puller{CAS: cas, Registry: registry.client(t), Platform: Platform{OS: "linux", Architecture: "amd64"}}
}

func TestPullAndUnpack(t *testing.T) {
	image := buildTestImage(t)
	registry := newTestRegistry(t, image, true)
	puller := newPuller(t, registry)
	ref, err := ParseReference(registry.host(t) + "/library/test:latest")
	if err != nil {
		t.Fatal(err)
	}
	pulled, err := puller.Pull(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if pulled.ManifestDigest != image.manifestDigest {
		t.Fatalf("manifest digest = %s", pulled.ManifestDigest)
	}
	if merged := MergeConfig(pulled.Config, RuntimeOptions{}); len(merged.Args) != 1 || merged.Args[0] != "/bin/app" || merged.WorkingDir != "/srv" {
		t.Fatalf("merged = %+v", merged)
	}
	root := t.TempDir()
	if err := puller.Unpack(pulled, root, UnpackOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "bin/app"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "echo hi") {
		t.Fatalf("unpacked content = %q", data)
	}
}

func TestPullConcurrentDedup(t *testing.T) {
	image := buildTestImage(t)
	registry := newTestRegistry(t, image, true)
	puller := newPuller(t, registry)
	ref, _ := ParseReference(registry.host(t) + "/library/test:latest")

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := puller.Pull(context.Background(), ref)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	registry.mu.Lock()
	blobRequests := registry.blobRequest
	registry.mu.Unlock()
	if blobRequests != 2 {
		t.Fatalf("blob requests = %d, want 2 (config + layer fetched once)", blobRequests)
	}
}

func TestPullRejectsCorruptBlob(t *testing.T) {
	image := buildTestImage(t)
	image.layer = []byte("corrupted content")
	registry := newTestRegistry(t, image, true)
	puller := newPuller(t, registry)
	ref, _ := ParseReference(registry.host(t) + "/library/test:latest")
	if _, err := puller.Pull(context.Background(), ref); err == nil {
		t.Fatal("expected digest mismatch for corrupt blob")
	}
}

func TestPullRejectsBadDiffID(t *testing.T) {
	image := buildTestImage(t)
	// Rebuild the config with a diff_id that does not match the layer content.
	config := fmt.Sprintf(`{"architecture":"amd64","os":"linux","config":{"Cmd":["/bin/app"]},"rootfs":{"type":"layers","diff_ids":["sha256:%s"]}}`, strings.Repeat("a", 64))
	configDigest := "sha256:" + sha256Hex([]byte(config))
	manifest := fmt.Sprintf(`{"schemaVersion":2,"mediaType":"%s","config":{"mediaType":"%s","digest":"%s","size":%d},"layers":[{"mediaType":"%s","digest":"%s","size":%d}]}`,
		MediaTypeOCIManifest, MediaTypeOCIConfig, configDigest, len(config), MediaTypeOCILayer, image.layerDigest, len(image.layer))
	manifestDigest := "sha256:" + sha256Hex([]byte(manifest))
	image.config = []byte(config)
	image.configDigest = configDigest
	image.manifest = []byte(manifest)
	image.manifestDigest = manifestDigest
	image.index = []byte(fmt.Sprintf(`{"schemaVersion":2,"mediaType":"%s","manifests":[{"mediaType":"%s","digest":"%s","size":%d,"platform":{"os":"linux","architecture":"amd64"}}]}`,
		MediaTypeOCIIndex, MediaTypeOCIManifest, manifestDigest, len(manifest)))
	registry := newTestRegistry(t, image, true)
	puller := newPuller(t, registry)
	ref, _ := ParseReference(registry.host(t) + "/library/test:latest")
	pulled, err := puller.Pull(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := puller.Unpack(pulled, t.TempDir(), UnpackOptions{}); err == nil {
		t.Fatal("expected diff_id mismatch")
	}
}

func TestRegistryRejectsMissingToken(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="http://127.0.0.1:1/token",service="test"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	host, _ := urlHost(server.URL)
	client := NewRegistryClient()
	client.Insecure = []string{host}
	ref := Reference{Registry: host, Repository: "library/test", Tag: "latest"}
	if _, _, err := client.FetchManifest(context.Background(), ref); err == nil {
		t.Fatal("expected token failure")
	}
}

func TestRegistryManifestSizeLimit(t *testing.T) {
	image := buildTestImage(t)
	registry := newTestRegistry(t, image, true)
	client := registry.client(t)
	client.MaxManifestBytes = 8
	ref, _ := ParseReference(registry.host(t) + "/library/test:latest")
	if _, _, err := client.FetchManifest(context.Background(), ref); err == nil {
		t.Fatal("expected size limit error")
	}
}

func TestCrossHostRedirectStripsAuthorization(t *testing.T) {
	image := buildTestImage(t)
	var leaked atomic.Value
	var second *httptest.Server
	second = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		leaked.Store(req.Header.Get("Authorization"))
		_, _ = w.Write(image.layer)
	}))
	defer second.Close()

	var first *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "test-token"})
	})
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer test-token" {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="test"`, first.URL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.Redirect(w, req, second.URL+req.URL.Path, http.StatusFound)
	})
	first = httptest.NewServer(mux)
	defer first.Close()
	host, _ := urlHost(first.URL)

	client := NewRegistryClient()
	client.Insecure = []string{host}
	ref := Reference{Registry: host, Repository: "library/test", Tag: "latest"}
	body, _, err := client.OpenBlob(context.Background(), ref, image.layerDigest)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
	if forwarded, _ := leaked.Load().(string); strings.Contains(forwarded, "Bearer") {
		t.Fatalf("Authorization header leaked across hosts: %q", forwarded)
	}
}

func urlHost(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	return u.Host, nil
}
