//go:build linux

// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

type routeCore struct{ fakeCore }

func (*routeCore) Routes(context.Context, string) ([]RouteStatus, error) {
	return []RouteStatus{{Hostname: "app.local", Path: "/", PathType: "Prefix", Endpoint: "http://127.0.0.1:18080"}}, nil
}

func TestStatusIncludesPublicRouteFallback(t *testing.T) {
	server := NewServer(Options{Core: &routeCore{}})
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/v1/applications/app", nil))
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var body struct {
		Routes []RouteStatus `json:"routes"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Routes) != 1 || body.Routes[0].Endpoint != "http://127.0.0.1:18080" {
		t.Fatalf("missing actual fallback: %+v", body)
	}
}
