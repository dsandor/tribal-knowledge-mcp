package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/storage"
)

// --- mockRuleStore ---

type mockRuleStore struct {
	rules    []storage.Rule
	storeErr error
	getErr   error
	listErr  error
	updateErr error
	deleteErr error
	applyErr  error
}

func (m *mockRuleStore) StoreRule(_ context.Context, rule storage.Rule) (string, error) {
	if m.storeErr != nil {
		return "", m.storeErr
	}
	rule.ID = "rule-" + rule.Title
	rule.CreatedAt = time.Now()
	rule.UpdatedAt = time.Now()
	m.rules = append(m.rules, rule)
	return rule.ID, nil
}

func (m *mockRuleStore) GetRule(_ context.Context, id string) (*storage.Rule, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for _, r := range m.rules {
		if r.ID == id {
			cp := r
			return &cp, nil
		}
	}
	return nil, errors.New("not found")
}

func (m *mockRuleStore) ListRules(_ context.Context, filter storage.RuleFilter) ([]storage.Rule, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var out []storage.Rule
	for _, r := range m.rules {
		if filter.Scope != "" && r.Scope != filter.Scope {
			continue
		}
		if filter.ScopeValue != "" && r.ScopeValue != filter.ScopeValue {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (m *mockRuleStore) UpdateRule(_ context.Context, rule storage.Rule) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	for i, r := range m.rules {
		if r.ID == rule.ID {
			m.rules[i] = rule
			return nil
		}
	}
	return errors.New("not found")
}

func (m *mockRuleStore) DeleteRule(_ context.Context, id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	for i, r := range m.rules {
		if r.ID == id {
			m.rules = append(m.rules[:i], m.rules[i+1:]...)
			return nil
		}
	}
	return errors.New("not found")
}

// GetApplicableRules returns only enabled rules matching any of the given team/category/user values.
func (m *mockRuleStore) GetApplicableRules(_ context.Context, team, category, user string) ([]storage.Rule, error) {
	if m.applyErr != nil {
		return nil, m.applyErr
	}
	var out []storage.Rule
	for _, r := range m.rules {
		if !r.Enabled {
			continue
		}
		switch r.Scope {
		case storage.RuleScopeTeam:
			if team != "" && r.ScopeValue == team {
				out = append(out, r)
			}
		case storage.RuleScopeCategory:
			if category != "" && r.ScopeValue == category {
				out = append(out, r)
			}
		case storage.RuleScopeUser:
			if user != "" && r.ScopeValue == user {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

// --- tests ---

func TestHandleRuleStore_Success(t *testing.T) {
	store := &mockRuleStore{}
	handler := internalmcp.HandleRuleStore(store)
	req := callReq(
		"title", "My Rule",
		"content", "Always use bullet points.",
		"scope", "team",
		"scope_value", "finance",
		"priority", float64(5),
		"author", "alice",
	)

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}
	text := textContent(result)
	if !strings.Contains(text, "id=") {
		t.Errorf("expected response to contain 'id=', got %q", text)
	}
	if len(store.rules) != 1 {
		t.Fatalf("expected 1 stored rule, got %d", len(store.rules))
	}
}

func TestHandleRuleStore_MissingRequired(t *testing.T) {
	store := &mockRuleStore{}
	handler := internalmcp.HandleRuleStore(store)
	// missing content
	req := callReq("title", "Only Title")

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for missing content")
	}
}

func TestHandleRuleStore_InvalidScope(t *testing.T) {
	store := &mockRuleStore{}
	handler := internalmcp.HandleRuleStore(store)
	req := callReq(
		"title", "Bad Scope",
		"content", "Some content",
		"scope", "invalid",
	)

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for invalid scope")
	}
}

func TestHandleRuleGet_Success(t *testing.T) {
	store := &mockRuleStore{}
	// seed a rule directly
	_, err := store.StoreRule(context.Background(), storage.Rule{
		Title:   "FetchMe",
		Content: "Rule content here",
		Scope:   storage.RuleScopeTeam,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("seed StoreRule: %v", err)
	}
	id := store.rules[0].ID

	handler := internalmcp.HandleRuleGet(store)
	result, err := handler(context.Background(), callReq("id", id))
	if err != nil {
		t.Fatalf("get handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}

	var rule map[string]any
	if err := json.Unmarshal([]byte(textContent(result)), &rule); err != nil {
		t.Fatalf("parse result JSON: %v", err)
	}
	if rule["Title"] != "FetchMe" {
		t.Errorf("title: got %v, want FetchMe", rule["Title"])
	}
}

func TestHandleRuleGet_Missing(t *testing.T) {
	store := &mockRuleStore{}
	handler := internalmcp.HandleRuleGet(store)
	result, err := handler(context.Background(), callReq("id", "nonexistent"))
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for unknown id")
	}
}

func TestHandleRuleList(t *testing.T) {
	store := &mockRuleStore{}
	for _, title := range []string{"Rule A", "Rule B"} {
		store.StoreRule(context.Background(), storage.Rule{
			Title:   title,
			Content: "content",
			Scope:   storage.RuleScopeTeam,
			Enabled: true,
		})
	}

	handler := internalmcp.HandleRuleList(store)
	result, err := handler(context.Background(), callReq("limit", float64(20)))
	if err != nil {
		t.Fatalf("list handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}

	var rules []map[string]any
	if err := json.Unmarshal([]byte(textContent(result)), &rules); err != nil {
		t.Fatalf("parse result JSON: %v", err)
	}
	if len(rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(rules))
	}
}

func TestHandleRuleUpdate_Success(t *testing.T) {
	store := &mockRuleStore{}
	store.StoreRule(context.Background(), storage.Rule{
		Title:   "Original",
		Content: "Old content",
		Scope:   storage.RuleScopeTeam,
		Enabled: true,
	})
	id := store.rules[0].ID

	handler := internalmcp.HandleRuleUpdate(store)
	req := callReq(
		"id", id,
		"title", "Updated Title",
		"content", "New content",
		"enabled", "false",
	)

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("update handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}

	// verify mutation in store
	if store.rules[0].Title != "Updated Title" {
		t.Errorf("title: got %q, want %q", store.rules[0].Title, "Updated Title")
	}
	if store.rules[0].Enabled != false {
		t.Error("expected Enabled=false after update")
	}
}

func TestHandleRuleDelete_Success(t *testing.T) {
	store := &mockRuleStore{}
	store.StoreRule(context.Background(), storage.Rule{
		Title:   "ToDelete",
		Content: "delete me",
		Scope:   storage.RuleScopeTeam,
		Enabled: true,
	})
	id := store.rules[0].ID

	handler := internalmcp.HandleRuleDelete(store)
	result, err := handler(context.Background(), callReq("id", id))
	if err != nil {
		t.Fatalf("delete handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}
	if len(store.rules) != 0 {
		t.Errorf("expected 0 rules after delete, got %d", len(store.rules))
	}
}

func TestHandlePromptEnhance_WithRules(t *testing.T) {
	store := &mockRuleStore{}
	store.StoreRule(context.Background(), storage.Rule{
		Title:      "Team Rule 1",
		Content:    "Always cite your sources.",
		Scope:      storage.RuleScopeTeam,
		ScopeValue: "analysts",
		Enabled:    true,
	})
	store.StoreRule(context.Background(), storage.Rule{
		Title:      "Team Rule 2",
		Content:    "Use bullet points for lists.",
		Scope:      storage.RuleScopeTeam,
		ScopeValue: "analysts",
		Enabled:    true,
	})

	originalPrompt := "Summarize the Q3 earnings report."

	handler := internalmcp.HandlePromptEnhance(store)
	req := callReq(
		"prompt", originalPrompt,
		"team", "analysts",
	)

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("enhance handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}

	text := textContent(result)
	if !strings.Contains(text, originalPrompt) {
		t.Errorf("expected result to contain original prompt %q", originalPrompt)
	}
	if !strings.Contains(text, "Always cite your sources.") {
		t.Error("expected result to contain rule 1 content")
	}
	if !strings.Contains(text, "Use bullet points for lists.") {
		t.Error("expected result to contain rule 2 content")
	}
}

func TestHandlePromptEnhance_NoRules_ReturnsOriginal(t *testing.T) {
	store := &mockRuleStore{}
	originalPrompt := "Analyze this data set."

	handler := internalmcp.HandlePromptEnhance(store)
	req := callReq(
		"prompt", originalPrompt,
		"team", "no-such-team",
	)

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("enhance handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}

	text := textContent(result)
	if text != originalPrompt {
		t.Errorf("expected result == original prompt, got %q", text)
	}
}

func TestHandlePromptEnhance_MissingPrompt(t *testing.T) {
	store := &mockRuleStore{}
	handler := internalmcp.HandlePromptEnhance(store)
	result, err := handler(context.Background(), callReq("team", "analysts"))
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true when prompt is missing")
	}
}
