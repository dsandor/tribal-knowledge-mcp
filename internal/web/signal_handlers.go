package web

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
)

// handleKnowledgeTrending handles GET /api/knowledge/trending.
// Query params: days (int, default 7), limit (int, default 10)
func (s *Server) handleKnowledgeTrending(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	q := r.URL.Query()

	days, err := strconv.Atoi(q.Get("days"))
	if err != nil || days <= 0 {
		days = 7
	}
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit <= 0 {
		limit = 10
	}

	entries, err := s.store.GetTrendingEntries(r.Context(), tc.TeamID, days, limit)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("get trending entries: %v", err))
		return
	}
	if entries == nil {
		entries = []storage.TrendingEntry{}
	}
	writeJSON(w, entries)
}

// handleActivityFeed handles GET /api/activity.
// Query params: limit (int, default 20), offset (int, default 0)
func (s *Server) handleActivityFeed(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	q := r.URL.Query()

	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit <= 0 {
		limit = 20
	}
	offset, err := strconv.Atoi(q.Get("offset"))
	if err != nil || offset < 0 {
		offset = 0
	}

	events, err := s.store.ListActivity(r.Context(), tc.TeamID, limit, offset)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list activity: %v", err))
		return
	}
	if events == nil {
		events = []storage.ActivityEvent{}
	}
	writeJSON(w, events)
}
