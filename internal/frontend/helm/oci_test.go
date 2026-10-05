// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/oci"
)

type archiveEntry struct {
	name, data string
	kind       byte
	link       string
}

func chartArchive(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tr := tar.NewWriter(gz)
	for _, e := range entries {
		kind := e.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Mode: 0600, Typeflag: kind, Size: int64(len(e.data)), Linkname: e.link}
		if err := tr.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tr.Write([]byte(e.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func defaultEntries() []archiveEntry {
	return []archiveEntry{{name: "demo/Chart.yaml", data: "apiVersion: v2\nname: demo\nversion: 0.1.0\n"}, {name: "demo/values.yaml", data: "message: initial\n"}, {name: "demo/templates/pod.yaml", data: pod}}
}
func manifestFor(t *testing.T, archive []byte) ([]byte, []byte) {
	t.Helper()
	config := []byte(`{"name":"demo","version":"0.1.0"}`)
	manifest, err := json.Marshal(oci.Manifest{SchemaVersion: 2, MediaType: oci.MediaTypeOCIManifest, Config: oci.Descriptor{MediaType: chartConfigMedia, Digest: digest(config), Size: int64(len(config))}, Layers: []oci.Descriptor{{MediaType: chartLayerMedia, Digest: digest(archive), Size: int64(len(archive))}}})
	if err != nil {
		t.Fatal(err)
	}
	return manifest, config
}

type fixtureRegistry struct {
	manifest, config, archive []byte
	calls                     atomic.Int64
	err                       error
}

func (r *fixtureRegistry) FetchManifest(ctx context.Context, _ oci.Reference) ([]byte, string, error) {
	r.calls.Add(1)
	if r.err != nil {
		return nil, "", r.err
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	return r.manifest, oci.MediaTypeOCIManifest, nil
}
func (r *fixtureRegistry) OpenBlob(ctx context.Context, _ oci.Reference, d string) (io.ReadCloser, int64, error) {
	r.calls.Add(1)
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	data := r.archive
	if d == digest(r.config) {
		data = r.config
	}
	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
}
func registryFixture(t *testing.T, archive []byte) *fixtureRegistry {
	m, c := manifestFor(t, archive)
	return &fixtureRegistry{manifest: m, config: c, archive: archive}
}
func remoteOptions(t *testing.T, r ChartRegistry) Options {
	t.Helper()
	return Options{Chart: "oci://registry.example.test/charts/demo", ChartVersion: "0.1.0", Release: "demo", CacheDir: filepath.Join(t.TempDir(), "cache"), Registry: r}
}
func requireCode(t *testing.T, opts Options, want string) {
	t.Helper()
	result, err := Compile(context.Background(), opts)
	if err != nil || len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != want {
		t.Fatalf("want %s, got %v %v", want, err, result.Diagnostics)
	}
}
func cacheRecordPath(t *testing.T, dir string) string {
	t.Helper()
	entries, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("cache records: %v %v", entries, err)
	}
	return entries[0]
}
func TestOCIHTTPSRealHelmStableVerifiedOffline(t *testing.T) {
	binary := realHelm(t)
	archive := chartArchive(t, defaultEntries())
	manifest, config := manifestFor(t, archive)
	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/v2/charts/demo/manifests/0.1.0":
			w.Header().Set("Content-Type", oci.MediaTypeOCIManifest)
			w.Write(manifest)
		case "/v2/charts/demo/blobs/" + digest(config):
			w.Write(config)
		case "/v2/charts/demo/blobs/" + digest(archive):
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := oci.NewRegistryClient()
	client.Transport = server.Client().Transport
	opts := remoteOptions(t, client)
	opts.Binary = binary
	opts.Chart = "oci://" + strings.TrimPrefix(server.URL, "https://") + "/charts/demo"
	opts.ChartDigest = digest(manifest)
	requireCode(t, opts, "HELM_OCI_FETCH_REQUIRED")
	if calls.Load() != 0 {
		t.Fatal("fetch without consent")
	}
	if _, err := os.Stat(opts.CacheDir); !os.IsNotExist(err) {
		t.Fatal("cache created without consent")
	}
	opts.Fetch = true
	first, err := Compile(context.Background(), opts)
	if err != nil || first.Diagnostics.HasErrors() {
		t.Fatal(err, first.Diagnostics)
	}
	if calls.Load() != 3 {
		t.Fatal("unexpected Distribution requests", calls.Load())
	}
	cached := cacheRecordPath(t, opts.CacheDir)
	st, err := os.Stat(cached)
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("cache entry not private", err)
	}
	local, err := Compile(context.Background(), Options{Binary: binary, Chart: fixture(t, pod), Release: "demo"})
	if err != nil || local.Diagnostics.HasErrors() {
		t.Fatal(err, local.Diagnostics)
	}
	localHash, _ := model.Hash(local.Application)
	remoteHash, _ := model.Hash(first.Application)
	if localHash != remoteHash {
		t.Fatal("local and OCI chart IR differ")
	}
	server.Close()
	opts.Fetch = false
	again, err := Compile(context.Background(), opts)
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatal("offline cache unstable", err, again.Diagnostics)
	}
	// Consent does not silently refresh an existing mutable version tag.
	opts.Fetch = true
	again, err = Compile(context.Background(), opts)
	if err != nil || !reflect.DeepEqual(first, again) || calls.Load() != 3 {
		t.Fatal("cached pin refreshed", err)
	}
}
func TestOCIHTTPSRejectsRedirectAndTokenDowngrade(t *testing.T) {
	for _, mode := range []string{"redirect", "token"} {
		t.Run(mode, func(t *testing.T) {
			var insecureCalls atomic.Int64
			insecure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { insecureCalls.Add(1) }))
			defer insecure.Close()
			secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "redirect" {
					http.Redirect(w, r, insecure.URL, http.StatusFound)
				} else {
					w.Header().Set("WWW-Authenticate", `Bearer realm="`+insecure.URL+`/token"`)
					w.WriteHeader(http.StatusUnauthorized)
				}
			}))
			defer secure.Close()
			client := oci.NewRegistryClient()
			client.Transport = httpsTransport{inner: secure.Client().Transport}
			opts := remoteOptions(t, client)
			opts.Chart = "oci://" + strings.TrimPrefix(secure.URL, "https://") + "/charts/demo"
			opts.Fetch = true
			requireCode(t, opts, "HELM_OCI_FETCH_FAILED")
			if insecureCalls.Load() != 0 {
				t.Fatal("HTTP downgrade request sent")
			}
		})
	}
}

