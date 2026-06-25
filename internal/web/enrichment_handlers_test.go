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

// enrichStore embeds mockStore and adds controllable similarity-search results
// plus applicable rules so the preview endpoint can be exercised end to end.
type enrichStore struct {
	mockStore
	// searchResults is returned verbatim by SearchSimilar (scores = vector
	// distance). Drives the preview included/excluded split.
	searchResults []storage.SearchResult
	// rules is returned by GetApplicableRules.
	rules []storage.Rule
}

func (e *enrichStore) SearchSimilar(_ context.Context, _ []float32, _ int) ([]storage.SearchResult, error) {
	return e.searchResults, nil
}

func (e *enrichStore) GetApplicableRules(_ context.Context, _, _, _ string) ([]storage.Rule, error) {
	return e.rules, nil
}

func newEnrichServer(t *testing.T, store *enrichStore) *web.Server {
	t.Helper()
	staticFS := fstest.MapFS{
		"index.html": {Data: []byte("<html><body>app</body></html>"), Mode: 0444, ModTime: time.Now()},
	}
	return web.NewServer(staticFS, store).
		WithEnrichDefaults(0.30, 5).
		WithAISources(newReembedSources(store))
}

func decodeEnrichPrefs(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode prefs: %v", err)
	}
	return out
}

// TestEnrichmentPrefs_PutGetRoundTrip verifies a member can PUT prefs (override
// scalars + a deny_domain list) and GET them back, and that a null scalar on a
// subsequent PUT reverts to the deployment default.
func TestEnrichmentPrefs_PutGetRoundTrip(t *testing.T) {
	// apiKeyRole "member" proves these endpoints are not admin-gated.
	store := &enrichStore{mockStore: mockStore{apiKeyUserID: "user-a", apiKeyRole: "member"}}
	srv := newEnrichServer(t, store)

	// PUT overrides.
	putBody := `{"min_relevance":0.6,"max_memories":3,"llm_rewrite":false,"deny_domains":["Legal","HR"]}`
	req := authRequest("PUT", "/api/enrichment/prefs", putBody)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT want 200, got %d: %s", w.Code, w.Body.String())
	}
	got := decodeEnrichPrefs(t, w)
	if got["min_relevance"] != 0.6 {
		t.Errorf("min_relevance = %v, want 0.6", got["min_relevance"])
	}
	if got["max_memories"] != float64(3) {
		t.Errorf("max_memories = %v, want 3", got["max_memories"])
	}
	if got["llm_rewrite"] != false {
		t.Errorf("llm_rewrite = %v, want false", got["llm_rewrite"])
	}
	if got["min_relevance_default"] != false {
		t.Errorf("min_relevance_default = %v, want false", got["min_relevance_default"])
	}
	deny := got["deny_domains"].([]any)
	if len(deny) != 2 || deny[0] != "legal" || deny[1] != "hr" {
		t.Errorf("deny_domains = %v, want [legal hr]", deny)
	}

	// GET round-trips the override.
	req = authRequest("GET", "/api/enrichment/prefs", "")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET want 200, got %d: %s", w.Code, w.Body.String())
	}
	got = decodeEnrichPrefs(t, w)
	if got["min_relevance"] != 0.6 || got["max_memories"] != float64(3) {
		t.Errorf("GET scalars = %v/%v, want 0.6/3", got["min_relevance"], got["max_memories"])
	}
	if got["min_relevance_default"] != false {
		t.Errorf("GET min_relevance_default = %v, want false", got["min_relevance_default"])
	}

	// PUT with null scalar reverts to the deployment default.
	req = authRequest("PUT", "/api/enrichment/prefs", `{"min_relevance":null,"max_memories":null,"llm_rewrite":null}`)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT revert want 200, got %d: %s", w.Code, w.Body.String())
	}
	got = decodeEnrichPrefs(t, w)
	if got["min_relevance"] != 0.30 {
		t.Errorf("reverted min_relevance = %v, want 0.30 (default)", got["min_relevance"])
	}
	if got["max_memories"] != float64(5) {
		t.Errorf("reverted max_memories = %v, want 5 (default)", got["max_memories"])
	}
	if got["min_relevance_default"] != true {
		t.Errorf("reverted min_relevance_default = %v, want true", got["min_relevance_default"])
	}
	if got["llm_rewrite"] != true {
		t.Errorf("reverted llm_rewrite = %v, want true (default)", got["llm_rewrite"])
	}
	defaults := got["defaults"].(map[string]any)
	if defaults["min_relevance"] != 0.30 || defaults["max_memories"] != float64(5) {
		t.Errorf("defaults = %v, want {0.30, 5}", defaults)
	}
}

// TestEnrichmentPins_AddRemove verifies pin add/remove flows through to the
// pinned_entries list returned by GET prefs.
func TestEnrichmentPins_AddRemove(t *testing.T) {
	store := &enrichStore{mockStore: mockStore{apiKeyUserID: "user-a", apiKeyRole: "member"}}
	srv := newEnrichServer(t, store)

	// Pin an entry.
	req := authRequest("POST", "/api/enrichment/pins/entry-1", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("pin want 200, got %d: %s", w.Code, w.Body.String())
	}

	req = authRequest("GET", "/api/enrichment/prefs", "")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	got := decodeEnrichPrefs(t, w)
	pins := got["pinned_entries"].([]any)
	if len(pins) != 1 || pins[0] != "entry-1" {
		t.Fatalf("pinned_entries = %v, want [entry-1]", pins)
	}

	// Unpin it.
	req = authRequest("DELETE", "/api/enrichment/pins/entry-1", "")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("unpin want 200, got %d: %s", w.Code, w.Body.String())
	}

	req = authRequest("GET", "/api/enrichment/prefs", "")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	got = decodeEnrichPrefs(t, w)
	pins = got["pinned_entries"].([]any)
	if len(pins) != 0 {
		t.Fatalf("pinned_entries after unpin = %v, want []", pins)
	}
}

