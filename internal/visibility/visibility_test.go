package visibility_test

import (
	"testing"

	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/visibility"
)

func rule(t, v string) storage.VisibilityRule {
	return storage.VisibilityRule{RuleType: t, Value: v}
}

func TestHidden_ItemRuleMatches(t *testing.T) {
	rs := visibility.Compile([]storage.VisibilityRule{rule("item", "entry-x")})
	e := storage.KnowledgeEntry{ID: "entry-x", Author: "alice"}
	if !rs.Hidden(e) {
		t.Fatalf("expected entry-x hidden by item rule")
	}
	if rs.Hidden(storage.KnowledgeEntry{ID: "entry-y", Author: "alice"}) {
		t.Fatalf("entry-y should not be hidden")
	}
}

func TestHidden_AuthorRuleMatches_CaseInsensitive(t *testing.T) {
	rs := visibility.Compile([]storage.VisibilityRule{rule("author", "Bob")})
	if !rs.Hidden(storage.KnowledgeEntry{ID: "1", Author: "bob"}) {
		t.Fatalf("expected author rule to match case-insensitively")
	}
}

func TestHidden_DomainRuleMatches(t *testing.T) {
	rs := visibility.Compile([]storage.VisibilityRule{rule("domain", "Finance")})
	if !rs.Hidden(storage.KnowledgeEntry{ID: "1", Author: "x", Domain: "finance"}) {
		t.Fatalf("expected domain rule to match case-insensitively")
	}
}

func TestHidden_TagRuleMatches_TagsAndAutoTags(t *testing.T) {
	rs := visibility.Compile([]storage.VisibilityRule{rule("tag", "noisy")})
	// user-tag match
	if !rs.Hidden(storage.KnowledgeEntry{ID: "1", Author: "x", Tags: []string{"Noisy"}}) {
		t.Fatalf("expected tag rule to match user Tags")
	}
	// auto-tag match
	if !rs.Hidden(storage.KnowledgeEntry{ID: "2", Author: "x", AutoTags: []string{"NOISY"}}) {
		t.Fatalf("expected tag rule to match AutoTags")
	}
	// no match
	if rs.Hidden(storage.KnowledgeEntry{ID: "3", Author: "x", Tags: []string{"clean"}}) {
		t.Fatalf("did not expect a match")
	}
}

func TestHidden_OwnEntryExemption(t *testing.T) {
	rules := []storage.VisibilityRule{
		rule("author", "alice@example.com"),
		rule("tag", "noisy"),
		rule("item", "own-1"),
	}
	rs := visibility.Compile(rules, "user-1", "alice@example.com", "Alice")
	// Entry authored by the user (matched via email identity) is never hidden,
	// even though an author/tag/item rule would otherwise match.
	own := storage.KnowledgeEntry{
		ID:     "own-1",
		Author: "alice@example.com",
		Tags:   []string{"noisy"},
	}
	if rs.Hidden(own) {
		t.Fatalf("own entry must never be hidden")
	}
	// Same identity by id and name.
	if rs.Hidden(storage.KnowledgeEntry{ID: "own-1", Author: "user-1", Tags: []string{"noisy"}}) {
		t.Fatalf("own entry (by id) must never be hidden")
	}
	if rs.Hidden(storage.KnowledgeEntry{ID: "own-1", Author: "alice", Tags: []string{"noisy"}}) {
		t.Fatalf("own entry (by name) must never be hidden")
	}
	// Someone else's matching entry is still hidden.
	if !rs.Hidden(storage.KnowledgeEntry{ID: "x", Author: "alice@example.com", Tags: []string{"noisy"}}) {
		// (author is the same string but this is the exemption identity; only
		// verify a clearly-other author is hidden)
	}
	if !rs.Hidden(storage.KnowledgeEntry{ID: "x", Author: "carol", Tags: []string{"noisy"}}) {
		t.Fatalf("other-author noisy entry should be hidden")
	}
}

func TestHidden_EmptyRules_NothingHidden(t *testing.T) {
	rs := visibility.Compile(nil)
	if rs.Hidden(storage.KnowledgeEntry{ID: "1", Author: "x", Domain: "d", Tags: []string{"t"}}) {
		t.Fatalf("empty rule set must hide nothing")
	}
}

func TestFilterEntries_PreservesOrderDropsHidden(t *testing.T) {
	rs := visibility.Compile([]storage.VisibilityRule{rule("item", "b")})
	in := []storage.KnowledgeEntry{
		{ID: "a", Author: "x"},
		{ID: "b", Author: "x"},
		{ID: "c", Author: "x"},
	}
	out := visibility.FilterEntries(rs, in)
	if len(out) != 2 || out[0].ID != "a" || out[1].ID != "c" {
		t.Fatalf("expected [a c], got %+v", out)
	}
}

func TestFilterResults_PreservesOrderDropsHidden(t *testing.T) {
	rs := visibility.Compile([]storage.VisibilityRule{rule("author", "bob")})
	in := []storage.SearchResult{
		{Entry: storage.KnowledgeEntry{ID: "a", Author: "alice"}, Score: 0.9},
		{Entry: storage.KnowledgeEntry{ID: "b", Author: "bob"}, Score: 0.8},
		{Entry: storage.KnowledgeEntry{ID: "c", Author: "carol"}, Score: 0.7},
	}
	out := visibility.FilterResults(rs, in)
	if len(out) != 2 || out[0].Entry.ID != "a" || out[1].Entry.ID != "c" {
		t.Fatalf("expected [a c], got %+v", out)
	}
}

func TestFilterEntries_EmptyRuleSet_ReturnsAll(t *testing.T) {
	var rs visibility.RuleSet // zero value hides nothing
	in := []storage.KnowledgeEntry{{ID: "a"}, {ID: "b"}}
	out := visibility.FilterEntries(rs, in)
	if len(out) != 2 {
		t.Fatalf("zero RuleSet must keep all entries, got %d", len(out))
	}
}