func TestOCIFetchCancellation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	client := oci.NewRegistryClient()
	client.Transport = server.Client().Transport
	opts := remoteOptions(t, client)
	opts.Chart = "oci://" + strings.TrimPrefix(server.URL, "https://") + "/charts/demo"
	opts.Fetch = true
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := Compile(ctx, opts); err != context.DeadlineExceeded {
		t.Fatal("fetch cancellation ignored", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("fetch cancellation unbounded")
	}
	if _, err := os.Stat(opts.CacheDir); !os.IsNotExist(err) {
		t.Fatal("canceled fetch published cache")
	}
}

func TestOCIReferenceValidationAndConsent(t *testing.T) {
	r := registryFixture(t, chartArchive(t, defaultEntries()))
	for _, version := range []string{"", "latest", "^0.1.0", "0.*", "v0.1.0", "0.1", "00.1.0", "0.1.0-01"} {
		opts := remoteOptions(t, r)
		opts.ChartVersion = version
		opts.Fetch = true
		requireCode(t, opts, "HELM_OCI_REFERENCE")
	}
	for _, reference := range []string{"oci://user:secret@registry.example.test/charts/demo", "oci://registry.example.test/charts/demo:latest", "oci://registry.example.test/charts/../demo", "oci://registry.example.test/charts/demo?secret=value", "oci://registry.example.test/charts/demo#x"} {
		opts := remoteOptions(t, r)
		opts.Chart = reference
		opts.Fetch = true
		requireCode(t, opts, "HELM_OCI_REFERENCE")
	}
	if r.calls.Load() != 0 {
		t.Fatal("invalid reference fetched")
	}
	opts := remoteOptions(t, r)
	opts.Fetch = true
	opts.ChartDigest = "invalid"
	requireCode(t, opts, "HELM_OCI_DIGEST")
	opts.ChartDigest = "sha256:" + strings.Repeat("0", 64)
	requireCode(t, opts, "HELM_OCI_DIGEST")
	if r.calls.Load() != 1 {
		t.Fatal("digest mismatch fetched blobs")
	}
	opts = remoteOptions(t, r)
	requireCode(t, opts, "HELM_OCI_FETCH_REQUIRED")
}
func TestOCICorruptionFailureRetryAndRedaction(t *testing.T) {
	binary := realHelm(t)
	good := chartArchive(t, defaultEntries())
	r := registryFixture(t, good)
	opts := remoteOptions(t, r)
	opts.Binary = binary
	opts.Fetch = true
	r.archive = good[:len(good)/2]
	requireCode(t, opts, "HELM_OCI_FETCH_FAILED")
	if _, err := os.Stat(opts.CacheDir); !os.IsNotExist(err) {
		t.Fatal("partial cache published")
	}
	r.archive = append([]byte(nil), good...)
	r.archive[0] ^= 1
	requireCode(t, opts, "HELM_OCI_FETCH_FAILED")
	if _, err := os.Stat(opts.CacheDir); !os.IsNotExist(err) {
		t.Fatal("digest-mismatched blob published")
	}
	r.archive = good
	result, err := Compile(context.Background(), opts)
	if err != nil || result.Diagnostics.HasErrors() {
		t.Fatal("retry failed", err, result.Diagnostics)
	}
	file := cacheRecordPath(t, opts.CacheDir)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var record cachedChart
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	record.Archive[0] ^= 1
	corrupt, _ := json.Marshal(record)
	if err := os.WriteFile(file, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	before := r.calls.Load()
	requireCode(t, opts, "HELM_OCI_CACHE_CORRUPT")
	if r.calls.Load() != before {
		t.Fatal("corrupt cache triggered implicit repair")
	}
	unchanged, _ := os.ReadFile(file)
	if !bytes.Equal(unchanged, corrupt) {
		t.Fatal("corrupt cache overwritten")
	}
	r.err = errors.New("synthetic-sensitive-registry-error")
	opts = remoteOptions(t, r)
	opts.Fetch = true
	result, err = Compile(context.Background(), opts)
	if err != nil || !result.Diagnostics.HasErrors() || strings.Contains(result.Diagnostics[0].Error(), "synthetic-sensitive") {
		t.Fatal("registry error leaked", err, result.Diagnostics)
	}
}
func TestOCIArchiveRejectionsAndNoPublication(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry archiveEntry
	}{
		{"traversal", archiveEntry{name: "demo/../../escape", data: "bad"}},
		{"absolute", archiveEntry{name: "/escape", data: "bad"}},
		{"symlink", archiveEntry{name: "demo/link", kind: tar.TypeSymlink, link: "/etc/passwd"}},
		{"hardlink", archiveEntry{name: "demo/link", kind: tar.TypeLink, link: "demo/values.yaml"}},
		{"fifo", archiveEntry{name: "demo/pipe", kind: tar.TypeFifo}},
		{"duplicate", archiveEntry{name: "demo/values.yaml", data: "replacement"}},
		{"large", archiveEntry{name: "demo/large", data: strings.Repeat("x", maxFile+1)}},
		{"other-root", archiveEntry{name: "other/Chart.yaml", data: "bad"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries := append(defaultEntries(), tc.entry)
			r := registryFixture(t, chartArchive(t, entries))
			opts := remoteOptions(t, r)
			opts.Fetch = true
			requireCode(t, opts, "HELM_OCI_ARCHIVE")
			if _, err := os.Stat(opts.CacheDir); !os.IsNotExist(err) {
				t.Fatal("unsafe archive cached")
			}
		})
	}
	// Valid tar with corrupted gzip trailer must still fail.
	b := chartArchive(t, defaultEntries())
	b[len(b)-1] ^= 1
	opts := remoteOptions(t, registryFixture(t, b))
	opts.Fetch = true
	requireCode(t, opts, "HELM_OCI_ARCHIVE")
}
func TestOCIMetadataAndManifestRestrictions(t *testing.T) {
	archive := chartArchive(t, defaultEntries())
	for _, change := range []func(*oci.Manifest){
		func(m *oci.Manifest) { m.SchemaVersion = 1 },
		func(m *oci.Manifest) { m.Config.MediaType = oci.MediaTypeOCIConfig },
		func(m *oci.Manifest) { m.Layers[0].MediaType = oci.MediaTypeOCILayerGzip },
		func(m *oci.Manifest) { m.Layers[0].Size = maxInput + 1 },
		func(m *oci.Manifest) { m.Layers[0].Digest = "invalid" },
		func(m *oci.Manifest) { m.Layers = append(m.Layers, m.Layers[0]) },
	} {
		r := registryFixture(t, archive)
		var m oci.Manifest
		json.Unmarshal(r.manifest, &m)
		change(&m)
		r.manifest, _ = json.Marshal(m)
		opts := remoteOptions(t, r)
		opts.Fetch = true
		requireCode(t, opts, "HELM_OCI_MANIFEST")
	}
	entries := defaultEntries()
	entries[0].data = strings.Replace(entries[0].data, "version: 0.1.0", "version: 0.2.0", 1)
	opts := remoteOptions(t, registryFixture(t, chartArchive(t, entries)))
	opts.Fetch = true
	requireCode(t, opts, "HELM_OCI_METADATA")
}
func TestOCILockedDependenciesNeverFetchedOrUpdated(t *testing.T) {
	entries := defaultEntries()
	entries[0].data += "dependencies:\n  - name: dep\n    version: 1.2.3\n    repository: oci://registry.example.test/dependencies\n"
	lock := "dependencies:\n  - name: dep\n    version: 1.2.3\n    repository: oci://registry.example.test/dependencies\ndigest: sha256:" + strings.Repeat("0", 64) + "\n"
	entries = append(entries, archiveEntry{name: "demo/Chart.lock", data: lock})
	r := registryFixture(t, chartArchive(t, entries))
	original := append([]byte(nil), r.archive...)
	opts := remoteOptions(t, r)
	opts.Fetch = true
	requireCode(t, opts, "HELM_DEPENDENCIES_UNSUPPORTED")
	if r.calls.Load() != 3 || !bytes.Equal(r.archive, original) {
		t.Fatal("dependency fetched or chart lock changed")
	}
	if _, err := os.Stat(opts.CacheDir); !os.IsNotExist(err) {
		t.Fatal("unsupported dependency chart published")
	}
}

