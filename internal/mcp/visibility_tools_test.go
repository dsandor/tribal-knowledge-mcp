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

func TestKnowledgeHide_NoUserID_UsesFallbackIdentity(t *testing.T) {
	store := &mockStore{}
	handler := internalmcp.HandleKnowledgeHide(store)

	// Empty UserID and empty KeyID (dev-bypass / stdio): the tool succeeds and
	// operates under the "local" fallback identity.
	res, err := handler(context.Background(), callReq("entry_id", "X"))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success under fallback identity, got error: %s", textContent(res))
	}
	if !findVisRule(t, store, "local", "item", "X") {
		t.Errorf("expected item rule under fallback 'local' identity, rules=%v", store.visRules)
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

func TestKnowledgeMute_NoUserID_UsesFallbackIdentity(t *testing.T) {
	store := &mockStore{}
	handler := internalmcp.HandleKnowledgeMute(store)

	// No user id and no key id: succeeds under the "local" fallback identity.
	res, err := handler(context.Background(), callReq("kind", "author", "value", "bob"))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success under fallback identity, got: %s", textContent(res))
	}
	if !findVisRule(t, store, "local", "author", "bob") {
		t.Errorf("expected author rule under fallback 'local' identity, rules=%v", store.visRules)
	}
}

// TestKnowledgeMute_KeyIDFallback verifies that when only a KeyID is present
// (a team-scoped API key, no user id) the rule is stored under the key id.
func TestKnowledgeMute_KeyIDFallback(t *testing.T) {
	store := &mockStore{}
	handler := internalmcp.HandleKnowledgeMute(store)

	ctx := ctxWithKey("t1", "key-123")
	res, err := handler(ctx, callReq("kind", "author", "value", "bob"))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success under key-id identity, got: %s", textContent(res))
	}
	if !findVisRule(t, store, "key-123", "author", "bob") {
		t.Errorf("expected author rule under key id, rules=%v", store.visRules)
	}
}

// TestKnowledgeMute_UserIDPrecedence verifies that when a real UserID is present
// it takes precedence over the key id for the stored rule's owner.
func TestKnowledgeMute_UserIDPrecedence(t *testing.T) {
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
		t.Errorf("expected rule under user id (precedence), rules=%v", store.visRules)
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

func TestKnowledgeVisibility_NoUserID_UsesFallbackIdentity(t *testing.T) {
	store := &mockStore{
		visRules: map[string][]storage.VisibilityRule{
			"local": {{RuleType: "item", Value: "X"}},
		},
	}
	handler := internalmcp.HandleKnowledgeVisibility(store)

	// No user id / no key id: lists the "local" fallback identity's rules.
	res, err := handler(context.Background(), callReq())
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success under fallback identity, got: %s", textContent(res))
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(textContent(res)), &got); err != nil {
		t.Fatalf("parse result JSON: %v\n%s", err, textContent(res))
	}
	if len(got) != 1 || got[0]["rule_type"] != "item" || got[0]["value"] != "X" {
		t.Fatalf("expected the 'local' identity's single rule, got %v", got)
	}
}

// TestRegisterVisibilityTools ensures registration wires up without panicking.
func TestRegisterVisibilityTools(t *testing.T) {
	s := server.NewMCPServer("test", "0.0.0")
	internalmcp.RegisterVisibilityTools(s, &mockStore{})
}
