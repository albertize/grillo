//go:build linux

// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func sessionRequest(b *Bridge, body, origin string) int {
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9090/session", strings.NewReader(body))
	r.Header.Set("Origin", origin)
	w := httptest.NewRecorder()
	b.Handler().ServeHTTP(w, r)
	return w.Code
}

func TestBootstrapSingleUseConcurrent(t *testing.T) {
	b := New(fakeCore{})
	body := `{"token":"` + b.BootstrapToken() + `"}`
	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			code := sessionRequest(b, body, "http://127.0.0.1:9090")
			if code == http.StatusNoContent {
				successes.Add(1)
			} else if code != http.StatusUnauthorized {
				t.Errorf("status = %d", code)
			}
		})
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful exchanges = %d", successes.Load())
	}
}

func TestBootstrapExpiryAndInvalidBodies(t *testing.T) {
	for _, body := range []string{`{`, `{"token":"wrong"}`, `{"token":"TOKEN","extra":1}`, `{"token":"TOKEN"} {}`, `{"token":"TOKEN"}` + strings.Repeat(" ", 4096)} {
		b := New(fakeCore{})
		body = strings.ReplaceAll(body, "TOKEN", b.BootstrapToken())
		if code := sessionRequest(b, body, ""); code == http.StatusNoContent {
			t.Fatalf("invalid body accepted")
		}
		// Invalid input must not consume the real token.
		if code := sessionRequest(b, `{"token":"`+b.BootstrapToken()+`"}`, ""); code != http.StatusNoContent {
			t.Fatalf("valid exchange = %d", code)
		}
	}
	b := New(fakeCore{})
	b.now = func() time.Time { return b.expires }
	if code := sessionRequest(b, `{"token":"`+b.BootstrapToken()+`"}`, ""); code != http.StatusUnauthorized {
		t.Fatalf("expired exchange = %d", code)
	}
}

func TestSessionOriginMustMatch(t *testing.T) {
	for _, origin := range []string{"http://evil.example", "http://localhost:9090", "http://127.0.0.1:9091", "https://127.0.0.1:9090", "null", "http://127.0.0.1:9090/path", "http://user@127.0.0.1:9090"} {
		t.Run(origin, func(t *testing.T) {
			b := New(fakeCore{})
			body := `{"token":"` + b.BootstrapToken() + `"}`
			if code := sessionRequest(b, body, origin); code != http.StatusForbidden {
				t.Fatalf("status = %d", code)
			}
			if code := sessionRequest(b, body, "http://127.0.0.1:9090"); code != http.StatusNoContent {
				t.Fatalf("same origin status = %d", code)
			}
		})
	}
}

func TestPublicListenRejected(t *testing.T) {
	for _, addr := range []string{":0", "0.0.0.0:0", "[::]:0", "192.0.2.1:0", "localhost:0", "invalid"} {
		if err := New(fakeCore{}).ListenAndServe(context.Background(), addr); err == nil {
			t.Fatalf("accepted address %q", addr)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := New(fakeCore{}).ListenAndServe(ctx, "127.0.0.1:0"); err != nil {
		t.Fatalf("loopback: %v", err)
	}
}
