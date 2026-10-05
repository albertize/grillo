// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/albertize/grillo/internal/oci"
	"github.com/albertize/grillo/internal/state"
	"go.yaml.in/yaml/v3"
)

const chartConfigMedia = "application/vnd.cncf.helm.config.v1+json"
const chartLayerMedia = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"
const maxManifest = 1 << 20
const maxCacheRecord = 25 << 20

// ChartRegistry is the OCI Distribution subset needed for chart artifacts.
// The normal implementation uses HTTPS with no inherited credentials.
type ChartRegistry interface {
	FetchManifest(context.Context, oci.Reference) ([]byte, string, error)
	OpenBlob(context.Context, oci.Reference, string) (io.ReadCloser, int64, error)
}

// Exact SemVer only: never latest, a range, a wildcard or a leading v.
var exactVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)
var shaDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type cachedChart struct {
	Schema    int    `json:"schema"`
	Reference string `json:"reference"`
	Version   string `json:"version"`
	Digest    string `json:"digest"`
	Manifest  []byte `json:"manifest"`
	Config    []byte `json:"config"`
	Archive   []byte `json:"archive"`
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func chartReference(input, version string) (oci.Reference, error) {
	if !exactVersion.MatchString(version) {
		return oci.Reference{}, errors.New("exact chart version required")
	}
	// Numeric prerelease identifiers cannot have leading zeroes (SemVer).
	pre := strings.SplitN(strings.SplitN(version, "+", 2)[0], "-", 2)
	if len(pre) == 2 {
		for _, part := range strings.Split(pre[1], ".") {
			if len(part) > 1 && part[0] == '0' && strings.Trim(part, "0123456789") == "" {
				return oci.Reference{}, errors.New("invalid prerelease")
			}
		}
	}
	u, err := url.Parse(input)
	if err != nil || u.Scheme != "oci" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || strings.Contains(u.Path, "@") {
		return oci.Reference{}, errors.New("invalid OCI chart reference")
	}
	// ParseReference validates repository syntax; require an explicit registry.
	repository := strings.TrimPrefix(u.Path, "/")
	ref, err := oci.ParseReference("chart.invalid/" + repository + ":" + strings.ReplaceAll(version, "+", "_"))
	if err != nil || ref.Repository != repository || ref.Digest != "" {
		return oci.Reference{}, errors.New("invalid OCI chart reference")
	}
	ref.Registry = u.Host
	return ref, nil
}

// resolveChart reads a pinned cache entry without mutations. Missing entries
// may be fetched only with explicit consent. Even consent never refreshes a
// cached version's pin silently; use a new cache location to review a new pin.
func resolveChart(ctx context.Context, opts Options, workspace string) (string, string) {
	ref, err := chartReference(opts.Chart, opts.ChartVersion)
	if err != nil {
		return "", "HELM_OCI_REFERENCE"
	}
	if opts.ChartDigest != "" && !shaDigest.MatchString(opts.ChartDigest) {
		return "", "HELM_OCI_DIGEST"
	}
	if opts.CacheDir == "" {
		return "", "HELM_OCI_CACHE"
	}
	opts.CacheDir, err = filepath.Abs(opts.CacheDir)
	if err != nil || safeCachePath(opts.CacheDir) != nil {
		return "", "HELM_OCI_CACHE"
	}
	key := strings.TrimPrefix(digest([]byte(ref.Name()+"\n"+opts.ChartVersion)), "sha256:") + ".json"
	cachePath := filepath.Join(opts.CacheDir, key)
	recordBytes, err := readCache(cachePath)
	var record cachedChart
	if err == nil {
		if json.Unmarshal(recordBytes, &record) != nil {
			return "", "HELM_OCI_CACHE_CORRUPT"
		}
	} else if !os.IsNotExist(err) {
		return "", "HELM_OCI_CACHE_CORRUPT"
	} else {
		if !opts.Fetch {
			return "", "HELM_OCI_FETCH_REQUIRED"
		}
		registry := opts.Registry
		if registry == nil {
			client := oci.NewRegistryClient()
			// A private transport avoids inherited credential-bearing proxies.
			transport := &http.Transport{ForceAttemptHTTP2: true, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 10 * time.Second, IdleConnTimeout: 30 * time.Second}
			defer transport.CloseIdleConnections()
			client.Transport = httpsTransport{inner: transport}
			client.MaxManifestBytes = maxManifest
			client.MaxBlobBytes = maxInput
			registry = client
		}
		manifest, _, err := registry.FetchManifest(ctx, ref)
		if err != nil || len(manifest) > maxManifest {
			return "", "HELM_OCI_FETCH_FAILED"
		}
		record = cachedChart{Schema: 1, Reference: ref.Name(), Version: opts.ChartVersion, Digest: digest(manifest), Manifest: manifest}
		if opts.ChartDigest != "" && record.Digest != opts.ChartDigest {
			return "", "HELM_OCI_DIGEST"
		}
		m, err := chartManifest(record.Manifest)
		if err != nil {
			return "", "HELM_OCI_MANIFEST"
		}
		record.Config, err = fetchChartBlob(ctx, registry, ref, m.Config, maxFile)
		if err != nil {
			return "", "HELM_OCI_FETCH_FAILED"
		}
		record.Archive, err = fetchChartBlob(ctx, registry, ref, m.Layers[0], maxInput)
		if err != nil {
			return "", "HELM_OCI_FETCH_FAILED"
		}
	}
	if record.Schema != 1 || record.Reference != ref.Name() || record.Version != opts.ChartVersion || record.Digest != digest(record.Manifest) || (opts.ChartDigest != "" && record.Digest != opts.ChartDigest) {
		return "", "HELM_OCI_CACHE_CORRUPT"
	}
	m, err := chartManifest(record.Manifest)
	if err != nil || !verifyBlob(record.Config, m.Config, maxFile) || !verifyBlob(record.Archive, m.Layers[0], maxInput) {
		return "", "HELM_OCI_CACHE_CORRUPT"
	}
	name := path.Base(ref.Repository)
	var metadata struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if json.Unmarshal(record.Config, &metadata) != nil || metadata.Name != name || metadata.Version != opts.ChartVersion {
		return "", "HELM_OCI_METADATA"
	}
	extracted := filepath.Join(workspace, "oci")
	if err := os.Mkdir(extracted, 0700); err != nil {
		return "", "HELM_OCI_ARCHIVE"
	}
	if err := extractChart(ctx, record.Archive, extracted, name); err != nil {
		return "", "HELM_OCI_ARCHIVE"
	}
	chart := filepath.Join(extracted, name)
	data, err := readRegular(filepath.Join(chart, "Chart.yaml"))
	var chartMeta struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	}
	if err != nil || yaml.Unmarshal(data, &chartMeta) != nil || chartMeta.Name != name || chartMeta.Version != opts.ChartVersion {
		return "", "HELM_OCI_METADATA"
	}
	// Validate the entire chart before publishing its cache record.
	preflight := filepath.Join(workspace, "oci-preflight")
	if err := os.Mkdir(preflight, 0700); err != nil {
		return "", "HELM_OCI_ARCHIVE"
	}
	if code := snapshot(ctx, chart, preflight); code != "" {
		return "", code
	}
	if recordBytes == nil {
		encoded, err := json.Marshal(record)
		if err != nil || len(encoded) > maxCacheRecord {
			return "", "HELM_OCI_CACHE"
		}
		if err := publishCache(ctx, opts.CacheDir, key, encoded); err != nil {
			return "", "HELM_OCI_CACHE"
		}
	}
	return chart, ""
}

