//go:build linux

// SPDX-License-Identifier: Apache-2.0

// Package ui is the local web console bridge. It binds an HTTP server to
// loopback, requires an explicit session cookie obtained from a bootstrap token,
// checks Host and Origin, serves a CSP with no external resources, and renders
// data as text nodes. It is never a host file server.
package ui

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/albertize/grillo/internal/api"
	"github.com/albertize/grillo/internal/observe"
)

//go:embed assets
var assets embed.FS

// Core is the data the console reads. The API client satisfies it.
type Core interface {
	Applications(ctx context.Context) ([]string, error)
	Status(ctx context.Context, application string) ([]api.ContainerStatus, error)
	ListLogs(ctx context.Context, since uint64, resource, container string) ([]observe.LogRecord, error)
	Events(ctx context.Context, since uint64) (io.ReadCloser, error)
}

const sessionCookie = "grillo_session"

// Bridge serves the console.
type Bridge struct {
	core      Core
	bootstrap string
	session   string
	mu        sync.Mutex
	used      bool
	expires   time.Time
	now       func() time.Time
	streams   chan struct{}
	execs     chan struct{}
	terminals map[string]*terminalSession // guarded by mu; lifetime belongs to the output request
}

// New returns a bridge with fresh bootstrap and session tokens.
func New(core Core) *Bridge {
	return &Bridge{core: core, bootstrap: randomToken(), session: randomToken(), expires: time.Now().Add(5 * time.Minute), now: time.Now, streams: make(chan struct{}, 8), execs: make(chan struct{}, 4), terminals: make(map[string]*terminalSession)}
}

// BootstrapToken is the one-time token placed in the URL fragment.
func (b *Bridge) BootstrapToken() string { return b.bootstrap }

// URL returns the local URL to open, including the bootstrap fragment.
func (b *Bridge) URL(addr string) string {
	return fmt.Sprintf("http://%s/#token=%s", addr, b.bootstrap)
}

// ListenAndServe runs the console on addr until ctx is canceled.
func (b *Bridge) ListenAndServe(ctx context.Context, addr string) error {
	listener, err := Listen(addr)
	if err != nil {
		return err
	}
	return b.Serve(ctx, listener)
}

// Listen rejects public bindings before opening a socket. Callers can report
// the assigned port before serving, including when port zero was requested.
func Listen(addr string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return nil, errors.New("UI requires a numeric loopback listen address")
	}
	return net.Listen("tcp", addr)
}

// Serve owns listener until it returns, but never owns workload lifetime.
func (b *Bridge) Serve(ctx context.Context, listener net.Listener) error {
	defer listener.Close()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok || !addr.IP.IsLoopback() {
		return errors.New("UI requires a loopback TCP listener")
	}
	server := &http.Server{Handler: b.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
		case <-done:
			return
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if server.Shutdown(shutdownCtx) != nil {
			_ = server.Close()
		}
	}()
	err := server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Handler returns the console HTTP handler.
func (b *Bridge) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", b.handleIndex)
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, _ *http.Request) {
		serveAsset(w, "assets/generated/icons/favicon.ico", "image/vnd.microsoft.icon")
	})
	mux.HandleFunc("GET /assets/", b.handleStatic)
	mux.HandleFunc("POST /session", b.handleSession)
	mux.HandleFunc("GET /v1/applications", b.auth(b.handleApplications))
	mux.HandleFunc("GET /v1/applications/{id}", b.auth(b.handleStatus))
	mux.HandleFunc("GET /v1/applications/{id}/view", b.auth(b.handleView))
	mux.HandleFunc("POST /v1/exec", b.auth(b.handleExec))
	mux.HandleFunc("POST /v1/terminal", b.auth(b.handleTerminal))
	mux.HandleFunc("POST /v1/terminal/{id}/input", b.auth(b.handleTerminalInput))
	mux.HandleFunc("POST /v1/terminal/{id}/close", b.auth(b.handleTerminalClose))
	mux.HandleFunc("GET /v1/logs", b.auth(b.handleLogs))
	mux.HandleFunc("GET /v1/events", b.auth(b.handleEvents))
	return b.secure(mux)
}

