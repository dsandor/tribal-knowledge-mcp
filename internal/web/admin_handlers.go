package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
)

func (s *Server) handleListTeams(w http.ResponseWriter, r *http.Request) {
	teams, err := s.store.ListTeams(r.Context())
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list teams: %v", err))
		return
	}
	if teams == nil {
		teams = []storage.Team{}
	}
	writeJSON(w, teams)
}

func (s *Server) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name           string   `json:"name"`
		DomainPatterns []string `json:"domain_patterns"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	if body.Name == "" {
		writeError(w, 400, "bad_request", "name required")
		return
	}
	id, err := s.store.CreateTeam(r.Context(), storage.Team{
		Name:           body.Name,
		DomainPatterns: body.DomainPatterns,
		Enabled:        true,
	})
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("create team: %v", err))
		return
	}
	writeJSON(w, map[string]string{"id": id})
}

func (s *Server) handleUpdateTeam(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Name           string   `json:"name"`
		DomainPatterns []string `json:"domain_patterns"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	if body.Name == "" {
		writeError(w, 400, "bad_request", "name required")
		return
	}
	if body.DomainPatterns == nil {
		body.DomainPatterns = []string{}
	}
	if err := s.store.UpdateTeam(r.Context(), id, body.Name, body.DomainPatterns); err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("update team: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleSetTeamEnabled(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	if err := s.store.SetTeamEnabled(r.Context(), id, body.Enabled); err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("set team enabled: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleDeleteTeam(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteTeam(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("delete team: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleListTeamUsers(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	teamID := chi.URLParam(r, "id")
	// Non-superadmin callers may only inspect their own team
	if tc.Role != "superadmin" && teamID != tc.TeamID {
		writeError(w, 403, "forbidden", "forbidden")
		return
	}
	users, err := s.store.ListUsers(r.Context(), teamID)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list users: %v", err))
		return
	}
	type safeUser struct {
		ID               string `json:"id"`
		TeamID           string `json:"team_id"`
		Email            string `json:"email"`
		Name             string `json:"name"`
		Role             string `json:"role"`
		ManuallyAssigned bool   `json:"manually_assigned"`
	}
	safe := make([]safeUser, len(users))
	for i, u := range users {
		safe[i] = safeUser{ID: u.ID, TeamID: u.TeamID, Email: u.Email, Name: u.Name, Role: u.Role, ManuallyAssigned: u.ManuallyAssigned}
	}
	writeJSON(w, safe)
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	users, err := s.store.ListUsers(r.Context(), tc.TeamID)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list users: %v", err))
		return
	}
	type safeUser struct {
		ID               string `json:"id"`
		TeamID           string `json:"team_id"`
		Email            string `json:"email"`
		Name             string `json:"name"`
		Role             string `json:"role"`
		ManuallyAssigned bool   `json:"manually_assigned"`
	}
	safe := make([]safeUser, len(users))
	for i, u := range users {
		safe[i] = safeUser{ID: u.ID, TeamID: u.TeamID, Email: u.Email, Name: u.Name, Role: u.Role, ManuallyAssigned: u.ManuallyAssigned}
	}
	writeJSON(w, safe)
}

func (s *Server) handleAssignUser(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	var body struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	if body.Email == "" {
		writeError(w, 400, "bad_request", "email required")
		return
	}
	if body.Role == "" {
		body.Role = "member"
	}
	if !validAdminRole[body.Role] {
		writeError(w, 400, "bad_request", "invalid role: must be member, curator, or admin")
		return
	}
	if adminRoleRank(body.Role) >= adminRoleRank(tc.Role) {
		writeError(w, 403, "forbidden", "cannot grant a role equal to or higher than your own")
		return
	}
	user, _ := s.store.GetUserByEmail(r.Context(), body.Email)
	if user == nil {
		uid, err := s.store.UpsertUser(r.Context(), storage.User{Email: body.Email, Role: body.Role})
		if err != nil {
			writeError(w, 500, "internal_error", fmt.Sprintf("create user: %v", err))
			return
		}
		_ = s.store.AssignUserToTeam(r.Context(), uid, tc.TeamID, body.Role)
		writeJSON(w, map[string]string{"id": uid})
		return
	}
	if err := s.store.AssignUserToTeam(r.Context(), user.ID, tc.TeamID, body.Role); err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("assign user: %v", err))
		return
	}
	writeJSON(w, map[string]string{"id": user.ID})
}

