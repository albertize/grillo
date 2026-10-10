// SPDX-License-Identifier: Apache-2.0

// Package helm renders local and explicitly fetched OCI charts with a
// caller-provisioned official Helm executable, then delegates workload semantics
// to the Kubernetes frontend. It never installs tools or contacts a cluster.
package helm

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/albertize/grillo/internal/frontend/kubernetes"
	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/runtimeassets"
	"github.com/albertize/grillo/internal/source"
	"go.yaml.in/yaml/v3"
)

// Version is the exact external renderer version for this initial adapter.
const Version = "v4.2.2"
const KubernetesVersion = "1.30.0"
const maxInput = 16 << 20
const maxFile = 1 << 20
const maxOutput = 8 << 20
const maxFiles = 512
const renderTimeout = 30 * time.Second

// Options configures local or exact-version OCI charts. Values are applied in
// order. Namespace is fixed to default; no plugins or dependency updates run.
type Options struct {
	Binary        string
	Chart         string
	Release       string
	Values        []string
	AllowDegraded []string
	ChartVersion  string
	ChartDigest   string // optional trusted manifest SHA-256 pin
	CacheDir      string
	Fetch         bool          // explicit permission to fetch a missing OCI chart
	Registry      ChartRegistry // nil uses the HTTPS OCI Distribution client
}

var releaseName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// Conservative preflight: reject these tokens even in comments or quoted text.
// This is a compatibility restriction, not a Go-template sandbox.
var unsafeFunctions = regexp.MustCompile(`\b(lookup|getHostByName|randAlphaNum|randAlpha|randAscii|randNumeric|randBytes|randInt|randFloat64|bcrypt|htpasswd|uuidv4|now|ago|durationRound|date|dateInZone|htmlDate|htmlDateInZone|genCA|genSelfSignedCert|genSignedCert|genCAWithKey|genSelfSignedCertWithKey|genSignedCertWithKey|genPrivateKey|encryptAES|shuffle|tpl)\b`)

func diagnostic(code, message string) kubernetes.Result {
	return kubernetes.Result{Diagnostics: source.List{{Code: code, Severity: source.SeverityError, Compatibility: source.Unsupported, Message: message}}}
}

