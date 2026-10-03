//go:build linux

// SPDX-License-Identifier: Apache-2.0

// Package api implements the local Unix-socket HTTP API and its client. Public
// DTOs are separate from private structures; errors never carry raw stderr or
// secrets; peer credentials are validated on every connection.
package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"grillo.local/grillo/internal/model"
	"grillo.local/grillo/internal/observe"
	"grillo.local/grillo/internal/reconcile"
)

// Error is the public error DTO.
type Error struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Resource  string            `json:"resource,omitempty"`
	Retryable bool              `json:"retryable,omitempty"`
	Details   map[string]string `json:"details,omitempty"`
}

// VersionInfo describes the daemon.
type VersionInfo struct {
	Version    string `json:"version"`
	APIVersion string `json:"apiVersion"`
}

// Health reports daemon health.
type Health struct {
	Status    string `json:"status"`
	UptimeSec int64  `json:"uptimeSec"`
}

// Operation is an accepted asynchronous mutation.
type Operation struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Application string    `json:"application,omitempty"`
	State       string    `json:"state"`
	Error       *Error    `json:"error,omitempty"`
	StartedAt   time.Time `json:"startedAt"`
	FinishedAt  time.Time `json:"finishedAt,omitempty"`
}

// ContainerStatus is the public observed state of one container.
type ContainerStatus struct {
	Container string `json:"container"`
	State     string `json:"state"`
	ExitCode  int    `json:"exitCode,omitempty"`
}

// Core is the subset of the runtime the API uses.
type Core interface {
	Apply(ctx context.Context, desired model.Application) (reconcile.Result, error)
	Down(ctx context.Context, application string, removeVolumes bool) (reconcile.Result, error)
	Status(ctx context.Context, application string) ([]ContainerStatus, error)
	Exec(ctx context.Context, application, container string, args []string, maxOutput int64) (int, string, string, error)
	ExecStream(ctx context.Context, application, container string, args []string, stdout, stderr io.Writer) (int, error)
	Applications(ctx context.Context) ([]string, error)
	Restart(ctx context.Context, application string) error
}

// Options configures the API server.
type Options struct {
	Core    Core
	Events  *observe.EventStore
	Logs    *observe.LogSpool
	Version string
	// Shutdown stops the daemon (not applications). May be nil.
	Shutdown func()
	// Images is the optional image inventory and build surface.
	Images ImageManager
	// PeerUID is the only UID allowed to connect. Defaults to the current user.
	PeerUID uint32
	// MaxBodyBytes bounds request bodies (default 8 MiB).
	MaxBodyBytes int64
}

// Server serves the local API.
type Server struct {
	opts    Options
	ops     *Operations
	started time.Time
}

// NewServer builds the API handler.
func NewServer(opts Options) *Server {
	return &Server{opts: opts, ops: NewOperations(), started: time.Now()}
}

// Operations exposes the operation registry.
func (s *Server) Operations() *Operations { return s.ops }

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/version", s.handleVersion)
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	mux.HandleFunc("GET /v1/applications", s.handleApplications)
	mux.HandleFunc("GET /v1/applications/{id}", s.handleStatus)
	mux.HandleFunc("POST /v1/applications/{id}/restart", s.handleRestart)
	mux.HandleFunc("POST /v1/applications", s.handleApply)
	mux.HandleFunc("POST /v1/applications/{id}/down", s.handleDown)
	mux.HandleFunc("POST /v1/exec", s.handleExec)
	mux.HandleFunc("GET /v1/operations/{id}", s.handleOperation)
	mux.HandleFunc("POST /v1/operations/{id}/cancel", s.handleCancel)
	mux.HandleFunc("GET /v1/events", s.handleEvents)
	mux.HandleFunc("GET /v1/images", s.handleImages)
	mux.HandleFunc("GET /v1/images/inspect", s.handleInspectImage)
	mux.HandleFunc("POST /v1/images/prune", s.handlePruneImages)
	mux.HandleFunc("POST /v1/images/pin", s.handlePinImage)
	mux.HandleFunc("POST /v1/build", s.handleBuild)
	mux.HandleFunc("GET /v1/logs", s.handleLogs)
	mux.HandleFunc("POST /v1/shutdown", s.handleShutdown)
	return mux
}

