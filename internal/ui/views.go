//go:build linux

// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/albertize/grillo/internal/observe"
)

type viewer interface {
	View(context.Context, string) (observe.ApplicationView, error)
}
type execCore interface {
	Exec(context.Context, string, string, []string) (int, string, string, error)
}

func (b *Bridge) handleView(w http.ResponseWriter, r *http.Request) {
	core, ok := b.core.(viewer)
	if !ok {
		http.Error(w, "resource inspection unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	view, err := core.View(ctx, r.PathValue("id"))
	if err != nil {
		http.Error(w, "resource inspection unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, view)
}

func (b *Bridge) handleExec(w http.ResponseWriter, r *http.Request) {
	// Mutations require a browser same-origin request, not merely a cookie.
	if !sameOrigin(r.Header.Get("Origin"), r.Host) {
		http.Error(w, "same origin required", http.StatusForbidden)
		return
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		http.Error(w, "JSON required", http.StatusUnsupportedMediaType)
		return
	}
	core, ok := b.core.(execCore)
	if !ok {
		http.Error(w, "exec unavailable", http.StatusServiceUnavailable)
		return
	}
	select {
	case b.execs <- struct{}{}:
		defer func() { <-b.execs }()
	default:
		http.Error(w, "too many exec requests", http.StatusTooManyRequests)
		return
	}
	var request struct {
		Application string   `json:"application"`
		Container   string   `json:"container"`
		Args        []string `json:"args"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || request.Application == "" || request.Container == "" || len(request.Args) == 0 || len(request.Args) > 128 {
		http.Error(w, "invalid exec request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	code, stdout, stderr, err := core.Exec(ctx, request.Application, request.Container, request.Args)
	if err != nil {
		http.Error(w, "exec failed or timed out; check the selected container", http.StatusBadGateway)
		return
	}
	const limit = 1 << 20
	truncated := len(stdout)+len(stderr) > limit
	if len(stdout) > limit {
		stdout = stdout[:limit]
	}
	if len(stderr) > limit-len(stdout) {
		stderr = stderr[:limit-len(stdout)]
	}
	writeJSON(w, struct {
		ExitCode  int    `json:"exitCode"`
		Stdout    string `json:"stdout"`
		Stderr    string `json:"stderr"`
		Truncated bool   `json:"truncated"`
	}{code, stdout, stderr, truncated})
}
