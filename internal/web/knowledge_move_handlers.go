package web

import (
	"encoding/json"
	"net/http"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/sharing"
)

// handleMoveKnowledge reassigns the given entries to a target team.
// Superadmin-only (gated at the router). Body: {entry_ids:[], team_id}.
func (s *Server) handleMoveKnowledge(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EntryIDs []string `json:"entry_ids"`
		TeamID   string   `json:"team_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TeamID == "" || len(body.EntryIDs) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "entry_ids and team_id required")
		return
	}
	if t, _ := s.store.GetTeam(r.Context(), body.TeamID); t == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "unknown team_id")
		return
	}
	if err := s.store.ReassignEntriesTeam(r.Context(), body.EntryIDs, body.TeamID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "move failed")
		return
	}
	writeJSON(w, map[string]any{"ok": true, "moved": len(body.EntryIDs)})
}

// handleCopyKnowledge duplicates the given entries into each target team as new
// pending entries (re-embedded for the destination team). The originals are left
// untouched. Superadmin-only (gated at the router). Body: {entry_ids:[], team_ids:[]}.
func (s *Server) handleCopyKnowledge(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EntryIDs []string `json:"entry_ids"`
		TeamIDs  []string `json:"team_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.EntryIDs) == 0 || len(body.TeamIDs) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "entry_ids and team_ids required")
		return
	}
	if s.aiSrc == nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "embedding not configured — cannot copy entries")
		return
	}
	tc := auth.GetTeamContext(r.Context())
	count := 0
	for _, tid := range body.TeamIDs {
		if t, _ := s.store.GetTeam(r.Context(), tid); t == nil {
			writeError(w, http.StatusBadRequest, "bad_request", "unknown team_id: "+tid)
			return
		}
		for _, eid := range body.EntryIDs {
			if _, err := sharing.CopyEntryToTeam(r.Context(), s.store, s.aiSrc, eid, tid, tc.EffectiveActorID()); err != nil {
				writeError(w, http.StatusInternalServerError, "internal_error", "copy failed")
				return
			}
			count++
		}
	}
	writeJSON(w, map[string]any{"ok": true, "copied": count})
}