func (s *Server) maxBody() int64 {
	if s.opts.MaxBodyBytes > 0 {
		return s.opts.MaxBodyBytes
	}
	return 8 << 20
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, VersionInfo{Version: s.opts.Version, APIVersion: "v1"})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, Health{Status: "ok", UptimeSec: int64(time.Since(s.started).Seconds())})
}

func (s *Server) handleApply(w http.ResponseWriter, r *http.Request) {
	var app model.Application
	if err := decodeJSON(w, r, s.maxBody(), &app); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if app.Identity.Name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "application name is required")
		return
	}
	if s.opts.Core == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "no core configured")
		return
	}
	operation := s.ops.Start("apply", app.Identity.Name, func(ctx context.Context) error {
		_, err := s.opts.Core.Apply(ctx, app)
		return err
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"operationId": operation.ID})
}

func (s *Server) handleApplications(w http.ResponseWriter, r *http.Request) {
	if s.opts.Core == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "no core configured")
		return
	}
	applications, err := s.opts.Core.Applications(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"applications": applications})
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.opts.Core == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "no core configured")
		return
	}
	operation := s.ops.Start("restart", id, func(ctx context.Context) error {
		return s.opts.Core.Restart(ctx, id)
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"operationId": operation.ID})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.opts.Core == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "no core configured")
		return
	}
	containers, err := s.opts.Core.Status(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "status_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"application": id, "containers": containers})
}

func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Application string   `json:"application"`
		Container   string   `json:"container"`
		Args        []string `json:"args"`
	}
	if err := decodeJSON(w, r, s.maxBody(), &request); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if request.Application == "" || request.Container == "" || len(request.Args) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "application, container, and args are required")
		return
	}
	if s.opts.Core == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "no core configured")
		return
	}
	if r.URL.Query().Get("stream") == "true" {
		s.handleExecStream(w, r, request.Application, request.Container, request.Args)
		return
	}
	code, stdout, stderr, err := s.opts.Core.Exec(r.Context(), request.Application, request.Container, request.Args, 1<<20)
	if err != nil {
		writeError(w, http.StatusBadGateway, "exec_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"exitCode": code, "stdout": stdout, "stderr": stderr})
}

// handleExecStream streams exec output as SSE frames so the CLI can show output
// live. stdout and stderr data are base64-encoded; a final event carries the
// exit code.
func (s *Server) handleExecStream(w http.ResponseWriter, r *http.Request, application, container string, args []string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	stdout := &sseWriter{w: w, flusher: flusher, event: "stdout"}
	stderr := &sseWriter{w: w, flusher: flusher, event: "stderr"}
	code, err := s.opts.Core.ExecStream(r.Context(), application, container, args, stdout, stderr)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", base64.StdEncoding.EncodeToString([]byte(err.Error())))
	} else {
		fmt.Fprintf(w, "event: exit\ndata: %d\n\n", code)
	}
	flusher.Flush()
}

type sseWriter struct {
	w       io.Writer
	flusher http.Flusher
	event   string
}

func (s *sseWriter) Write(p []byte) (int, error) {
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", s.event, base64.StdEncoding.EncodeToString(p)); err != nil {
		return 0, err
	}
	s.flusher.Flush()
	return len(p), nil
}

func (s *Server) handleDown(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	removeVolumes := r.URL.Query().Get("volumes") == "true"
	if s.opts.Core == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "no core configured")
		return
	}
	operation := s.ops.Start("down", id, func(ctx context.Context) error {
		_, err := s.opts.Core.Down(ctx, id, removeVolumes)
		return err
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"operationId": operation.ID})
}

