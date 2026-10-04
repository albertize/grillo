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
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/albertize/grillo/internal/api"
	"github.com/albertize/grillo/internal/observe"
)

//go:embed assets/index.html assets/app.js
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
}

// New returns a bridge with fresh bootstrap and session tokens.
func New(core Core) *Bridge {
	return &Bridge{core: core, bootstrap: randomToken(), session: randomToken()}
}

// BootstrapToken is the one-time token placed in the URL fragment.
func (b *Bridge) BootstrapToken() string { return b.bootstrap }

// URL returns the local URL to open, including the bootstrap fragment.
func (b *Bridge) URL(addr string) string {
	return fmt.Sprintf("http://%s/#token=%s", addr, b.bootstrap)
}

// ListenAndServe runs the console on addr until ctx is canceled.
func (b *Bridge) ListenAndServe(ctx context.Context, addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: b.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Handler returns the console HTTP handler.
func (b *Bridge) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", b.handleIndex)
	mux.HandleFunc("GET /app.js", b.handleAsset("assets/app.js", "application/javascript"))
	mux.HandleFunc("POST /session", b.handleSession)
	mux.HandleFunc("GET /v1/applications", b.auth(b.handleApplications))
	mux.HandleFunc("GET /v1/applications/{id}", b.auth(b.handleStatus))
	mux.HandleFunc("GET /v1/logs", b.auth(b.handleLogs))
	mux.HandleFunc("GET /v1/events", b.auth(b.handleEvents))
	return b.secure(mux)
}

func (b *Bridge) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHost(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if origin := r.Header.Get("Origin"); origin != "" && !loopbackOrigin(origin) {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'none'")
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
	serveAsset(w, "assets/index.html", "text/html; charset=utf-8")
}

func (b *Bridge) handleAsset(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		serveAsset(w, name, contentType)
	}
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
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&request); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if subtle.ConstantTimeCompare([]byte(request.Token), []byte(b.bootstrap)) != 1 {
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
	records, err := b.core.ListLogs(r.Context(), 0, r.URL.Query().Get("resource"), r.URL.Query().Get("container"))
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"records": records})
}

func (b *Bridge) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	stream, err := b.core.Events(r.Context(), 0)
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
		fmt.Fprintln(w, scanner.Text())
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

func loopbackOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return loopbackHost(parsed.Host)
}
