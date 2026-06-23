package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
)

// validVisibilityRuleTypes is the set of accepted rule types for a per-user
// visibility (suppression) rule.
var validVisibilityRuleTypes = map[string]bool{
	"item":   true,
	"author": true,
	"tag":    true,
	"domain": true,
}

// visibilityRuleResponse is the JSON shape returned to the SPA for a single
// per-user visibility rule.
type visibilityRuleResponse struct {
	RuleType    string `json:"rule_type"`
	Value       string `json:"value"`
	CreatedAt   string `json:"created_at"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

func toVisibilityRuleResponse(r storage.VisibilityRule) visibilityRuleResponse {
	return visibilityRuleResponse{
		RuleType:  r.RuleType,
		Value:     r.Value,
		CreatedAt: r.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// actorID resolves a stable per-caller identity for the caller's personal
// visibility rules. It prefers the user id, falling back to the API key id and
// then the constant "local" (dev-bypass / no-auth), so the feature works for
// team-scoped keys and single-operator setups. Never empty.
func actorID(r *http.Request) string {
	return auth.GetTeamContext(r.Context()).EffectiveActorID()
}

// handleListVisibility returns the caller's per-user visibility rules.
func (s *Server) handleListVisibility(w http.ResponseWriter, r *http.Request) {
	userID := actorID(r)
	rules, err := s.store.ListVisibilityRules(r.Context(), userID)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list visibility rules: %v", err))
		return
	}
	out := make([]visibilityRuleResponse, 0, len(rules))
	for _, rule := range rules {
		resp := toVisibilityRuleResponse(rule)
		// For hidden individual items, enrich the row with the entry's title
		// and description so the caller sees what they hid (not a raw UUID).
		// The caller is viewing their own hidden list, so team-access
		// filtering is intentionally not applied here.
		if rule.RuleType == "item" {
			entry, err := s.store.GetEntry(r.Context(), rule.Value)
			switch {
			case errors.Is(err, storage.ErrNotFound) || (err == nil && entry == nil):
				resp.Title = "(entry not found)"
			case err != nil:
				// Don't fail the whole request for one bad lookup; leave the
				// title/description empty and move on.
				slog.Warn("visibility: lookup hidden entry", "id", rule.Value, "err", err)
			default:
				resp.Title = entry.Title
				resp.Description = entry.Description
			}
		}
		out = append(out, resp)
	}
	writeJSON(w, out)
}

// handleAddVisibility creates a per-user visibility rule for the caller.
func (s *Server) handleAddVisibility(w http.ResponseWriter, r *http.Request) {
	userID := actorID(r)
	var body struct {
		RuleType string `json:"rule_type"`
		Value    string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	if !validVisibilityRuleTypes[body.RuleType] {
		writeError(w, 400, "bad_request", "rule_type must be one of: item, author, tag, domain")
		return
	}
	if body.Value == "" {
		writeError(w, 400, "bad_request", "value is required")
		return
	}
	if len(body.Value) > 500 {
		writeError(w, 400, "bad_request", "value too long")
		return
	}
	rule, err := s.store.AddVisibilityRule(r.Context(), userID, body.RuleType, body.Value)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("add visibility rule: %v", err))
		return
	}
	writeJSON(w, toVisibilityRuleResponse(rule))
}

// handleDeleteVisibility removes a per-user visibility rule for the caller.
// The rule is identified by rule_type+value taken from the JSON body, falling
// back to query parameters when no body is supplied.
func (s *Server) handleDeleteVisibility(w http.ResponseWriter, r *http.Request) {
	userID := actorID(r)
	var body struct {
		RuleType string `json:"rule_type"`
		Value    string `json:"value"`
	}
	// Body is optional; decode errors fall through to query params.
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.RuleType == "" {
		body.RuleType = r.URL.Query().Get("rule_type")
	}
	if body.Value == "" {
		body.Value = r.URL.Query().Get("value")
	}
	if !validVisibilityRuleTypes[body.RuleType] {
		writeError(w, 400, "bad_request", "rule_type must be one of: item, author, tag, domain")
		return
	}
	if body.Value == "" {
		writeError(w, 400, "bad_request", "value is required")
		return
	}
	if err := s.store.DeleteVisibilityRule(r.Context(), userID, body.RuleType, body.Value); err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("delete visibility rule: %v", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