// Compile returns fixed, redacted diagnostics for all external-tool failures.
// Raw stderr, YAML parser errors and values never enter ordinary diagnostics.
func Compile(ctx context.Context, opts Options) (kubernetes.Result, error) {
	if err := ctx.Err(); err != nil {
		return kubernetes.Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()
	if len(opts.Release) > 53 || !releaseName.MatchString(opts.Release) {
		return diagnostic("HELM_RELEASE_INVALID", "Provide a lowercase DNS-compatible release name of at most 53 characters."), nil
	}
	if len(opts.Values) > 32 {
		return diagnostic("HELM_INPUT_LIMIT", "Too many values files."), nil
	}
	root, err := os.MkdirTemp("", "grillo-helm-")
	if err != nil {
		return kubernetes.Result{}, errors.New("create private Helm workspace")
	}
	defer os.RemoveAll(root)
	inputChart := opts.Chart
	if strings.HasPrefix(opts.Chart, "oci://") {
		resolved, code := resolveChart(ctx, opts, root)
		if code != "" {
			if ctx.Err() != nil {
				return kubernetes.Result{}, ctx.Err()
			}
			return diagnostic(code, "OCI chart resolution failed. Require an exact --version, explicit --fetch-chart for a missing cache entry, and verified content; no tool or registry output is exposed."), nil
		}
		inputChart = resolved
	}
	chart := filepath.Join(root, "chart")
	if err := os.Mkdir(chart, 0700); err != nil {
		return kubernetes.Result{}, errors.New("create chart snapshot")
	}
	code := snapshot(ctx, inputChart, chart)
	if code != "" {
		if ctx.Err() != nil {
			return kubernetes.Result{}, ctx.Err()
		}
		return diagnostic(code, "Chart preflight failed; use a bounded local chart without symlinks, dependencies, CRDs, lookup or nondeterministic template functions."), nil
	}
	args := []string{"template", opts.Release, chart, "--namespace", model.DefaultNamespace, "--kube-version", KubernetesVersion, "--dry-run=client"}
	// Helm's built-in versioned API set is used; no cluster discovery or custom
	// capabilities are permitted in this increment.
	total := 0
	for i, path := range opts.Values {
		data, err := readRegular(path)
		total += len(data)
		if err != nil || total > maxInput {
			return diagnostic("HELM_VALUES_INVALID", "Values must be bounded regular files without symlinks."), nil
		}
		target := filepath.Join(root, "values-"+strconv.Itoa(i)+".yaml")
		if err := os.WriteFile(target, data, 0600); err != nil {
			return kubernetes.Result{}, errors.New("snapshot Helm values")
		}
		args = append(args, "--values", target)
	}
	override := opts.Binary
	if override == "" {
		override = os.Getenv("GRILLO_HELM_BINARY")
	}
	binary, err := runtimeassets.Resolve("helm", override)
	if err != nil {
		return diagnostic("HELM_UNAVAILABLE", "Provide the installed pinned Helm helper or an explicit GRILLO_HELM_BINARY override."), nil
	}
	binary, err = exec.LookPath(binary)
	if err != nil {
		return diagnostic("HELM_UNAVAILABLE", "Provide official Helm "+Version+" through the installed helper or an explicit GRILLO_HELM_BINARY override."), nil
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		return diagnostic("HELM_UNAVAILABLE", "Cannot resolve Helm executable."), nil
	}
	env := []string{"HOME=" + root, "HELM_CONFIG_HOME=" + filepath.Join(root, "config"), "HELM_CACHE_HOME=" + filepath.Join(root, "cache"), "HELM_DATA_HOME=" + filepath.Join(root, "data"), "HELM_PLUGINS=" + filepath.Join(root, "plugins"), "KUBECONFIG=" + filepath.Join(root, "no-kubeconfig"), "LANG=C", "LC_ALL=C"}
	version, err := run(ctx, binary, []string{"version", "--template", "{{.Version}}"}, env, root, 1024)
	if err != nil || strings.TrimSpace(string(version)) != Version {
		if ctx.Err() != nil {
			return kubernetes.Result{}, ctx.Err()
		}
		return diagnostic("HELM_VERSION", "Helm version must match the pinned renderer "+Version+"."), nil
	}
	output, err := run(ctx, binary, args, env, root, maxOutput)
	if err != nil {
		if ctx.Err() != nil {
			return kubernetes.Result{}, ctx.Err()
		}
		return diagnostic("HELM_RENDER_FAILED", "Helm rendering failed, timed out, or exceeded output limits; tool output was suppressed."), nil
	}
	if hasHooks(output) {
		return diagnostic("HELM_HOOK_UNSUPPORTED", "Helm hooks and test hooks cannot be executed as ordinary workloads."), nil
	}
	result, err := kubernetes.Compile(ctx, output, kubernetes.Options{Path: filepath.Join(opts.Chart, "rendered.yaml"), AllowDegraded: opts.AllowDegraded})
	if err != nil {
		return diagnostic("HELM_MANIFEST_INVALID", "Rendered manifests could not be parsed; raw output was suppressed."), nil
	}
	// The release, not a private temporary filename, owns application identity.
	result.Application.Identity.Name = opts.Release
	result.Application.Source = &source.Source{Kind: source.KindHelm, Path: opts.Chart}
	// The caller replaces unresolved references with opaque store versions on
	// apply. Content-derived compiler fingerprints must not enter public IR.
	for i := range result.Application.Secrets {
		result.Application.Secrets[i].Version = "unresolved-offline"
	}
	return result, nil
}

func readRegular(path string) ([]byte, error) {
	// Reject symlinks in every path component, not only the final filename.
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	for p := absolute; ; p = filepath.Dir(p) {
		st, err := os.Lstat(p)
		if err != nil || st.Mode()&os.ModeSymlink != 0 || (p == absolute && !st.Mode().IsRegular()) {
			return nil, errors.New("unsafe input path")
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	root, err := os.OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := openInput(root, filepath.Base(absolute))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil || len(b) > maxFile {
		return nil, errors.New("input limit")
	}
	return b, nil
}

func snapshot(ctx context.Context, from, to string) string {
	st, err := os.Lstat(from)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return "HELM_CHART_INVALID"
	}
	root, err := os.OpenRoot(from)
	if err != nil {
		return "HELM_CHART_INVALID"
	}
	defer root.Close()
	count, total := 0, 0
	code := "HELM_CHART_INVALID"
	err = fs.WalkDir(root.FS(), ".", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		count++
		if count > maxFiles {
			code = "HELM_INPUT_LIMIT"
			return errors.New("limit")
		}
		rel := filepath.FromSlash(path)
		if entry.Type()&os.ModeSymlink != 0 || (!entry.IsDir() && !entry.Type().IsRegular()) {
			return errors.New("unsafe file type")
		}
		// Remote JSON-schema references can cause implicit network access.
		if rel == "crds" || rel == "charts" || rel == "values.schema.json" {
			code = "HELM_FEATURE_UNSUPPORTED"
			return errors.New("unsupported directory")
		}
		dest := filepath.Join(to, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		f, err := openInput(root, path)
		if err != nil {
			return err
		}
		st, err := f.Stat()
		if err != nil || !st.Mode().IsRegular() {
			f.Close()
			return errors.New("not a regular file")
		}
		data, err := io.ReadAll(io.LimitReader(f, maxFile+1))
		f.Close()
		if err != nil || len(data) > maxFile {
			return errors.New("input limit")
		}
		total += len(data)
		if total > maxInput {
			code = "HELM_INPUT_LIMIT"
			return errors.New("limit")
		}
		if strings.HasPrefix(rel, "templates"+string(filepath.Separator)) && unsafeFunctions.Match(data) {
			code = "HELM_TEMPLATE_UNSUPPORTED"
			return errors.New("unsupported template")
		}
		if rel == "Chart.yaml" {
			var meta struct {
				APIVersion   string    `yaml:"apiVersion"`
				Name         string    `yaml:"name"`
				Dependencies yaml.Node `yaml:"dependencies"`
			}
			if yaml.Unmarshal(data, &meta) != nil || meta.Name == "" || meta.APIVersion != "v2" {
				return errors.New("invalid chart metadata")
			}
			if meta.Dependencies.Kind != 0 {
				code = "HELM_DEPENDENCIES_UNSUPPORTED"
				return errors.New("dependencies")
			}
		}
		return os.WriteFile(dest, data, 0600)
	})
	if err != nil {
		return code
	}
	if _, err := os.Stat(filepath.Join(to, "Chart.yaml")); err != nil {
		return code
	}
	return ""
}

func hasHooks(data []byte) bool {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var walk func(*yaml.Node) bool
	walk = func(n *yaml.Node) bool {
		if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == "helm.sh/hook" {
					return true
				}
			}
		}
		for _, child := range n.Content {
			if walk(child) {
				return true
			}
		}
		return false
	}
	for {
		var n yaml.Node
		if decoder.Decode(&n) != nil {
			return false
		}
		if walk(&n) {
			return true
		}
	}
}

type boundedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *boundedBuffer) Bytes() []byte { return b.buffer.Bytes() }
func (b *boundedBuffer) Len() int      { return b.buffer.Len() }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("tool output limit")
	}
	return b.buffer.Write(p)
}
func run(ctx context.Context, binary string, args, env []string, dir string, limit int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.WaitDelay = time.Second
	stdout := &boundedBuffer{limit: limit}
	stderr := &boundedBuffer{limit: 64 << 10}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return nil, errors.New("external renderer failed")
	}
	return stdout.Bytes(), nil
}
