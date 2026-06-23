package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/web"
)

// newVisServer builds a test server backed by a visStore (which persists
// visibility rules in memory).
func newVisServer(t *testing.T, store *visStore) *web.Server {
	t.Helper()
	staticFS := fstest.MapFS{
		"index.html": {Data: []byte("<html><body>app</body></html>"), Mode: 0444, ModTime: time.Now()},
	}
	return web.NewServer(staticFS, store)
}

// visStore embeds mockStore but actually persists visibility rules in memory so
// the add/list/delete round-trip can be exercised end to end.
type visStore struct {
	mockStore
	rules []storage.VisibilityRule
}

func (v *visStore) AddVisibilityRule(_ context.Context, userID, ruleType, value string) (storage.VisibilityRule, error) {
	rule := storage.VisibilityRule{
		ID:       userID + ":" + ruleType + ":" + value,
		UserID:   userID,
		RuleType: ruleType,
		Value:    value,
	}
	for _, r := range v.rules {
		if r.RuleType == ruleType && r.Value == value && r.UserID == userID {
			return r, nil // idempotent
		}
	}
	v.rules = append(v.rules, rule)
	return rule, nil
}

func (v *visStore) DeleteVisibilityRule(_ context.Context, userID, ruleType, value string) error {
	out := v.rules[:0]
	for _, r := range v.rules {
		if r.UserID == userID && r.RuleType == ruleType && r.Value == value {
			continue
		}
		out = append(out, r)
	}
	v.rules = out
	return nil
}

func (v *visStore) ListVisibilityRules(_ context.Context, userID string) ([]storage.VisibilityRule, error) {
	var out []storage.VisibilityRule
	for _, r := range v.rules {
		if r.UserID == userID {
			out = append(out, r)
		}
	}
	return out, nil
}

func decodeVisRules(t *testing.T, w *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var rules []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&rules); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return rules
}

// TestVisibilityHandlers_AddListDelete exercises the full lifecycle of a
// per-user visibility rule via the HTTP API for a user-scoped caller.
func TestVisibilityHandlers_AddListDelete(t *testing.T) {
	store := &visStore{mockStore: mockStore{apiKeyUserID: "user-a"}}
	srv := newVisServer(t, store)

	// POST adds a rule.
	req := authRequest("POST", "/api/visibility", `{"rule_type":"author","value":"carol"}`)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST want 200, got %d: %s", w.Code, w.Body.String())
	}
	var created map[string]any
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created["rule_type"] != "author" || created["value"] != "carol" {
		t.Fatalf("unexpected created rule: %v", created)
	}

	// GET lists it.
	req = authRequest("GET", "/api/visibility", "")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET want 200, got %d: %s", w.Code, w.Body.String())
	}
	rules := decodeVisRules(t, w)
	if len(rules) != 1 || rules[0]["rule_type"] != "author" || rules[0]["value"] != "carol" {
		t.Fatalf("GET unexpected rules: %v", rules)
	}

	// DELETE removes it.
	req = authRequest("DELETE", "/api/visibility", `{"rule_type":"author","value":"carol"}`)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE want 204, got %d: %s", w.Code, w.Body.String())
	}

	// GET now empty.
	req = authRequest("GET", "/api/visibility", "")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET want 200, got %d: %s", w.Code, w.Body.String())
	}
	rules = decodeVisRules(t, w)
	if len(rules) != 0 {
		t.Fatalf("expected no rules after delete, got %v", rules)
	}
}

