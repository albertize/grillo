//go:build linux

// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/albertize/grillo/internal/observe"
)

type viewCore struct {
	fakeCore
	fail bool
}

func (c *viewCore) View(ctx context.Context, app string) (observe.ApplicationView, error) {
	if _, ok := ctx.Deadline(); !ok {
		return observe.ApplicationView{}, errors.New("missing deadline")
	}
	if c.fail {
		return observe.ApplicationView{}, errors.New("synthetic-private-detail")
	}
	return observe.ApplicationView{Application: app, Secrets: []observe.MetadataView{{Name: "token"}}}, nil
}
func TestViewAPIClientAndUnavailable(t *testing.T) {
	c := &viewCore{}
	socket, close := startServer(t, Options{Core: c})
	defer close()
	view, err := NewClient(socket).View(context.Background(), "backend")
	if err != nil || view.Application != "backend" || len(view.Secrets) != 1 {
		t.Fatalf("view %+v %v", view, err)
	}
	for _, core := range []Core{&fakeCore{}, &viewCore{fail: true}} {
		s := NewServer(Options{Core: core})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/v1/applications/backend/view", nil))
		if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "synthetic-private-detail") {
			t.Fatal("invalid unavailable response", w.Code, w.Body.String())
		}
	}
}
