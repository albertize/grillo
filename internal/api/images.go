// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"errors"
	"net/http"

	"grillo.local/grillo/internal/build"
	"grillo.local/grillo/internal/image"
	"grillo.local/grillo/internal/state"
)

// ImageManager is the optional image inventory and build surface. When nil the
// image and build routes report that the feature is unavailable.
type ImageManager interface {
	Images(ctx context.Context) ([]image.Record, error)
	InspectImage(ctx context.Context, reference string) (image.Record, error)
	PruneImages(ctx context.Context, keep []string) (image.PruneResult, error)
	PinImage(ctx context.Context, reference string, pinned bool) error
	Build(ctx context.Context, request build.Request, progress func(string)) (build.Result, error)
}

func (s *Server) handleImages(w http.ResponseWriter, r *http.Request) {
	if s.opts.Images == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "image inventory is not configured")
		return
	}
	records, err := s.opts.Images.Images(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"images": records})
}

func (s *Server) handleInspectImage(w http.ResponseWriter, r *http.Request) {
	if s.opts.Images == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "image inventory is not configured")
		return
	}
	reference := r.URL.Query().Get("ref")
	if reference == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "ref is required")
		return
	}
	record, err := s.opts.Images.InspectImage(r.Context(), reference)
	if errors.Is(err, image.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "inspect_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Server) handlePruneImages(w http.ResponseWriter, r *http.Request) {
	if s.opts.Images == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "image inventory is not configured")
		return
	}
	var request struct {
		Keep []string `json:"keep"`
	}
	if err := decodeJSON(w, r, s.maxBody(), &request); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	operation := s.ops.Start("prune-images", "", func(ctx context.Context) error {
		_, err := s.opts.Images.PruneImages(ctx, request.Keep)
		return err
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"operationId": operation.ID})
}

func (s *Server) handlePinImage(w http.ResponseWriter, r *http.Request) {
	if s.opts.Images == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "image inventory is not configured")
		return
	}
	var request struct {
		Reference string `json:"reference"`
		Pinned    bool   `json:"pinned"`
	}
	if err := decodeJSON(w, r, s.maxBody(), &request); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if request.Reference == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "reference is required")
		return
	}
	if err := s.opts.Images.PinImage(r.Context(), request.Reference, request.Pinned); err != nil {
		if errors.Is(err, image.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "pin_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reference": request.Reference, "pinned": request.Pinned})
}

func (s *Server) handleBuild(w http.ResponseWriter, r *http.Request) {
	if s.opts.Images == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "no builder configured")
		return
	}
	var request build.Request
	if err := decodeJSON(w, r, s.maxBody(), &request); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := request.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	operation := s.ops.Start("build", request.Reference, func(ctx context.Context) error {
		_, err := s.opts.Images.Build(ctx, request, s.buildProgress("build", request.Reference))
		return err
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"operationId": operation.ID})
}

func (s *Server) buildProgress(kind, resource string) func(string) {
	return func(line string) {
		if s.opts.Events == nil {
			return
		}
		_, _ = s.opts.Events.Emit(state.Event{
			Kind:     kind,
			Resource: resource,
			Source:   "builder",
			Reason:   "progress",
			Message:  line,
		})
	}
}
