package storage

import (
	"context"
	"errors"
	"testing"
)

func TestStoreAndGetRule(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()

	id, err := s.StoreRule(ctx, Rule{
		Title:      "Use bullet points",
		Content:    "Always format output as bullet points for clarity.",
		Scope:      RuleScopeTeam,
		ScopeValue: "team-alpha",
		Priority:   10,
		Enabled:    true,
		Author:     "alice",
	})
	if err != nil {
		t.Fatalf("StoreRule: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty id")
	}

	r, err := s.GetRule(ctx, id)
	if err != nil {
		t.Fatalf("GetRule: %v", err)
	}
	if r.Title != "Use bullet points" {
		t.Errorf("title = %q, want %q", r.Title, "Use bullet points")
	}
	if r.Scope != RuleScopeTeam {
		t.Errorf("scope = %q, want %q", r.Scope, RuleScopeTeam)
	}
	if r.Priority != 10 {
		t.Errorf("priority = %d, want 10", r.Priority)
	}
	if !r.Enabled {
		t.Error("expected Enabled=true")
	}
}

func TestGetRule_NotFound(t *testing.T) {
	s := newTestAnalysisStore(t)
	_, err := s.GetRule(context.Background(), "nonexistent-id")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestListRules(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()

	for i, title := range []string{"Rule A", "Rule B", "Rule C"} {
		_, err := s.StoreRule(ctx, Rule{
			Title:      title,
			Content:    "content",
			Scope:      RuleScopeTeam,
			ScopeValue: "team-alpha",
			Priority:   i,
			Enabled:    true,
		})
		if err != nil {
			t.Fatalf("StoreRule %s: %v", title, err)
		}
	}
	_, _ = s.StoreRule(ctx, Rule{
		Title: "User rule", Content: "user content",
		Scope: RuleScopeUser, ScopeValue: "bob", Enabled: true,
	})

	rules, err := s.ListRules(ctx, RuleFilter{Scope: RuleScopeTeam, ScopeValue: "team-alpha"})
	if err != nil {
		t.Fatalf("ListRules: %v", err)
	}
	if len(rules) != 3 {
		t.Errorf("want 3 rules, got %d", len(rules))
	}
	for i := 1; i < len(rules); i++ {
		if rules[i-1].Priority < rules[i].Priority {
			t.Errorf("expected descending priority order at index %d: got %d then %d", i, rules[i-1].Priority, rules[i].Priority)
		}
	}
}

func TestUpdateRule(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()

	id, err := s.StoreRule(ctx, Rule{
		Title: "Original", Content: "original content",
		Scope: RuleScopeTeam, ScopeValue: "team-x", Enabled: true,
	})
	if err != nil {
		t.Fatalf("StoreRule: %v", err)
	}

	err = s.UpdateRule(ctx, Rule{
		ID:         id,
		Title:      "Updated",
		Content:    "updated content",
		Scope:      RuleScopeTeam,
		ScopeValue: "team-x",
		Priority:   5,
		Enabled:    false,
	})
	if err != nil {
		t.Fatalf("UpdateRule: %v", err)
	}

	r, _ := s.GetRule(ctx, id)
	if r.Title != "Updated" {
		t.Errorf("title = %q, want Updated", r.Title)
	}
	if r.Enabled {
		t.Error("expected Enabled=false after update")
	}
	if r.Priority != 5 {
		t.Errorf("priority = %d, want 5", r.Priority)
	}
}

func TestUpdateRule_NotFound(t *testing.T) {
	s := newTestAnalysisStore(t)
	err := s.UpdateRule(context.Background(), Rule{ID: "bad-id", Title: "x", Content: "y"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestDeleteRule(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()

	id, err := s.StoreRule(ctx, Rule{
		Title: "Temp", Content: "temp", Scope: RuleScopeTeam, Enabled: true,
	})
	if err != nil {
		t.Fatalf("StoreRule: %v", err)
	}
	if err := s.DeleteRule(ctx, id); err != nil {
		t.Fatalf("DeleteRule: %v", err)
	}
	if _, err := s.GetRule(ctx, id); err == nil {
		t.Error("expected rule to be gone after delete")
	}
}

func TestDeleteRule_NotFound(t *testing.T) {
	s := newTestAnalysisStore(t)
	err := s.DeleteRule(context.Background(), "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestGetApplicableRules(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()

	_, _ = s.StoreRule(ctx, Rule{
		Title: "Team formatting", Content: "Use markdown tables.",
		Scope: RuleScopeTeam, ScopeValue: "alpha", Priority: 5, Enabled: true,
	})
	_, _ = s.StoreRule(ctx, Rule{
		Title: "Finance precision", Content: "Always show 2 decimal places.",
		Scope: RuleScopeCategory, ScopeValue: "finance", Priority: 3, Enabled: true,
	})
	_, _ = s.StoreRule(ctx, Rule{
		Title: "Alice style", Content: "Be concise.",
		Scope: RuleScopeUser, ScopeValue: "alice", Priority: 1, Enabled: true,
	})
	_, _ = s.StoreRule(ctx, Rule{
		Title: "Disabled", Content: "Should not appear.",
		Scope: RuleScopeTeam, ScopeValue: "alpha", Priority: 10, Enabled: false,
	})
	_, _ = s.StoreRule(ctx, Rule{
		Title: "Other team", Content: "Different team.",
		Scope: RuleScopeTeam, ScopeValue: "beta", Priority: 5, Enabled: true,
	})

	rules, err := s.GetApplicableRules(ctx, "alpha", "finance", "alice")
	if err != nil {
		t.Fatalf("GetApplicableRules: %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("want 3 rules, got %d: %+v", len(rules), rules)
	}
	if rules[0].Scope != RuleScopeTeam {
		t.Errorf("rules[0] scope = %q, want team", rules[0].Scope)
	}
	if rules[1].Scope != RuleScopeCategory {
		t.Errorf("rules[1] scope = %q, want category", rules[1].Scope)
	}
	if rules[2].Scope != RuleScopeUser {
		t.Errorf("rules[2] scope = %q, want user", rules[2].Scope)
	}
}

func TestGetApplicableRules_EmptyContext(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()

	_, _ = s.StoreRule(ctx, Rule{
		Title: "Team rule", Content: "content",
		Scope: RuleScopeTeam, ScopeValue: "alpha", Enabled: true,
	})

	rules, err := s.GetApplicableRules(ctx, "", "", "")
	if err != nil {
		t.Fatalf("GetApplicableRules: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("want 0 rules for empty context, got %d", len(rules))
	}
}