func (s *Server) handleSetUserRole(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	targetID := chi.URLParam(r, "id")
	var body struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	if !validAdminRole[body.Role] {
		writeError(w, 400, "bad_request", "invalid role: must be member, curator, or admin")
		return
	}
	if adminRoleRank(body.Role) >= adminRoleRank(tc.Role) {
		writeError(w, 403, "forbidden", "cannot grant a role equal to or higher than your own")
		return
	}
	// Verify target user belongs to the caller's team (IDOR guard)
	teamUsers, err := s.store.ListUsers(r.Context(), tc.TeamID)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list users: %v", err))
		return
	}
	found := false
	for _, u := range teamUsers {
		if u.ID == targetID {
			found = true
			break
		}
	}
	if !found {
		writeError(w, 404, "not_found", "user not found")
		return
	}
	if err := s.store.AssignUserToTeam(r.Context(), targetID, tc.TeamID, body.Role); err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("set role: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	keys, err := s.store.ListAPIKeys(r.Context(), tc.TeamID)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list api keys: %v", err))
		return
	}
	if keys == nil {
		keys = []storage.APIKey{}
	}
	type safeKey struct {
		ID        string `json:"id"`
		TeamID    string `json:"team_id"`
		UserID    string `json:"user_id"`
		KeyType   string `json:"key_type"`
		Name      string `json:"name"`
		Role      string `json:"role"`
		CreatedAt string `json:"created_at"`
	}
	safe := make([]safeKey, len(keys))
	for i, k := range keys {
		safe[i] = safeKey{ID: k.ID, TeamID: k.TeamID, UserID: k.UserID, KeyType: k.KeyType, Name: k.Name, Role: k.Role, CreatedAt: k.CreatedAt.Format("2006-01-02T15:04:05Z")}
	}
	writeJSON(w, safe)
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	var body struct {
		Name    string `json:"name"`
		Role    string `json:"role"`
		KeyType string `json:"key_type"`
		UserID  string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	if body.Name == "" {
		writeError(w, 400, "bad_request", "name required")
		return
	}
	if body.KeyType == "" {
		body.KeyType = storage.APIKeyTypeTeam
	}
	if body.Role == "" {
		body.Role = "member"
	}
	if !validAdminRole[body.Role] {
		writeError(w, 400, "bad_request", "invalid role: must be member, curator, or admin")
		return
	}
	if adminRoleRank(body.Role) >= adminRoleRank(tc.Role) {
		writeError(w, 403, "forbidden", "cannot grant a role equal to or higher than your own")
		return
	}
	// If a UserID is provided, verify it belongs to the caller's team
	if body.UserID != "" {
		teamUsers, err := s.store.ListUsers(r.Context(), tc.TeamID)
		if err != nil {
			writeError(w, 500, "internal_error", fmt.Sprintf("list users: %v", err))
			return
		}
		found := false
		for _, u := range teamUsers {
			if u.ID == body.UserID {
				found = true
				break
			}
		}
		if !found {
			writeError(w, 404, "not_found", "user not found in team")
			return
		}
	}
	rawKey := generateRawKey()
	hash := auth.HashSHA256(rawKey)
	keyID := uuid.NewString()
	now := time.Now().UTC()
	key := storage.APIKey{
		ID:      keyID,
		TeamID:  tc.TeamID,
		UserID:  body.UserID,
		KeyType: body.KeyType,
		Name:    body.Name,
		KeyHash: hash,
		Role:    body.Role,
	}
	if err := s.store.CreateAPIKey(r.Context(), key); err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("create api key: %v", err))
		return
	}
	writeJSON(w, map[string]any{
		"id":         keyID,
		"raw_key":    rawKey,
		"name":       body.Name,
		"role":       body.Role,
		"key_type":   body.KeyType,
		"created_at": now.Format("2006-01-02T15:04:05Z"),
	})
}

func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	targetID := chi.URLParam(r, "id")
	// Verify the key belongs to the caller's team (IDOR guard)
	teamKeys, err := s.store.ListAPIKeys(r.Context(), tc.TeamID)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list api keys: %v", err))
		return
	}
	found := false
	for _, k := range teamKeys {
		if k.ID == targetID {
			found = true
			break
		}
	}
	if !found {
		writeError(w, 404, "not_found", "api key not found")
		return
	}
	if err := s.store.RevokeAPIKey(r.Context(), targetID); err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("revoke key: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// adminRoleRank returns a numeric rank for role comparison.
// Callers may not grant a role >= their own rank.
func adminRoleRank(role string) int {
	switch role {
	case "superadmin":
		return 4
	case "admin":
		return 3
	case "curator":
		return 2
	case "member":
		return 1
	}
	return 0
}

// validAdminRole is the set of roles that admin-level callers may grant.
var validAdminRole = map[string]bool{"member": true, "curator": true, "admin": true}

func generateRawKey() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return "tk_" + hex.EncodeToString(b)
}