// TestEnrichmentPreview_IncludedExcluded verifies the preview endpoint splits a
// seeded set of search results into included/excluded with reasons, using a
// prefs_override (the unsaved form values) to drive selection.
func TestEnrichmentPreview_IncludedExcluded(t *testing.T) {
	store := &enrichStore{
		mockStore: mockStore{apiKeyUserID: "user-a", apiKeyRole: "member"},
		searchResults: []storage.SearchResult{
			// close → relevance 0.9 → included
			{Entry: storage.KnowledgeEntry{ID: "near", Title: "Near", Domain: "api", TeamID: "test-team"}, Score: 0.1},
			// far → relevance 0.1 → below_threshold
			{Entry: storage.KnowledgeEntry{ID: "far", Title: "Far", Domain: "api", TeamID: "test-team"}, Score: 0.9},
			// close but denied domain → denied
			{Entry: storage.KnowledgeEntry{ID: "legal", Title: "Legal", Domain: "legal", TeamID: "test-team"}, Score: 0.05},
		},
		rules: []storage.Rule{
			{ID: "r1", Title: "Be concise", Content: "Keep answers short.", Scope: storage.RuleScopeTeam},
		},
	}
	srv := newEnrichServer(t, store)

	body := `{"prompt":"how do I call the api","prefs_override":{"min_relevance":0.3,"max_memories":5,"deny_domains":["legal"]}}`
	req := authRequest("POST", "/api/enrichment/preview", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("preview want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Included []struct {
			ID        string  `json:"id"`
			Relevance float64 `json:"relevance"`
			Pinned    bool    `json:"pinned"`
		} `json:"included"`
		Excluded []struct {
			ID     string `json:"id"`
			Reason string `json:"reason"`
		} `json:"excluded"`
		ApplicableRules []struct {
			ID    string `json:"id"`
			Scope string `json:"scope"`
		} `json:"applicable_rules"`
		ImprovedPrompt string `json:"improved_prompt"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode preview: %v", err)
	}

	// Only the near entry should be included.
	if len(resp.Included) != 1 || resp.Included[0].ID != "near" {
		t.Fatalf("included = %+v, want [near]", resp.Included)
	}
	if resp.Included[0].Relevance < 0.89 || resp.Included[0].Relevance > 0.91 {
		t.Errorf("near relevance = %v, want ~0.9", resp.Included[0].Relevance)
	}

	// Excluded reasons: far below_threshold, legal denied.
	reasons := map[string]string{}
	for _, e := range resp.Excluded {
		reasons[e.ID] = e.Reason
	}
	if reasons["far"] != "below_threshold" {
		t.Errorf("far reason = %q, want below_threshold", reasons["far"])
	}
	if reasons["legal"] != "denied" {
		t.Errorf("legal reason = %q, want denied", reasons["legal"])
	}

	// Applicable rules + rule-enhanced improved prompt (no LLM in preview).
	if len(resp.ApplicableRules) != 1 || resp.ApplicableRules[0].ID != "r1" {
		t.Fatalf("applicable_rules = %+v, want [r1]", resp.ApplicableRules)
	}
	if resp.ImprovedPrompt == "" || resp.ImprovedPrompt == "how do I call the api" {
		t.Errorf("improved_prompt not rule-enhanced: %q", resp.ImprovedPrompt)
	}
}

// TestEnrichmentPreview_NilEmbedder verifies preview returns rules + empty
// memory lists (no 500) when the embedder is unconfigured.
func TestEnrichmentPreview_NilEmbedder(t *testing.T) {
	store := &enrichStore{
		mockStore: mockStore{apiKeyUserID: "user-a", apiKeyRole: "member"},
		rules:     []storage.Rule{{ID: "r1", Title: "Rule", Content: "C", Scope: storage.RuleScopeTeam}},
	}
	// No WithAISources → s.aiSrc nil → embedder nil.
	staticFS := fstest.MapFS{
		"index.html": {Data: []byte("<html></html>"), Mode: 0444, ModTime: time.Now()},
	}
	srv := web.NewServer(staticFS, store).WithEnrichDefaults(0.30, 5)

	req := authRequest("POST", "/api/enrichment/preview", `{"prompt":"hello world this is a prompt"}`)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("preview want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Included        []any `json:"included"`
		Excluded        []any `json:"excluded"`
		ApplicableRules []any `json:"applicable_rules"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Included) != 0 || len(resp.Excluded) != 0 {
		t.Errorf("expected empty memory lists, got included=%v excluded=%v", resp.Included, resp.Excluded)
	}
	if len(resp.ApplicableRules) != 1 {
		t.Errorf("expected 1 applicable rule, got %d", len(resp.ApplicableRules))
	}
}
