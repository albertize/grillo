// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// HTTP limits.
const (
	DefaultMaxManifestBytes = 8 << 20
	DefaultMaxBlobBytes     = 4 << 30
	DefaultMaxTokenBytes    = 1 << 20
	DefaultMaxRedirects     = 5
	DefaultHTTPTimeout      = 60 * time.Second
)

var manifestAccept = []string{
	MediaTypeOCIManifest,
	MediaTypeOCIIndex,
	MediaTypeDockerManifest,
	MediaTypeDockerList,
}

// RegistryClient talks to an OCI Distribution API registry.
type RegistryClient struct {
	Transport        http.RoundTripper
	Insecure         []string // registries allowed over plain HTTP
	Username         string
	Password         string
	MaxManifestBytes int64
	MaxBlobBytes     int64
	MaxRedirects     int
	UserAgent        string

	mu     sync.Mutex
	tokens map[string]string
}

// NewRegistryClient returns a client with safe defaults.
func NewRegistryClient() *RegistryClient {
	return &RegistryClient{
		MaxManifestBytes: DefaultMaxManifestBytes,
		MaxBlobBytes:     DefaultMaxBlobBytes,
		MaxRedirects:     DefaultMaxRedirects,
		UserAgent:        "grillo/0.0 (+https://grillo.local)",
		tokens:           make(map[string]string),
	}
}

// FetchManifest fetches a manifest or index by tag or digest.
func (c *RegistryClient) FetchManifest(ctx context.Context, ref Reference) ([]byte, string, error) {
	endpoint := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", c.scheme(ref.Registry), ref.RegistryHost(), ref.Repository, ref.ManifestRef())
	resp, err := c.do(ctx, http.MethodGet, endpoint, ref.Registry, c.scope(ref.Repository), manifestAccept)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", httpError("manifest", resp)
	}
	data, err := readLimited(resp.Body, c.maxManifestBytes())
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// OpenBlob opens a blob stream. The caller must verify and store it.
func (c *RegistryClient) OpenBlob(ctx context.Context, ref Reference, digest string) (io.ReadCloser, int64, error) {
	endpoint := fmt.Sprintf("%s://%s/v2/%s/blobs/%s", c.scheme(ref.Registry), ref.RegistryHost(), ref.Repository, digest)
	resp, err := c.do(ctx, http.MethodGet, endpoint, ref.Registry, c.scope(ref.Repository), nil)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, 0, httpError("blob", resp)
	}
	return resp.Body, resp.ContentLength, nil
}

func (c *RegistryClient) do(ctx context.Context, method, endpoint, registry, scope string, accept []string) (*http.Response, error) {
	newRequest := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", c.userAgent())
		if len(accept) > 0 {
			req.Header.Set("Accept", strings.Join(accept, ", "))
		}
		return req, nil
	}
	req, err := newRequest()
	if err != nil {
		return nil, err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	challenge := resp.Header.Get("WWW-Authenticate")
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
	authorization, err := c.authorize(ctx, challenge, registry, scope)
	if err != nil {
		return nil, err
	}
	retry, err := newRequest()
	if err != nil {
		return nil, err
	}
	retry.Header.Set("Authorization", authorization)
	resp, err = c.client().Do(retry)
	if err != nil {
		return nil, err
	}
	scheme, params := parseChallenge(challenge)
	if resp.StatusCode != http.StatusUnauthorized || !strings.EqualFold(scheme, "bearer") {
		return resp, nil
	}
	// A cached opaque token may expire or be revoked. Invalidate only the
	// rejected value (not a concurrent replacement), then refresh once. Never
	// follow a new realm from this second response or retry authentication forever.
	key := bearerCacheKey(params, scope)
	c.mu.Lock()
	if c.tokens[key] == strings.TrimPrefix(authorization, "Bearer ") {
		delete(c.tokens, key)
	}
	c.mu.Unlock()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
	authorization, err = c.authorize(ctx, challenge, registry, scope)
	if err != nil {
		return nil, err
	}
	retry, err = newRequest()
	if err != nil {
		return nil, err
	}
	retry.Header.Set("Authorization", authorization)
	return c.client().Do(retry)
}

