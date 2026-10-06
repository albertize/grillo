//go:build linux

// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"net/http"
	"time"
)

func (s *Server) handleView(w http.ResponseWriter, r *http.Request) {
	viewer, ok := s.opts.Core.(ApplicationViewer)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "resource inspection unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	view, err := viewer.View(ctx, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "view_unavailable", "resource inspection unavailable")
		return
	}
	writeJSON(w, http.StatusOK, view)
}
