package web

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/sharing"
	"github.com/dsandor/memory/internal/storage"
)

// handleCreateShare mints a single-use cross-team share token for an entry the
// caller can access. POST /api/knowledge/{id}/share.
func (s *Server) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// Reuse the standard team-access check: 404 missing, 403 cross-team.
	entry, ok := s.fetchEntryForTeam(w, r, id)
	if !ok {
		return
	}
	tc := auth.GetTeamContext(r.Context())
	createdBy := tc.UserID
	if createdBy == "" {
		createdBy = tc.KeyID
	}
	share, err := sharing.CreateShare(r.Context(), s.store, id, entry.TeamID, createdBy)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("create share: %v", err))
		return
	}
	writeJSON(w, map[string]any{
		"share_id": share.ID,
		"url":      "/share/" + share.ID,
	})
}

// sharePreview is the safe, recipient-facing view of a shared entry. It carries
// only display fields plus the share metadata — no embeddings or internal ids
// beyond the entry's own.
type sharePreview struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Content      string   `json:"content"`
	Type         string   `json:"type"`
	Domain       string   `json:"domain"`
	Author       string   `json:"author"`
	Tags         []string `json:"tags"`
	SourceTeamID string   `json:"source_team_id"`
	Importable   bool     `json:"importable"`
	AlreadyYours bool     `json:"already_yours"`
}

// handleGetShare returns the recipient-facing preview for a share token.
// GET /api/share/{token}. The token itself is the grant: we deliberately do NOT
// run the source team's access check on the entry — any authenticated user who
// holds the token may view it.
func (s *Server) handleGetShare(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	share, err := s.store.GetShare(r.Context(), token)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "not_found", "share not found")
			return
		}
		writeError(w, 500, "internal_error", fmt.Sprintf("get share: %v", err))
		return
	}
	// Load the entry WITHOUT a team-access check — the token grants visibility.
	entry, err := s.store.GetEntry(r.Context(), share.EntryID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "not_found", "shared entry no longer exists")
			return
		}
		writeError(w, 500, "internal_error", fmt.Sprintf("get entry: %v", err))
		return
	}
	tc := auth.GetTeamContext(r.Context())
	tags := entry.Tags
	if tags == nil {
		tags = []string{}
	}
	writeJSON(w, sharePreview{
		ID:           entry.ID,
		Title:        entry.Title,
		Content:      entry.Content,
		Type:         string(entry.Type),
		Domain:       entry.Domain,
		Author:       entry.Author,
		Tags:         tags,
		SourceTeamID: share.SourceTeamID,
		Importable:   share.UsedAt == nil && share.RevokedAt == nil,
		AlreadyYours: share.SourceTeamID == tc.TeamID,
	})
}

// handleImportShare copies the shared entry into the caller's team as a new
// pending entry and burns the token. POST /api/share/{token}/import.
func (s *Server) handleImportShare(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	tc := auth.GetTeamContext(r.Context())
	destTeamID := tc.TeamID
	if destTeamID == "" {
		writeError(w, 400, "bad_request", "import requires a team context")
		return
	}
	destUserID := tc.UserID
	if destUserID == "" {
		destUserID = tc.KeyID
	}
	newID, err := sharing.Import(r.Context(), s.store, s.aiSrc, token, destTeamID, destUserID)
	if err != nil {
		// Same-team is a friendly no-op, not an error.
		if errors.Is(err, sharing.ErrSameTeam) {
			writeJSON(w, map[string]any{"status": "already_yours"})
			return
		}
		// Used / revoked / not-found / embedding-not-configured → conflict.
		writeError(w, 409, "conflict", err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"imported_entry_id": newID,
		"status":            "pending",
	})
}