// Apply HTTPS policy to every request, including redirects and token realms.
// The registry adapter handles authentication and never forwards credentials
// across origins; this layer additionally prevents TLS downgrades.
type httpsTransport struct{ inner http.RoundTripper }

func (t httpsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" || req.URL.User != nil {
		return nil, errors.New("chart requests require HTTPS without URL credentials")
	}
	return t.inner.RoundTrip(req)
}

func chartManifest(data []byte) (oci.Manifest, error) {
	var m oci.Manifest
	if len(data) > maxManifest || json.Unmarshal(data, &m) != nil || m.SchemaVersion != 2 || m.MediaType != oci.MediaTypeOCIManifest || m.Config.MediaType != chartConfigMedia || !validDescriptor(m.Config, maxFile) || len(m.Layers) != 1 || m.Layers[0].MediaType != chartLayerMedia || !validDescriptor(m.Layers[0], maxInput) {
		return m, errors.New("invalid Helm OCI manifest")
	}
	return m, nil
}
func validDescriptor(d oci.Descriptor, max int64) bool {
	return shaDigest.MatchString(d.Digest) && d.Size > 0 && d.Size <= max
}
func verifyBlob(data []byte, d oci.Descriptor, max int64) bool {
	return validDescriptor(d, max) && int64(len(data)) == d.Size && digest(data) == d.Digest
}
func fetchChartBlob(ctx context.Context, registry ChartRegistry, ref oci.Reference, d oci.Descriptor, max int64) ([]byte, error) {
	r, size, err := registry.OpenBlob(ctx, ref, d.Digest)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if size >= 0 && size != d.Size {
		return nil, errors.New("blob size mismatch")
	}
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil || !verifyBlob(data, d, max) {
		return nil, errors.New("blob verification failed")
	}
	return data, nil
}
func safeCachePath(dir string) error {
	for p := dir; ; p = filepath.Dir(p) {
		st, err := os.Lstat(p)
		if err == nil {
			if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || (p == dir && (st.Mode().Perm()&0077 != 0 || !ownedCacheEntry(st))) {
				return errors.New("unsafe cache path")
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	return nil
}
func readCache(file string) ([]byte, error) {
	st, err := os.Lstat(file)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || !ownedCacheEntry(st) || st.Size() > maxCacheRecord {
		return nil, errors.New("unsafe cache entry")
	}
	// Rooted reads reject escaping symlinks; cache records are always private.
	root, err := os.OpenRoot(filepath.Dir(file))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := openInput(root, filepath.Base(file))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	current, err := f.Stat()
	if err != nil || !current.Mode().IsRegular() || !ownedCacheEntry(current) || current.Mode().Perm()&0077 != 0 {
		return nil, errors.New("unsafe opened cache entry")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxCacheRecord+1))
	if len(data) > maxCacheRecord {
		return nil, errors.New("cache size limit")
	}
	return data, err
}
func publishCache(ctx context.Context, dir, key string, data []byte) error {
	if err := safeCachePath(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := safeCachePath(dir); err != nil {
		return err
	}
	return withCacheLock(ctx, dir, func() error {
		// Concurrent first fetches must agree on the exact pinned record.
		existing, err := readCache(filepath.Join(dir, key))
		if err == nil {
			if !bytes.Equal(existing, data) {
				return errors.New("concurrent chart pin conflict")
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return err
		}
		return state.AtomicWriteFile(dir, key, data, 0600, state.DefaultOps())
	})
}

func extractChart(ctx context.Context, data []byte, dest, name string) error {
	compressed := bytes.NewReader(data)
	gz, err := gzip.NewReader(compressed)
	if err != nil {
		return err
	}
	defer gz.Close()
	// Include bounded tar headers/padding in the decompression budget.
	bounded := &io.LimitedReader{R: gz, N: maxInput + maxFiles*2048}
	tr := tar.NewReader(bounded)
	count, total := 0, int64(0)
	seen := map[string]bool{}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		count++
		clean := path.Clean(h.Name)
		if count > maxFiles || seen[clean] || strings.Contains(h.Name, "\\") || path.IsAbs(h.Name) || (clean != name && !strings.HasPrefix(clean, name+"/")) || strings.Contains(h.Name, "../") {
			return errors.New("unsafe archive entry")
		}
		seen[clean] = true
		target := filepath.Join(dest, filepath.FromSlash(clean))
		switch h.Typeflag {
		case tar.TypeDir:
			if h.Size != 0 {
				return errors.New("directory with data")
			}
			if err := os.MkdirAll(target, 0700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if h.Size < 0 || h.Size > maxFile || total+h.Size > maxInput {
				return errors.New("archive size limit")
			}
			total += h.Size
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			b, err := io.ReadAll(tr)
			if err != nil || int64(len(b)) != h.Size {
				return errors.New("truncated archive")
			}
			if err := os.WriteFile(target, b, 0600); err != nil {
				return err
			}
		default:
			return errors.New("archive links and special files are unsupported")
		}
	}
	// Consume through gzip checksum validation; reject decompression bombs even
	// after tar's end marker instead of accepting a plausible first chart.
	if _, err := io.Copy(io.Discard, bounded); err != nil {
		return err
	}
	if bounded.N == 0 {
		return errors.New("decompression limit")
	}
	return nil
}
