package web

import (
	"fmt"
	"net/http"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
)

// handleMyAPIKeys returns the API keys visible to the calling member: their
// own personal (user-scoped) key(s), plus team-scoped keys if their role is
// admin or above. Unlike handleListAPIKeys (admin-only, returns every key in
// the team), this is safe to expose to any authenticated team member — it's
// what the avatar-menu / MCP-setup UI calls for non-admins.
func (s *Server) handleMyAPIKeys(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	keys, err := s.store.ListAPIKeys(r.Context(), tc.TeamID)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list api keys: %v", err))
		return
	}
	// Same admin-or-above comparison RequireAdmin uses (auth.roleRank isn't
	// exported, but adminRoleRank is this package's equivalent rank table —
	// reused rather than re-deriving a second role-ranking scheme).
	isAdminOrAbove := adminRoleRank(tc.Role) >= adminRoleRank("admin")
	visible := make([]storage.APIKey, 0, len(keys))
	for _, k := range keys {
		switch {
		case k.KeyType == storage.APIKeyTypeUser && k.UserID != "" && k.UserID == tc.UserID:
			visible = append(visible, k)
		case k.KeyType == storage.APIKeyTypeTeam && isAdminOrAbove:
			visible = append(visible, k)
		}
	}
	writeJSON(w, toSafeAPIKeys(visible))
}
