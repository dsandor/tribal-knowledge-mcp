package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/storage"
	"github.com/mark3labs/mcp-go/server"
)

// findVisRule reports whether the user has a rule of the given type/value.
func findVisRule(t *testing.T, store *mockStore, userID, ruleType, value string) bool {
	t.Helper()
	rules, err := store.ListVisibilityRules(context.Background(), userID)
	if err != nil {
		t.Fatalf("ListVisibilityRules: %v", err)
	}
	for _, r := range rules {
		if r.RuleType == ruleType && r.Value == value {
			return true
		}
	}
	return false
}

func TestKnowledgeHide_AddsItemRule(t *testing.T) {
	store := &mockStore{}
	handler := internalmcp.HandleKnowledgeHide(store)

	res, err := handler(ctxWithUser("t1", "user-a"), callReq("entry_id", "X"))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %s", textContent(res))
	}
	if !findVisRule(t, store, "user-a", "item", "X") {
		t.Errorf("expected item rule for entry X, rules=%v", store.visRules["user-a"])
	}
}

func TestKnowledgeHide_NoUserID_Errors(t *testing.T) {
	store := &mockStore{}
	handler := internalmcp.HandleKnowledgeHide(store)

	// Empty UserID (team token / stdio): must error and add nothing.
	res, err := handler(context.Background(), callReq("entry_id", "X"))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected tool error for missing user id, got success: %s", textContent(res))
	}
	if len(store.visRules) != 0 {
		t.Errorf("no rules should have been added, got %v", store.visRules)
	}
}

func TestKnowledgeHide_MissingEntryID_Errors(t *testing.T) {
	store := &mockStore{}
	handler := internalmcp.HandleKnowledgeHide(store)

	res, err := handler(ctxWithUser("t1", "user-a"), callReq())
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected error for missing entry_id, got: %s", textContent(res))
	}
}

func TestKnowledgeUnhide_DeletesItemRule(t *testing.T) {
	store := &mockStore{}
	if _, err := store.AddVisibilityRule(context.Background(), "user-a", "item", "X"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	handler := internalmcp.HandleKnowledgeUnhide(store)

	res, err := handler(ctxWithUser("t1", "user-a"), callReq("entry_id", "X"))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %s", textContent(res))
	}
	if findVisRule(t, store, "user-a", "item", "X") {
		t.Errorf("item rule for X should have been deleted")
	}
}

func TestKnowledgeMute_AddsAuthorRule(t *testing.T) {
	store := &mockStore{}
	handler := internalmcp.HandleKnowledgeMute(store)

	res, err := handler(ctxWithUser("t1", "user-a"), callReq("kind", "author", "value", "bob"))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %s", textContent(res))
	}
	if !findVisRule(t, store, "user-a", "author", "bob") {
		t.Errorf("expected author rule for bob, rules=%v", store.visRules["user-a"])
	}
}

func TestKnowledgeMute_InvalidKind_Errors(t *testing.T) {
	store := &mockStore{}
	handler := internalmcp.HandleKnowledgeMute(store)

	res, err := handler(ctxWithUser("t1", "user-a"), callReq("kind", "item", "value", "X"))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected error for invalid kind, got: %s", textContent(res))
	}
	if len(store.visRules) != 0 {
		t.Errorf("no rules should have been added for invalid kind, got %v", store.visRules)
	}
}

func TestKnowledgeMute_NoUserID_Errors(t *testing.T) {
	store := &mockStore{}
	handler := internalmcp.HandleKnowledgeMute(store)

	res, err := handler(context.Background(), callReq("kind", "author", "value", "bob"))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected tool error for missing user id, got: %s", textContent(res))
	}
}

func TestKnowledgeUnmute_DeletesRule(t *testing.T) {
	store := &mockStore{}
	if _, err := store.AddVisibilityRule(context.Background(), "user-a", "tag", "noisy"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	handler := internalmcp.HandleKnowledgeUnmute(store)

	res, err := handler(ctxWithUser("t1", "user-a"), callReq("kind", "tag", "value", "noisy"))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %s", textContent(res))
	}
	if findVisRule(t, store, "user-a", "tag", "noisy") {
		t.Errorf("tag rule for noisy should have been deleted")
	}
}

func TestKnowledgeUnmute_InvalidKind_Errors(t *testing.T) {
	store := &mockStore{}
	handler := internalmcp.HandleKnowledgeUnmute(store)

	res, err := handler(ctxWithUser("t1", "user-a"), callReq("kind", "bogus", "value", "X"))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected error for invalid kind, got: %s", textContent(res))
	}
}

func TestKnowledgeVisibility_ListsRules(t *testing.T) {
	store := &mockStore{
		visRules: map[string][]storage.VisibilityRule{
			"user-a": {
				{RuleType: "item", Value: "X"},
				{RuleType: "author", Value: "bob"},
			},
		},
	}
	handler := internalmcp.HandleKnowledgeVisibility(store)

	res, err := handler(ctxWithUser("t1", "user-a"), callReq())
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %s", textContent(res))
	}

	var got []map[string]any
	if err := json.Unmarshal([]byte(textContent(res)), &got); err != nil {
		t.Fatalf("parse result JSON: %v\n%s", err, textContent(res))
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rules, got %d: %v", len(got), got)
	}
	seen := map[string]string{}
	for _, r := range got {
		seen[r["rule_type"].(string)] = r["value"].(string)
	}
	if seen["item"] != "X" || seen["author"] != "bob" {
		t.Errorf("unexpected rules: %v", got)
	}
}

func TestKnowledgeVisibility_NoUserID_Errors(t *testing.T) {
	store := &mockStore{}
	handler := internalmcp.HandleKnowledgeVisibility(store)

	res, err := handler(context.Background(), callReq())
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected tool error for missing user id, got: %s", textContent(res))
	}
}

// TestRegisterVisibilityTools ensures registration wires up without panicking.
func TestRegisterVisibilityTools(t *testing.T) {
	s := server.NewMCPServer("test", "0.0.0")
	internalmcp.RegisterVisibilityTools(s, &mockStore{})
}