func TestOCIConcurrentCachePublication(t *testing.T) {
	binary := realHelm(t)
	r := registryFixture(t, chartArchive(t, defaultEntries()))
	opts := remoteOptions(t, r)
	opts.Binary = binary
	opts.Fetch = true
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := Compile(context.Background(), opts)
			if err != nil || result.Diagnostics.HasErrors() {
				t.Errorf("concurrent compile: %v %v", err, result.Diagnostics)
			}
		}()
	}
	wg.Wait()
	cacheRecordPath(t, opts.CacheDir)
	opts.Registry = &fixtureRegistry{err: errors.New("no network")}
	opts.Fetch = false
	result, err := Compile(context.Background(), opts)
	if err != nil || result.Diagnostics.HasErrors() {
		t.Fatal("concurrent cache incomplete", err, result.Diagnostics)
	}
}
func TestOCIVersionBuildMetadataAndSingleLabelHost(t *testing.T) {
	ref, err := chartReference("oci://registry/charts/demo", "1.2.3-rc.1+build.7")
	if err != nil || ref.Registry != "registry" || ref.Repository != "charts/demo" || ref.Tag != "1.2.3-rc.1_build.7" {
		t.Fatal("OCI SemVer tag mapping", ref, err)
	}
}

func TestOCIUnsafeCacheAndCanceledFetch(t *testing.T) {
	r := registryFixture(t, chartArchive(t, defaultEntries()))
	opts := remoteOptions(t, r)
	opts.Fetch = true
	if err := os.Mkdir(opts.CacheDir, 0755); err != nil {
		t.Fatal(err)
	}
	requireCode(t, opts, "HELM_OCI_CACHE")
	opts = remoteOptions(t, r)
	target := t.TempDir()
	if err := os.Symlink(target, opts.CacheDir); err != nil {
		t.Fatal(err)
	}
	requireCode(t, opts, "HELM_OCI_CACHE")
	if r.calls.Load() != 0 {
		t.Fatal("unsafe cache triggered fetch")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Compile(ctx, opts); err != context.Canceled {
		t.Fatal("cancellation ignored", err)
	}
}
func TestExtractChartCountAndDecompressionLimits(t *testing.T) {
	entries := defaultEntries()
	for i := 0; i < maxFiles; i++ {
		entries = append(entries, archiveEntry{name: "demo/file-" + strings.Repeat("x", i+1)})
	}
	if err := extractChart(context.Background(), chartArchive(t, entries), t.TempDir(), "demo"); err == nil {
		t.Fatal("file count unbounded")
	}
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	gz.Write(bytes.Repeat([]byte{0}, maxInput+maxFiles*2048+1))
	gz.Close()
	if err := extractChart(context.Background(), b.Bytes(), t.TempDir(), "demo"); err == nil {
		t.Fatal("post-tar decompression bomb accepted")
	}
}
func TestOCICachePinConflictAndLockCancellation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	if err := publishCache(context.Background(), dir, "record.json", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := publishCache(context.Background(), dir, "record.json", []byte("other")); err == nil {
		t.Fatal("pin conflict silently refreshed")
	}
	locked := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error)
	go func() {
		done <- withCacheLock(context.Background(), dir, func() error { close(locked); <-release; return nil })
	}()
	<-locked
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := withCacheLock(ctx, dir, func() error { return nil })
	close(release)
	if err != context.DeadlineExceeded {
		t.Fatal("cache lock cancellation ignored", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
