package storage_test

import (
	"context"
	"testing"
)

func TestVisibilityRules_AddListDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const userA = "user-a"

	// Add three rules of different types.
	if _, err := s.AddVisibilityRule(ctx, userA, "item", "entry-1"); err != nil {
		t.Fatalf("add item rule: %v", err)
	}
	if _, err := s.AddVisibilityRule(ctx, userA, "author", "bob"); err != nil {
		t.Fatalf("add author rule: %v", err)
	}
	if _, err := s.AddVisibilityRule(ctx, userA, "tag", "noisy"); err != nil {
		t.Fatalf("add tag rule: %v", err)
	}

	rules, err := s.ListVisibilityRules(ctx, userA)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(rules))
	}
	// The returned rule should carry an ID, the user, and populated fields.
	for _, r := range rules {
		if r.ID == "" {
			t.Errorf("rule missing ID: %+v", r)
		}
		if r.UserID != userA {
			t.Errorf("rule UserID = %q, want %q", r.UserID, userA)
		}
		if r.CreatedAt.IsZero() {
			t.Errorf("rule CreatedAt is zero: %+v", r)
		}
	}
}

func TestVisibilityRules_AddIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const userA = "user-a"

	if _, err := s.AddVisibilityRule(ctx, userA, "item", "entry-1"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := s.AddVisibilityRule(ctx, userA, "author", "bob"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := s.AddVisibilityRule(ctx, userA, "tag", "noisy"); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Re-add an existing rule: must not error and must not create a duplicate.
	got, err := s.AddVisibilityRule(ctx, userA, "item", "entry-1")
	if err != nil {
		t.Fatalf("duplicate add returned error: %v", err)
	}
	if got.ID == "" {
		t.Errorf("idempotent add returned empty rule: %+v", got)
	}

	rules, err := s.ListVisibilityRules(ctx, userA)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules after duplicate add, got %d", len(rules))
	}
}

func TestVisibilityRules_Delete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const userA = "user-a"

	for _, r := range [][2]string{{"item", "entry-1"}, {"author", "bob"}, {"tag", "noisy"}} {
		if _, err := s.AddVisibilityRule(ctx, userA, r[0], r[1]); err != nil {
			t.Fatalf("add %v: %v", r, err)
		}
	}

	if err := s.DeleteVisibilityRule(ctx, userA, "author", "bob"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	rules, err := s.ListVisibilityRules(ctx, userA)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules after delete, got %d", len(rules))
	}
	for _, r := range rules {
		if r.RuleType == "author" && r.Value == "bob" {
			t.Errorf("deleted rule still present: %+v", r)
		}
	}

	// Deleting a non-existent rule is a no-op (no error).
	if err := s.DeleteVisibilityRule(ctx, userA, "author", "does-not-exist"); err != nil {
		t.Errorf("deleting absent rule returned error: %v", err)
	}
}

func TestVisibilityRules_ScopedPerUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.AddVisibilityRule(ctx, "user-a", "item", "entry-1"); err != nil {
		t.Fatalf("add for user-a: %v", err)
	}
	if _, err := s.AddVisibilityRule(ctx, "user-b", "item", "entry-2"); err != nil {
		t.Fatalf("add for user-b: %v", err)
	}

	a, err := s.ListVisibilityRules(ctx, "user-a")
	if err != nil {
		t.Fatalf("list user-a: %v", err)
	}
	if len(a) != 1 || a[0].Value != "entry-1" {
		t.Fatalf("user-a rules = %+v, want exactly entry-1", a)
	}

	b, err := s.ListVisibilityRules(ctx, "user-b")
	if err != nil {
		t.Fatalf("list user-b: %v", err)
	}
	if len(b) != 1 || b[0].Value != "entry-2" {
		t.Fatalf("user-b rules = %+v, want exactly entry-2", b)
	}
}
