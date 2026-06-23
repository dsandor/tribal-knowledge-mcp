package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dsandor/memory/internal/auth"
	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/storage"
)

// ctxWithUser returns a context carrying a user-scoped TeamContext.
func ctxWithUser(teamID, userID string) context.Context {
	return auth.WithTestTeamContext(context.Background(), auth.TeamContext{
		TeamID: teamID,
		UserID: userID,
		Role:   "member",
	})
}

// ctxWithKey returns a context carrying a team-scoped (key-only, no user id)
// TeamContext, exercising the EffectiveActorID key-id fallback.
func ctxWithKey(teamID, keyID string) context.Context {
	return auth.WithTestTeamContext(context.Background(), auth.TeamContext{
		TeamID: teamID,
		KeyID:  keyID,
		Role:   "member",
	})
}

func entryIDs(t *testing.T, jsonText string) map[string]bool {
	t.Helper()
	var entries []map[string]any
	if err := json.Unmarshal([]byte(jsonText), &entries); err != nil {
		t.Fatalf("parse result JSON: %v", err)
	}
	ids := map[string]bool{}
	for _, e := range entries {
		if id, ok := e["ID"].(string); ok {
			ids[id] = true
		}
	}
	return ids
}

// TestKnowledgeList_UserItemRule_HidesEntry verifies the per-user suppression
// filter is wired into HandleKnowledgeList: user A has an item rule hiding entry
// X, so a list as user A excludes X while user B (and an empty UserID) include it.
func TestKnowledgeList_UserItemRule_HidesEntry(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{
			{ID: "X", Title: "X", Author: "carol"},
			{ID: "Y", Title: "Y", Author: "carol"},
		},
		visRules: map[string][]storage.VisibilityRule{
			"user-a": {{RuleType: "item", Value: "X"}},
		},
		users: map[string]storage.User{
			"user-a": {ID: "user-a", Email: "a@example.com", Name: "Alice"},
			"user-b": {ID: "user-b", Email: "b@example.com", Name: "Bob"},
		},
	}

	listHandler := internalmcp.HandleKnowledgeList(store)

	// User A: X hidden.
	res, err := listHandler(ctxWithUser("t1", "user-a"), callReq("limit", float64(10)))
	if err != nil || res.IsError {
		t.Fatalf("list as user-a failed: err=%v isErr=%v", err, res.IsError)
	}
	ids := entryIDs(t, textContent(res))
	if ids["X"] {
		t.Errorf("user-a should NOT see X")
	}
	if !ids["Y"] {
		t.Errorf("user-a should see Y")
	}

	// User B: no rules, sees X.
	res, err = listHandler(ctxWithUser("t1", "user-b"), callReq("limit", float64(10)))
	if err != nil || res.IsError {
		t.Fatalf("list as user-b failed: err=%v isErr=%v", err, res.IsError)
	}
	ids = entryIDs(t, textContent(res))
	if !ids["X"] || !ids["Y"] {
		t.Errorf("user-b should see both X and Y, got %v", ids)
	}

	// Empty UserID (team token / stdio): no filtering.
	res, err = listHandler(context.Background(), callReq("limit", float64(10)))
	if err != nil || res.IsError {
		t.Fatalf("list with empty UserID failed: err=%v isErr=%v", err, res.IsError)
	}
	ids = entryIDs(t, textContent(res))
	if !ids["X"] || !ids["Y"] {
		t.Errorf("empty UserID should see both X and Y, got %v", ids)
	}
}

// TestKnowledgeList_OwnEntryExemption verifies a user's own entry is never
// hidden even when an author rule would match it.
func TestKnowledgeList_OwnEntryExemption(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{
			{ID: "own", Title: "own", Author: "a@example.com"},
			{ID: "other", Title: "other", Author: "a@example.com"},
		},
		// A self-targeting author rule (e.g. matching the user's own email) must
		// not suppress their own entries.
		visRules: map[string][]storage.VisibilityRule{
			"user-a": {{RuleType: "author", Value: "a@example.com"}},
		},
		users: map[string]storage.User{
			"user-a": {ID: "user-a", Email: "a@example.com", Name: "Alice"},
		},
	}

	listHandler := internalmcp.HandleKnowledgeList(store)
	res, err := listHandler(ctxWithUser("t1", "user-a"), callReq("limit", float64(10)))
	if err != nil || res.IsError {
		t.Fatalf("list failed: err=%v isErr=%v", err, res.IsError)
	}
	ids := entryIDs(t, textContent(res))
	if !ids["own"] || !ids["other"] {
		t.Errorf("own entries must never be hidden, got %v", ids)
	}
}

// TestKnowledgeSearch_UserItemRule_HidesEntry verifies the filter is wired into
// the search path (the most valuable end-to-end check).
func TestKnowledgeSearch_UserItemRule_HidesEntry(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{
			{ID: "X", Title: "X", Author: "carol", Content: "x"},
			{ID: "Y", Title: "Y", Author: "carol", Content: "y"},
		},
		visRules: map[string][]storage.VisibilityRule{
			"user-a": {{RuleType: "item", Value: "X"}},
		},
		users: map[string]storage.User{
			"user-a": {ID: "user-a", Email: "a@example.com", Name: "Alice"},
		},
	}
	embedder := &mockEmbedder{embedding: []float32{0.1, 0.2}}
	searchHandler := internalmcp.HandleKnowledgeSearch(store, newTestSources(embedder, nil))

	// User A: X hidden.
	res, err := searchHandler(ctxWithUser("t1", "user-a"), callReq("query", "anything", "top_k", float64(10)))
	if err != nil || res.IsError {
		t.Fatalf("search as user-a failed: err=%v isErr=%v text=%s", err, res.IsError, textContent(res))
	}
	out := textContent(res)
	if strings.Contains(out, "id=X") {
		t.Errorf("user-a search should NOT include X:\n%s", out)
	}
	if !strings.Contains(out, "id=Y") {
		t.Errorf("user-a search should include Y:\n%s", out)
	}

	// Empty UserID: no filtering.
	res, err = searchHandler(context.Background(), callReq("query", "anything", "top_k", float64(10)))
	if err != nil || res.IsError {
		t.Fatalf("search empty UserID failed: err=%v isErr=%v", err, res.IsError)
	}
	out = textContent(res)
	if !strings.Contains(out, "id=X") || !strings.Contains(out, "id=Y") {
		t.Errorf("empty UserID search should include X and Y:\n%s", out)
	}
}
