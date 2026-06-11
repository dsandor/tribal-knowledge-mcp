package web

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
	tagspkg "github.com/dsandor/memory/internal/tags"
)

// importEntry is the JSON shape for a single entry in the bulk-import request body.
type importEntry struct {
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Type    string   `json:"type"`
	Domain  string   `json:"domain"`
	Tags    []string `json:"tags"`
}

// handleKnowledgeImport handles POST /api/knowledge/import.
// Accepts either:
//   - application/json: JSON array of entry objects
//   - multipart/form-data: CSV file in the "file" field
//
// All imported entries have Status = "approved".
func (s *Server) handleKnowledgeImport(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())

	ct := r.Header.Get("Content-Type")

	var rawEntries []importEntry

	switch {
	case strings.HasPrefix(ct, "application/json"):
		if err := json.NewDecoder(r.Body).Decode(&rawEntries); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
			return
		}

	case strings.HasPrefix(ct, "multipart/form-data"):
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "parse multipart form: "+err.Error())
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "missing 'file' field in form")
			return
		}
		defer file.Close()

		parsed, parseErr := parseImportCSV(file)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "parse CSV: "+parseErr.Error())
			return
		}
		rawEntries = parsed

	default:
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
			"Content-Type must be application/json or multipart/form-data")
		return
	}

	if len(rawEntries) == 0 {
		writeJSON(w, map[string]any{"imported": 0, "skipped": 0, "errors": []string{}})
		return
	}

	entries := make([]storage.KnowledgeEntry, 0, len(rawEntries))
	for _, re := range rawEntries {
		entryType := storage.KnowledgeType(re.Type)
		if entryType == "" {
			entryType = storage.KTPattern // sensible default for imports
		}
		domain := re.Domain
		if domain == "" {
			domain = "general"
		}
		tags := tagspkg.Merge(re.Tags, tagspkg.ExtractHashtags(re.Title+" "+re.Content))
		if tags == nil {
			tags = []string{}
		}
		entries = append(entries, storage.KnowledgeEntry{
			Title:   re.Title,
			Content: re.Content,
			Type:    entryType,
			Domain:  domain,
			Tags:    tags,
			TeamID:  tc.TeamID,
			Team:    tc.TeamID,
			Status:  "approved",
		})
	}

	imported, skipped, errs, err := s.store.BulkImport(r.Context(), entries)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error",
			fmt.Sprintf("bulk import: %v", err))
		return
	}

	if errs == nil {
		errs = []string{}
	}
	writeJSON(w, map[string]any{
		"imported": imported,
		"skipped":  skipped,
		"errors":   errs,
	})
}

// parseImportCSV reads a CSV reader with header row: title,content,type,domain,tags
// where tags may be pipe-separated or a quoted comma-separated list.
func parseImportCSV(r io.Reader) ([]importEntry, error) {
	cr := csv.NewReader(r)
	cr.LazyQuotes = true
	cr.TrimLeadingSpace = true

	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("read CSV header: %w", err)
	}

	// Build column index map (case-insensitive).
	colIdx := make(map[string]int, len(header))
	for i, col := range header {
		colIdx[strings.ToLower(strings.TrimSpace(col))] = i
	}

	titleIdx, hasTitleCol := colIdx["title"]
	contentIdx, hasContentCol := colIdx["content"]
	if !hasTitleCol || !hasContentCol {
		return nil, fmt.Errorf("CSV must have 'title' and 'content' columns")
	}
	typeIdx := colIdx["type"]
	domainIdx := colIdx["domain"]
	tagsIdx, hasTagsCol := colIdx["tags"]

	var entries []importEntry
	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read CSV row: %w", err)
		}
		if len(row) <= titleIdx || len(row) <= contentIdx {
			continue // skip malformed rows
		}

		entry := importEntry{
			Title:   strings.TrimSpace(row[titleIdx]),
			Content: strings.TrimSpace(row[contentIdx]),
		}
		if entry.Title == "" || entry.Content == "" {
			continue
		}

		if _, ok := colIdx["type"]; ok && len(row) > typeIdx {
			entry.Type = strings.TrimSpace(row[typeIdx])
		}
		if _, ok := colIdx["domain"]; ok && len(row) > domainIdx {
			entry.Domain = strings.TrimSpace(row[domainIdx])
		}
		if hasTagsCol && len(row) > tagsIdx {
			raw := strings.TrimSpace(row[tagsIdx])
			if raw != "" {
				entry.Tags = splitTags(raw)
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// splitTags handles both pipe-separated ("a|b|c") and comma-separated ("a,b,c") tag lists.
func splitTags(raw string) []string {
	var sep string
	if strings.Contains(raw, "|") {
		sep = "|"
	} else {
		sep = ","
	}
	parts := strings.Split(raw, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
