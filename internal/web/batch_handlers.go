package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/live"
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
	var succeededIDs []string
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
			succeededIDs = append(succeededIDs, id)
		}
	}

	if errs == nil {
		errs = []string{}
	}

	// Publish live event(s) only for IDs that actually succeeded.
	if len(succeededIDs) > 0 {
		actorID := tc.UserID
		if actorID == "" {
			actorID = tc.KeyID
		}
		actor := live.ActorRef{ID: actorID, Display: tc.Display}
		if len(succeededIDs) > 10 {
			// Large batch: publish a single summarizing event.
			s.publishLive(live.LiveEvent{
				Type:   live.TypeApproved,
				TeamID: tc.TeamID,
				Actor:  actor,
				Meta:   map[string]string{"count": strconv.Itoa(approved)},
			})
		} else {
			for _, id := range succeededIDs {
				s.publishLive(live.LiveEvent{
					Type:    live.TypeApproved,
					TeamID:  tc.TeamID,
					EntryID: id,
					Actor:   actor,
				})
			}
		}
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
	var succeededIDs []string
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
			succeededIDs = append(succeededIDs, id)
		}
	}

	if errs == nil {
		errs = []string{}
	}

	// Publish live event(s) only for IDs that actually succeeded.
	if len(succeededIDs) > 0 {
		actorID := tc.UserID
		if actorID == "" {
			actorID = tc.KeyID
		}
		actor := live.ActorRef{ID: actorID, Display: tc.Display}
		if len(succeededIDs) > 10 {
			// Large batch: publish a single summarizing event.
			s.publishLive(live.LiveEvent{
				Type:   live.TypeRejected,
				TeamID: tc.TeamID,
				Actor:  actor,
				Meta:   map[string]string{"count": strconv.Itoa(rejected)},
			})
		} else {
			for _, id := range succeededIDs {
				s.publishLive(live.LiveEvent{
					Type:    live.TypeRejected,
					TeamID:  tc.TeamID,
					EntryID: id,
					Actor:   actor,
				})
			}
		}
	}

	writeJSON(w, map[string]any{"rejected": rejected, "errors": errs})
}