func (c *RegistryClient) authorize(ctx context.Context, challenge, registry, scope string) (string, error) {
	if challenge == "" {
		return "", errors.New("oci: registry returned 401 without a challenge")
	}
	scheme, params := parseChallenge(challenge)
	switch strings.ToLower(scheme) {
	case "basic":
		if c.Username == "" && c.Password == "" {
			return "", errors.New("oci: registry requires credentials")
		}
		raw := base64.StdEncoding.EncodeToString([]byte(c.Username + ":" + c.Password))
		return "Basic " + raw, nil
	case "bearer":
		token, err := c.bearerToken(ctx, params, scope)
		if err != nil {
			return "", err
		}
		return "Bearer " + token, nil
	default:
		return "", fmt.Errorf("oci: unsupported authentication scheme %q", scheme)
	}
}

func (c *RegistryClient) bearerToken(ctx context.Context, params map[string]string, scope string) (string, error) {
	realm := params["realm"]
	if realm == "" {
		return "", errors.New("oci: bearer challenge has no realm")
	}
	key := bearerCacheKey(params, scope)
	c.mu.Lock()
	if token, ok := c.tokens[key]; ok {
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	parsed, err := url.Parse(realm)
	if err != nil {
		return "", fmt.Errorf("oci: invalid token realm: %w", err)
	}
	query := parsed.Query()
	if service := params["service"]; service != "" {
		query.Set("service", service)
	}
	if scope == "" {
		scope = params["scope"]
	}
	if scope != "" {
		query.Set("scope", scope)
	}
	parsed.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", c.userAgent())
	if c.Username != "" || c.Password != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("oci: token endpoint returned %s", resp.Status)
	}
	data, err := readLimited(resp.Body, DefaultMaxTokenBytes)
	if err != nil {
		return "", err
	}
	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", fmt.Errorf("oci: decode token response: %w", err)
	}
	token := payload.Token
	if token == "" {
		token = payload.AccessToken
	}
	if token == "" {
		return "", errors.New("oci: token response contained no token")
	}
	c.mu.Lock()
	c.tokens[key] = token
	c.mu.Unlock()
	return token, nil
}

func bearerCacheKey(params map[string]string, scope string) string {
	if scope == "" {
		scope = params["scope"]
	}
	// Include service so tokens for distinct audiences are not mixed.
	return params["realm"] + "|" + params["service"] + "|" + scope
}

func (c *RegistryClient) client() *http.Client {
	maxRedirects := c.MaxRedirects
	if maxRedirects <= 0 {
		maxRedirects = DefaultMaxRedirects
	}
	return &http.Client{
		Transport: c.Transport,
		Timeout:   DefaultHTTPTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("oci: stopped after %d redirects", maxRedirects)
			}
			// Never forward credentials to a different host.
			if len(via) > 0 && !sameHost(req.URL, via[0].URL) {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}
}

func (c *RegistryClient) scheme(registry string) string {
	for _, host := range c.Insecure {
		if host == registry {
			return "http"
		}
	}
	return "https"
}

func (c *RegistryClient) scope(repository string) string {
	return "repository:" + repository + ":pull"
}

func (c *RegistryClient) maxManifestBytes() int64 {
	if c.MaxManifestBytes <= 0 {
		return DefaultMaxManifestBytes
	}
	return c.MaxManifestBytes
}

func (c *RegistryClient) userAgent() string {
	if c.UserAgent == "" {
		return "grillo/0.0"
	}
	return c.UserAgent
}

func sameHost(a, b *url.URL) bool {
	return strings.EqualFold(a.Host, b.Host) && strings.EqualFold(a.Scheme, b.Scheme)
}

func readLimited(r io.Reader, max int64) ([]byte, error) {
	limited := io.LimitReader(r, max+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("oci: response exceeds %d bytes", max)
	}
	return data, nil
}

func httpError(what string, resp *http.Response) error {
	body, _ := readLimited(resp.Body, 4096)
	return fmt.Errorf("oci: %s request failed: %s: %s", what, resp.Status, strings.TrimSpace(string(body)))
}

// parseChallenge parses an auth challenge such as
// `Bearer realm="https://auth",service="registry",scope="repository:x:pull"`.
func parseChallenge(header string) (string, map[string]string) {
	scheme, rest, _ := strings.Cut(header, " ")
	params := map[string]string{}
	for _, part := range splitChallengeParams(rest) {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		params[strings.ToLower(strings.TrimSpace(key))] = value
	}
	return scheme, params
}

func splitChallengeParams(s string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			current.WriteRune(r)
		case r == ',' && !inQuote:
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}
