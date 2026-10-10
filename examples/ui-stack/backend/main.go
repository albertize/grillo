// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

type note struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
}
type api struct {
	mu      sync.Mutex
	notes   []note
	started time.Time
}

func (a *api) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { reply(w, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		host, _ := os.Hostname()
		reply(w, map[string]any{"service": "go-backend", "hostname": host, "goVersion": runtime.Version(), "uptimeSeconds": int(time.Since(a.started).Seconds()), "time": time.Now().UTC()})
	})
	mux.HandleFunc("GET /api/notes", func(w http.ResponseWriter, r *http.Request) { a.mu.Lock(); defer a.mu.Unlock(); reply(w, a.notes) })
	mux.HandleFunc("POST /api/notes", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "application/json required", http.StatusUnsupportedMediaType)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		defer r.Body.Close()
		var input struct {
			Text string `json:"text"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || strings.TrimSpace(input.Text) == "" || len(input.Text) > 256 {
			http.Error(w, "Provide text between 1 and 256 bytes", http.StatusBadRequest)
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			http.Error(w, "one JSON object required", http.StatusBadRequest)
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if len(a.notes) >= 128 {
			http.Error(w, "Demo note limit reached", http.StatusConflict)
			return
		}
		value := note{ID: len(a.notes) + 1, Text: strings.TrimSpace(input.Text)}
		a.notes = append(a.notes, value)
		log.Printf("note created id=%d", value.ID)
		w.WriteHeader(http.StatusCreated)
		reply(w, value)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}
func reply(w http.ResponseWriter, value any) {
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Print("response write failed")
	}
}
func main() {
	a := &api{started: time.Now(), notes: []note{{ID: 1, Text: "UI → Nginx → Go backend, each service in its own microVM."}}}
	server := &http.Server{Addr: ":8080", Handler: a.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	fmt.Println("Go backend listening on :8080; demo notes are volatile and bounded to 128")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
