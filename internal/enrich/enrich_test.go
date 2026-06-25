package enrich

import (
	"testing"

	"github.com/dsandor/memory/internal/storage"
)

func TestRelevance(t *testing.T) {
	cases := []struct{ dist, want float64 }{
		{0, 1}, {0.7, 0.3}, {1, 0}, {1.5, 0}, {-0.1, 1},
	}
	for _, c := range cases {
		got := Relevance(c.dist)
		if got < c.want-1e-9 || got > c.want+1e-9 {
			t.Errorf("Relevance(%v)=%v want %v", c.dist, got, c.want)
		}
	}
}

// findCandidate returns the candidate for the entry with the given ID, or nil.
func findCandidate(cands []Candidate, id string) *Candidate {
	for i := range cands {
		if cands[i].Entry.ID == id {
			return &cands[i]
		}
	}
	return nil
}

func TestSelect(t *testing.T) {
	t.Run("below_threshold drop", func(t *testing.T) {
		scored := []ScoredEntry{
			{Entry: storage.KnowledgeEntry{ID: "close", Domain: "api"}, Distance: 0.1}, // relevance 0.9
			{Entry: storage.KnowledgeEntry{ID: "far", Domain: "api"}, Distance: 0.9},   // relevance 0.1
		}
		prefs := storage.EnrichmentPrefs{MinRelevance: 0.3, MaxMemories: 2}
		included, excluded := Select(scored, prefs)

		if c := findCandidate(included, "close"); c == nil {
			t.Fatalf("expected close entry included")
		}
		c := findCandidate(excluded, "far")
		if c == nil {
			t.Fatalf("expected far entry excluded")
		}
		if c.Reason != "below_threshold" {
			t.Errorf("far reason = %q, want below_threshold", c.Reason)
		}
		if findCandidate(included, "far") != nil {
			t.Errorf("far entry should not be included")
		}
	})

	t.Run("denied drop", func(t *testing.T) {
		scored := []ScoredEntry{
			{Entry: storage.KnowledgeEntry{ID: "legal", Domain: "Legal"}, Distance: 0.0}, // relevance 1.0 (very close)
			{Entry: storage.KnowledgeEntry{ID: "ok", Domain: "api"}, Distance: 0.1},
		}
		prefs := storage.EnrichmentPrefs{MinRelevance: 0.3, MaxMemories: 2, DenyDomains: []string{"legal"}}
		included, excluded := Select(scored, prefs)

		if findCandidate(included, "legal") != nil {
			t.Errorf("legal domain entry should be denied even when close")
		}
		c := findCandidate(excluded, "legal")
		if c == nil || c.Reason != "denied" {
			t.Fatalf("legal entry should be excluded with reason denied, got %+v", c)
		}
		if findCandidate(included, "ok") == nil {
			t.Errorf("ok entry should be included")
		}
	})

	t.Run("deny by tag", func(t *testing.T) {
		scored := []ScoredEntry{
			{Entry: storage.KnowledgeEntry{ID: "tagged", Domain: "api", AutoTags: []string{"Secret"}}, Distance: 0.0},
		}
		prefs := storage.EnrichmentPrefs{MinRelevance: 0.3, MaxMemories: 5, DenyTags: []string{"secret"}}
		included, excluded := Select(scored, prefs)
		if findCandidate(included, "tagged") != nil {
			t.Errorf("entry with denied auto-tag should be excluded")
		}
		if c := findCandidate(excluded, "tagged"); c == nil || c.Reason != "denied" {
			t.Fatalf("expected tagged entry denied, got %+v", c)
		}
	})

	t.Run("allow-list restrict", func(t *testing.T) {
		scored := []ScoredEntry{
			{Entry: storage.KnowledgeEntry{ID: "apidoc", Domain: "docs", Tags: []string{"api"}}, Distance: 0.1},
			{Entry: storage.KnowledgeEntry{ID: "other", Domain: "docs", Tags: []string{"misc"}}, Distance: 0.1},
		}
		prefs := storage.EnrichmentPrefs{MinRelevance: 0.3, MaxMemories: 5, AllowTags: []string{"api"}}
		included, excluded := Select(scored, prefs)

		if findCandidate(included, "apidoc") == nil {
			t.Errorf("apidoc (matching allow tag) should be included")
		}
		c := findCandidate(excluded, "other")
		if c == nil || c.Reason != "not_in_allowlist" {
			t.Fatalf("other entry should be excluded not_in_allowlist, got %+v", c)
		}
	})

	t.Run("pinned below threshold included", func(t *testing.T) {
		scored := []ScoredEntry{
			{Entry: storage.KnowledgeEntry{ID: "pin", Domain: "api"}, Distance: 0.95}, // relevance 0.05
		}
		prefs := storage.EnrichmentPrefs{MinRelevance: 0.3, MaxMemories: 5, PinnedEntries: []string{"pin"}}
		included, _ := Select(scored, prefs)

		c := findCandidate(included, "pin")
		if c == nil {
			t.Fatalf("pinned entry should be included despite low relevance")
		}
		if !c.Pinned {
			t.Errorf("pinned entry should have Pinned=true")
		}
	})

	t.Run("over_max truncation pins first", func(t *testing.T) {
		scored := []ScoredEntry{
			{Entry: storage.KnowledgeEntry{ID: "a", Domain: "api"}, Distance: 0.1},   // relevance 0.9
			{Entry: storage.KnowledgeEntry{ID: "b", Domain: "api"}, Distance: 0.2},   // relevance 0.8
			{Entry: storage.KnowledgeEntry{ID: "c", Domain: "api"}, Distance: 0.3},   // relevance 0.7
			{Entry: storage.KnowledgeEntry{ID: "pin", Domain: "api"}, Distance: 0.6}, // relevance 0.4, pinned
		}
		prefs := storage.EnrichmentPrefs{MinRelevance: 0.3, MaxMemories: 2, PinnedEntries: []string{"pin"}}
		included, excluded := Select(scored, prefs)

		if len(included) != 2 {
			t.Fatalf("expected 2 included (cap), got %d", len(included))
		}
		// Pin must survive and be first.
		if included[0].Entry.ID != "pin" || !included[0].Pinned {
			t.Errorf("pinned entry should be first, got %+v", included[0])
		}
		// Highest-relevance non-pin (a) is the second slot.
		if included[1].Entry.ID != "a" {
			t.Errorf("second slot should be highest-relevance non-pin 'a', got %q", included[1].Entry.ID)
		}
		// b and c overflow.
		for _, id := range []string{"b", "c"} {
			c := findCandidate(excluded, id)
			if c == nil || c.Reason != "over_max" {
				t.Errorf("entry %q should be excluded over_max, got %+v", id, c)
			}
		}
	})
}
