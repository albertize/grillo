//go:build linux

// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"grillo.local/grillo/internal/api"
	"grillo.local/grillo/internal/observe"
)

type fakeCore struct{}

func (fakeCore) Applications(context.Context) ([]string, error) { return []string{"backend"}, nil }
func (fakeCore) Status(context.Context, string) ([]api.ContainerStatus, error) {
	return []api.ContainerStatus{{Container: "app", State: "running"}}, nil
}
func (fakeCore) ListLogs(context.Context, uint64, string, string) ([]observe.LogRecord, error) {
	return nil, nil
}
func (fakeCore) Events(context.Context, uint64) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("data: {\"seq\":1}\n\n")), nil
}

func postToken(t *testing.T, client *http.Client, url, token string) *http.Response {
	t.Helper()
	resp, err := client.Post(url+"/session", "application/json", strings.NewReader(`{"token":"`+token+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestBridgeRequiresSessionAndSetsSecurityHeaders(t *testing.T) {
	bridge := New(fakeCore{})
	server := httptest.NewServer(bridge.Handler())
	defer server.Close()
	client := server.Client()

	// Index is public but carries a strict CSP.
	resp, err := client.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Grillo") {
		t.Fatalf("index status=%d", resp.StatusCode)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Fatalf("CSP = %q", csp)
	}

	// Data endpoints require a session.
	resp, _ = client.Get(server.URL + "/v1/applications")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", resp.StatusCode)
	}

	// A wrong bootstrap token is rejected.
	resp = postToken(t, client, server.URL, "wrong")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d", resp.StatusCode)
	}

	// The correct token sets an HttpOnly session cookie.
	resp = postToken(t, client, server.URL, bridge.BootstrapToken())
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("session status = %d", resp.StatusCode)
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly {
		t.Fatalf("session cookie = %+v", cookies)
	}

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/applications", nil)
	req.AddCookie(cookies[0])
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "backend") {
		t.Fatalf("authorized status=%d body=%q", resp.StatusCode, body)
	}

	// A foreign Host header is rejected (DNS-rebinding defense).
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/", nil)
	req.Host = "evil.example"
	resp, _ = client.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign host status = %d", resp.StatusCode)
	}

	// Events proxy with a valid session.
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/v1/events", nil)
	req.AddCookie(cookies[0])
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	events, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(events), "seq") {
		t.Fatalf("events body = %q", events)
	}
}
