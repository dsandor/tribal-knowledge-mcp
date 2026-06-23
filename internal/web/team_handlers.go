package web

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleMyTeams(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	type teamDTO struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var teams []teamDTO
	switch {
	case tc.Role == "superadmin":
		all, _ := s.store.ListTeams(r.Context())
		for _, t := range all {
			teams = append(teams, teamDTO{t.ID, t.Name})
		}
	case tc.UserID != "":
		ut, _ := s.store.ListUserTeams(r.Context(), tc.UserID)
		for _, t := range ut {
			teams = append(teams, teamDTO{t.ID, t.Name})
		}
	default: // plain team key
		if t, _ := s.store.GetTeam(r.Context(), tc.TeamID); t != nil {
			teams = append(teams, teamDTO{t.ID, t.Name})
		}
	}
	writeJSON(w, map[string]any{"teams": teams, "active_team": tc.TeamID})
}

// handleListUserTeams returns the teams a specific user belongs to (home team
// plus memberships). Used by the Users page membership editor to show current
// state. Gated by RequireAdmin at the route level.
func (s *Server) handleListUserTeams(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	tc := auth.GetTeamContext(r.Context())
	// IDOR guard: a non-superadmin caller may only read memberships for a user
	// whose home team they can manage.
	targetUser, err := s.store.GetUserByID(r.Context(), userID)
	if err != nil || targetUser == nil {
		writeError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	if tc.Role != "superadmin" && !s.authorizeTeamManage(r, targetUser.TeamID) {
		writeError(w, http.StatusForbidden, "forbidden", "forbidden")
		return
	}
	type teamDTO struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	teams := []teamDTO{}
	ut, _ := s.store.ListUserTeams(r.Context(), userID)
	for _, t := range ut {
		teams = append(teams, teamDTO{t.ID, t.Name})
	}
	writeJSON(w, map[string]any{"teams": teams})
}

// authorizeTeamManage reports whether the caller may manage memberships for
// teamID. Superadmins may manage any team; a user identity may manage teams
// they belong to; a plain team key may manage only its own team.
func (s *Server) authorizeTeamManage(r *http.Request, teamID string) bool {
	tc := auth.GetTeamContext(r.Context())
	if tc.Role == "superadmin" {
		return true
	}
	if tc.UserID != "" {
		ok, _ := s.store.IsTeamMember(r.Context(), tc.UserID, teamID)
		return ok
	}
	return teamID == tc.TeamID
}

func (s *Server) handleAddMembership(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	var body struct {
		TeamID string `json:"team_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TeamID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "team_id required")
		return
	}
	if !s.authorizeTeamManage(r, body.TeamID) {
		writeError(w, http.StatusForbidden, "forbidden", "forbidden")
		return
	}
	if err := s.store.AddTeamMember(r.Context(), userID, body.TeamID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "add membership failed")
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleRemoveMembership(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	teamID := chi.URLParam(r, "teamId")
	if !s.authorizeTeamManage(r, teamID) {
		writeError(w, http.StatusForbidden, "forbidden", "forbidden")
		return
	}
	if err := s.store.RemoveTeamMember(r.Context(), userID, teamID); err != nil {
		if errors.Is(err, storage.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "bad_request", "cannot remove the user's home team")
		} else {
			writeError(w, http.StatusInternalServerError, "internal_error", "remove membership failed")
		}
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
