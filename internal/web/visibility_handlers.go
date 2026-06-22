package web

import (
	"encoding/json"
	"fmt"
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
	RuleType  string `json:"rule_type"`
	Value     string `json:"value"`
	CreatedAt string `json:"created_at"`
}

func toVisibilityRuleResponse(r storage.VisibilityRule) visibilityRuleResponse {
	return visibilityRuleResponse{
		RuleType:  r.RuleType,
		Value:     r.Value,
		CreatedAt: r.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// requireUser resolves the caller's user id, writing a 400 error and returning
// ok=false when the request is not user-scoped (e.g. a team-only API key).
// Per-user visibility rules are meaningless without a user identity.
func requireUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	tc := auth.GetTeamContext(r.Context())
	if tc.UserID == "" {
		writeError(w, http.StatusBadRequest, "no_user", "requires a user session/user token")
		return "", false
	}
	return tc.UserID, true
}

// handleListVisibility returns the caller's per-user visibility rules.
func (s *Server) handleListVisibility(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	rules, err := s.store.ListVisibilityRules(r.Context(), userID)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list visibility rules: %v", err))
		return
	}
	out := make([]visibilityRuleResponse, 0, len(rules))
	for _, rule := range rules {
		out = append(out, toVisibilityRuleResponse(rule))
	}
	writeJSON(w, out)
}

// handleAddVisibility creates a per-user visibility rule for the caller.
func (s *Server) handleAddVisibility(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
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
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
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