func (b *Bridge) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Bound response writes too, including captured exec output to a client
		// that stops reading. SSE refreshes this deadline for each bounded line.
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(45 * time.Second))
		if !loopbackHost(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(10 * time.Second))
			if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r.Host) {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func (b *Bridge) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(b.session)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (b *Bridge) handleIndex(w http.ResponseWriter, _ *http.Request) {
	data, err := assets.ReadFile("assets/index.html")
	if err != nil {
		http.Error(w, "console unavailable", http.StatusInternalServerError)
		return
	}
	// Fresh per-document style authorization for the terminal's dynamic renderer.
	// No script nonce and no unsafe-inline permission are introduced.
	nonce := randomToken()
	w.Header().Set("Content-Security-Policy", strings.Replace(w.Header().Get("Content-Security-Policy"), "style-src 'self'", "style-src 'self' 'nonce-"+nonce+"'", 1))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, strings.Replace(string(data), `name="terminal-style-nonce" content=""`, `name="terminal-style-nonce" content="`+nonce+`"`, 1))
}

// Static reads are confined to immutable embedded build output, not the host
// filesystem. No directory listing, source maps or arbitrary file access.
func (b *Bridge) handleStatic(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if !strings.HasPrefix(name, "assets/generated/") {
		http.NotFound(w, r)
		return
	}
	contentType := mime.TypeByExtension(path.Ext(name))
	if path.Ext(name) == ".webmanifest" {
		contentType = "application/manifest+json"
	}
	if contentType == "" {
		http.NotFound(w, r)
		return
	}
	serveAsset(w, name, contentType)
}

func serveAsset(w http.ResponseWriter, name, contentType string) {
	data, err := assets.ReadFile(name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(data)
}

func (b *Bridge) handleSession(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Token string `json:"token"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	accepted := !b.used && b.now().Before(b.expires) && subtle.ConstantTimeCompare([]byte(request.Token), []byte(b.bootstrap)) == 1
	if accepted {
		b.used = true
	}
	b.mu.Unlock()
	if !accepted {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: b.session, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (b *Bridge) handleApplications(w http.ResponseWriter, r *http.Request) {
	applications, err := b.core.Applications(r.Context())
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"applications": applications})
}

func (b *Bridge) handleStatus(w http.ResponseWriter, r *http.Request) {
	containers, err := b.core.Status(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"containers": containers})
}

func (b *Bridge) handleLogs(w http.ResponseWriter, r *http.Request) {
	since, err := cursor(r)
	if err != nil {
		http.Error(w, "invalid cursor", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	records, err := b.core.ListLogs(ctx, since, r.URL.Query().Get("resource"), r.URL.Query().Get("container"))
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	// Keep each browser response bounded even when the retained spool is large.
	if len(records) > 200 {
		records = records[:200]
	}
	writeJSON(w, map[string]any{"records": records})
}

func (b *Bridge) handleEvents(w http.ResponseWriter, r *http.Request) {
	since, err := cursor(r)
	if err != nil {
		http.Error(w, "invalid cursor", http.StatusBadRequest)
		return
	}
	select {
	case b.streams <- struct{}{}:
		defer func() { <-b.streams }()
	default:
		http.Error(w, "too many streams", http.StatusTooManyRequests)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	stream, err := b.core.Events(ctx, since)
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	defer stream.Close()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	// The core already emits SSE frames; copy them through, flushing per frame.
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Second))
		line := scanner.Text()
		// EventSource's onmessage handles all runtime kinds; the JSON payload
		// retains the original kind, including events.gap.
		if strings.HasPrefix(line, "event:") {
			line = "event: message"
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return
		}
		if scanner.Text() == "" {
			flusher.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func randomToken() string {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

func loopbackHost(host string) bool {
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	name = strings.Trim(name, "[]")
	return name == "127.0.0.1" || name == "localhost" || name == "::1"
}

func cursor(r *http.Request) (uint64, error) {
	value := r.Header.Get("Last-Event-ID")
	if value == "" {
		value = r.URL.Query().Get("since")
	}
	if value == "" {
		return 0, nil
	}
	return strconv.ParseUint(value, 10, 64)
}

func sameOrigin(origin, host string) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" && parsed.Host == host && parsed.User == nil && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}
