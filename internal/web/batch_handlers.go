package web

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/dsandor/memory/internal/auth"
)

type batchIDsRequest struct {
	IDs []string `json:"ids"`
}

func (s *Server) handleBatchApprove(w http.ResponseWriter, r *http.Request) {
	var body batchIDsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	if len(body.IDs) == 0 {
		writeError(w, 400, "bad_request", "ids must be a non-empty array")
		return
	}

	ctx := r.Context()
	tc := auth.GetTeamContext(ctx)
	approved := 0
	var errs []string
	for _, id := range body.IDs {
		// Resolve the entry first and verify it belongs to the caller's team
		// before mutating — prevents IDOR cross-tenant approval.
		e, err := s.store.GetEntry(ctx, id)
		if err != nil || e == nil || e.TeamID != tc.TeamID {
			errs = append(errs, fmt.Sprintf("%s: not found", id))
			continue
		}
		if err := s.store.ApproveEntry(ctx, id); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", id, err))
		} else {
			approved++
		}
	}

	if errs == nil {
		errs = []string{}
	}
	writeJSON(w, map[string]any{"approved": approved, "errors": errs})
}

func (s *Server) handleBatchReject(w http.ResponseWriter, r *http.Request) {
	var body batchIDsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	if len(body.IDs) == 0 {
		writeError(w, 400, "bad_request", "ids must be a non-empty array")
		return
	}

	ctx := r.Context()
	tc := auth.GetTeamContext(ctx)
	rejected := 0
	var errs []string
	for _, id := range body.IDs {
		// Resolve the entry first and verify it belongs to the caller's team
		// before mutating — prevents IDOR cross-tenant rejection.
		e, err := s.store.GetEntry(ctx, id)
		if err != nil || e == nil || e.TeamID != tc.TeamID {
			errs = append(errs, fmt.Sprintf("%s: not found", id))
			continue
		}
		if err := s.store.RejectEntry(ctx, id); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", id, err))
		} else {
			rejected++
		}
	}

	if errs == nil {
		errs = []string{}
	}
	writeJSON(w, map[string]any{"rejected": rejected, "errors": errs})
}
