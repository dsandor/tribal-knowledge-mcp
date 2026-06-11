package web

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
)

// handleKnowledgeExport handles GET /api/knowledge/export.
// Query params:
//   - format: "json" (default) or "csv"
//   - domain: optional domain filter
//   - tag: optional tag filter (entry must have this tag)
//   - type: optional entry type filter
func (s *Server) handleKnowledgeExport(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	q := r.URL.Query()

	format := q.Get("format")
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "csv" {
		writeError(w, 400, "bad_request", "format must be json or csv")
		return
	}

	filter := storage.ListFilter{
		TeamID: tc.TeamID,
		Domain: q.Get("domain"),
		Type:   storage.KnowledgeType(q.Get("type")),
		Tag:    q.Get("tag"),
		Limit:  10000,
	}

	entries, err := s.store.ListEntries(r.Context(), filter)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list entries: %v", err))
		return
	}

	if entries == nil {
		entries = []storage.KnowledgeEntry{}
	}

	switch format {
	case "json":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="knowledge-export.json"`)
		_ = json.NewEncoder(w).Encode(entries)

	case "csv":
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", `attachment; filename="knowledge-export.csv"`)
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"id", "title", "type", "domain", "tags", "auto_tags", "content", "status", "quality_score", "created_at"})
		for _, e := range entries {
			tagsStr := strings.Join(e.Tags, "|")
			autoTagsStr := strings.Join(e.AutoTags, "|")
			// Replace embedded newlines with literal \n so each entry stays on one CSV row
			content := strings.ReplaceAll(e.Content, "\n", `\n`)
			content = strings.ReplaceAll(content, "\r", ``)
			_ = cw.Write([]string{
				e.ID,
				csvSafeCell(e.Title),
				string(e.Type),
				csvSafeCell(e.Domain),
				csvSafeCell(tagsStr),
				csvSafeCell(autoTagsStr),
				csvSafeCell(content),
				csvSafeCell(e.Status),
				fmt.Sprintf("%.4f", e.Rating),
				e.CreatedAt.Format("2006-01-02T15:04:05Z"),
			})
		}
		cw.Flush()
	}
}
