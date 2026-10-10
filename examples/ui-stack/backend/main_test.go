// SPDX-License-Identifier: Apache-2.0
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAPIStatusNotesAndValidation(t *testing.T) {
	a := &api{started: time.Now(), notes: []note{}}
	handler := a.handler()
	call := func(method, path, body, contentType string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", contentType)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/healthz", "/api/status", "/api/notes"} {
		if w := call("GET", path, "", ""); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := call("POST", "/api/notes", `{"text":"hello"}`, "application/json"); w.Code != http.StatusCreated {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call("GET", "/api/notes", "", ""); !strings.Contains(w.Body.String(), "hello") {
		t.Fatal("note missing")
	}
	for _, body := range []string{`{}`, `{"text":""}`, `{"text":"hi","unknown":true}`, `{"text":"hi"} {}`, `{"text":"` + strings.Repeat("x", 257) + `"}`, strings.Repeat("x", 4097)} {
		if w := call("POST", "/api/notes", body, "application/json"); w.Code != http.StatusBadRequest {
			t.Fatal("invalid body accepted", w.Code)
		}
	}
	if w := call("POST", "/api/notes", `{"text":"hi"}`, "text/plain"); w.Code != http.StatusUnsupportedMediaType {
		t.Fatal("simple cross-origin form content accepted", w.Code)
	}
	a.notes = make([]note, 128)
	if w := call("POST", "/api/notes", `{"text":"hi"}`, "application/json"); w.Code != http.StatusConflict {
		t.Fatal("aggregate note limit missing", w.Code)
	}
}