func (s *Server) handleOperation(w http.ResponseWriter, r *http.Request) {
	operation, ok := s.ops.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "operation not found")
		return
	}
	writeJSON(w, http.StatusOK, operation)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	if !s.ops.Cancel(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "not_found", "operation not found or already finished")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if s.opts.Events == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "events are unavailable")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}
	since := sinceParam(r)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	stream := s.opts.Events.Follow(r.Context(), since, 200*time.Millisecond)
	for event := range stream {
		data, err := json.Marshal(event)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, event.Kind, data)
		flusher.Flush()
	}
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if s.opts.Logs == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "logs are unavailable")
		return
	}
	since := sinceParam(r)
	resource := r.URL.Query().Get("resource")
	container := r.URL.Query().Get("container")
	if r.URL.Query().Get("follow") == "true" {
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "internal", "streaming unsupported")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		stream := s.opts.Logs.Follow(r.Context(), since, resource, container, 200*time.Millisecond)
		for record := range stream {
			data, _ := json.Marshal(record)
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", record.Sequence, data)
			flusher.Flush()
		}
		return
	}
	records, err := s.opts.Logs.List(since, 0, resource, container)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": records})
}

func (s *Server) handleShutdown(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusAccepted)
	if s.opts.Shutdown != nil {
		go s.opts.Shutdown()
	}
}

func sinceParam(r *http.Request) uint64 {
	if header := r.Header.Get("Last-Event-ID"); header != "" {
		if value, err := strconv.ParseUint(header, 10, 64); err == nil {
			return value
		}
	}
	if value := r.URL.Query().Get("since"); value != "" {
		if parsed, err := strconv.ParseUint(value, 10, 64); err == nil {
			return parsed
		}
	}
	return 0
}

func decodeJSON(w http.ResponseWriter, r *http.Request, limit int64, out any) error {
	body := http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(out); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return fmt.Errorf("request body exceeds %d bytes", limit)
		}
		if errors.Is(err, io.EOF) {
			return errors.New("empty request body")
		}
		return fmt.Errorf("invalid JSON: %v", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, Error{Code: code, Message: message})
}

// Operations tracks asynchronous mutations so a client disconnect does not
// cancel accepted work. Operations derive from the daemon lifetime context, not
// the request context; they are canceled only explicitly or on shutdown.
type Operations struct {
	mu      sync.Mutex
	ops     map[string]*Operation
	cancels map[string]context.CancelFunc
	next    uint64
	base    context.Context
}

// NewOperations returns an empty registry bound to the daemon lifetime.
func NewOperations() *Operations {
	return &Operations{ops: map[string]*Operation{}, cancels: map[string]context.CancelFunc{}, base: context.Background()}
}

// Start runs fn in the background. A disconnected client does not cancel it.
func (o *Operations) Start(kind, application string, fn func(context.Context) error) *Operation {
	o.mu.Lock()
	o.next++
	id := fmt.Sprintf("op-%d", o.next)
	operation := &Operation{ID: id, Kind: kind, Application: application, State: "running", StartedAt: time.Now().UTC()}
	ctx, cancel := context.WithCancel(o.base)
	o.ops[id] = operation
	o.cancels[id] = cancel
	o.mu.Unlock()

	go func() {
		err := fn(ctx)
		o.mu.Lock()
		defer o.mu.Unlock()
		operation.FinishedAt = time.Now().UTC()
		switch {
		case ctx.Err() != nil:
			operation.State = "canceled"
		case err == nil:
			operation.State = "succeeded"
		default:
			operation.State = "failed"
			operation.Error = &Error{Code: "operation_failed", Message: err.Error()}
		}
		delete(o.cancels, id)
		cancel()
	}()
	return operation
}

// Get returns an operation.
func (o *Operations) Get(id string) (Operation, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	operation, ok := o.ops[id]
	if !ok {
		return Operation{}, false
	}
	return *operation, true
}

// Cancel requests cancellation. It reports whether the operation was running.
func (o *Operations) Cancel(id string) bool {
	o.mu.Lock()
	cancel, ok := o.cancels[id]
	o.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

// CancelAll cancels every running operation (daemon shutdown).
func (o *Operations) CancelAll() {
	o.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(o.cancels))
	for _, cancel := range o.cancels {
		cancels = append(cancels, cancel)
	}
	o.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}