// TestVisibilityHandlers_ItemRuleEnrichedWithTitle ensures that hiding an
// existing entry (an "item" rule) results in the GET /api/visibility response
// carrying the entry's Title (and Description) rather than just the raw UUID.
func TestVisibilityHandlers_ItemRuleEnrichedWithTitle(t *testing.T) {
	store := &visStore{mockStore: mockStore{
		apiKeyUserID: "user-a",
		entries: []storage.KnowledgeEntry{
			{ID: "entry-123", Title: "Q3 Earnings Playbook", Description: "How to prompt for earnings summaries"},
		},
	}}
	srv := newVisServer(t, store)

	// Hide the existing entry by id.
	req := authRequest("POST", "/api/visibility", `{"rule_type":"item","value":"entry-123"}`)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST want 200, got %d: %s", w.Code, w.Body.String())
	}

	// GET returns the rule enriched with the entry's title/description.
	req = authRequest("GET", "/api/visibility", "")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET want 200, got %d: %s", w.Code, w.Body.String())
	}
	rules := decodeVisRules(t, w)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %v", rules)
	}
	if rules[0]["rule_type"] != "item" || rules[0]["value"] != "entry-123" {
		t.Fatalf("unexpected rule: %v", rules[0])
	}
	if rules[0]["title"] != "Q3 Earnings Playbook" {
		t.Fatalf("expected title to be populated, got %v", rules[0]["title"])
	}
	if rules[0]["description"] != "How to prompt for earnings summaries" {
		t.Fatalf("expected description to be populated, got %v", rules[0]["description"])
	}
}

// TestVisibilityHandlers_ItemRuleMissingEntry ensures hiding an item whose
// entry no longer exists surfaces a "(entry not found)" placeholder title
// rather than failing the request.
func TestVisibilityHandlers_ItemRuleMissingEntry(t *testing.T) {
	store := &visStore{mockStore: mockStore{apiKeyUserID: "user-a"}}
	srv := newVisServer(t, store)

	req := authRequest("POST", "/api/visibility", `{"rule_type":"item","value":"gone-999"}`)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST want 200, got %d: %s", w.Code, w.Body.String())
	}

	req = authRequest("GET", "/api/visibility", "")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET want 200, got %d: %s", w.Code, w.Body.String())
	}
	rules := decodeVisRules(t, w)
	if len(rules) != 1 || rules[0]["title"] != "(entry not found)" {
		t.Fatalf("expected (entry not found) title, got %v", rules)
	}
}

// TestVisibilityHandlers_RejectsBadRuleType ensures an invalid rule_type is 400.
func TestVisibilityHandlers_RejectsBadRuleType(t *testing.T) {
	store := &visStore{mockStore: mockStore{apiKeyUserID: "user-a"}}
	srv := newVisServer(t, store)

	req := authRequest("POST", "/api/visibility", `{"rule_type":"bogus","value":"x"}`)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestVisibilityHandlers_TeamKeyFallbackIdentity ensures a request without a
// user identity (team-scoped API key) still works: it operates under the
// effective actor identity (the API key id), so it can add a rule and list it
// back rather than being rejected with a 400.
func TestVisibilityHandlers_TeamKeyFallbackIdentity(t *testing.T) {
	store := &visStore{} // apiKeyUserID empty → falls back to key id "test-key"
	srv := newVisServer(t, store)

	// POST succeeds under the fallback identity.
	req := authRequest("POST", "/api/visibility", `{"rule_type":"author","value":"carol"}`)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST want 200, got %d: %s", w.Code, w.Body.String())
	}

	// The rule was stored under the key id (the EffectiveActorID fallback).
	if len(store.rules) != 1 || store.rules[0].UserID != "test-key" {
		t.Fatalf("rule should be stored under fallback key id, got %+v", store.rules)
	}

	// GET lists it back for the same caller.
	req = authRequest("GET", "/api/visibility", "")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET want 200, got %d: %s", w.Code, w.Body.String())
	}
	rules := decodeVisRules(t, w)
	if len(rules) != 1 || rules[0]["rule_type"] != "author" || rules[0]["value"] != "carol" {
		t.Fatalf("GET unexpected rules: %v", rules)
	}

	// DELETE removes it.
	req = authRequest("DELETE", "/api/visibility", `{"rule_type":"author","value":"carol"}`)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE want 204, got %d: %s", w.Code, w.Body.String())
	}
	if len(store.rules) != 0 {
		t.Fatalf("expected no rules after delete, got %+v", store.rules)
	}
}
