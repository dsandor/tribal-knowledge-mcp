// Package visibility implements per-user knowledge suppression rules. A user
// may hide individual items, or everything by a given author, tag, or domain.
// Rules never hide the user's own entries.
//
// This package is pure: it depends only on storage's data types and may be
// safely imported by retrieval and web layers without creating cycles.
package visibility

import (
	"strings"

	"github.com/dsandor/memory/internal/storage"
)

// RuleSet is a compiled set of a single user's suppression rules for fast
// lookup. The zero value is valid and hides nothing.
type RuleSet struct {
	items   map[string]struct{} // exact entry IDs (case-sensitive)
	authors map[string]struct{} // lowercased authors
	tags    map[string]struct{} // lowercased tags
	domains map[string]struct{} // lowercased domains
	// owners holds the lowercased identities (id/email/name) that count as the
	// calling user's own — entries authored by any of these are never hidden.
	owners map[string]struct{}
}

// Compile builds a RuleSet from a user's rules plus the identities that count
// as "the user's own" (so their own entries are never hidden). Pass the user's
// id, email, and name (any non-empty ones); empties are ignored.
func Compile(rules []storage.VisibilityRule, ownerIdentities ...string) RuleSet {
	rs := RuleSet{
		items:   map[string]struct{}{},
		authors: map[string]struct{}{},
		tags:    map[string]struct{}{},
		domains: map[string]struct{}{},
		owners:  map[string]struct{}{},
	}
	for _, r := range rules {
		switch r.RuleType {
		case "item":
			if r.Value != "" {
				rs.items[r.Value] = struct{}{}
			}
		case "author":
			if r.Value != "" {
				rs.authors[strings.ToLower(r.Value)] = struct{}{}
			}
		case "tag":
			if r.Value != "" {
				rs.tags[strings.ToLower(r.Value)] = struct{}{}
			}
		case "domain":
			if r.Value != "" {
				rs.domains[strings.ToLower(r.Value)] = struct{}{}
			}
		}
	}
	for _, id := range ownerIdentities {
		if id != "" {
			rs.owners[strings.ToLower(id)] = struct{}{}
		}
	}
	return rs
}

// Hidden reports whether entry e should be hidden from this user.
//
//   - If e.Author matches any non-empty owner identity (case-insensitive), the
//     entry is the user's own and is never hidden.
//   - Otherwise the entry is hidden iff an item rule equals e.ID, an author rule
//     equals e.Author, a domain rule equals e.Domain, or a tag rule equals any
//     value in e.Tags ∪ e.AutoTags. Author/tag/domain compare case-insensitively;
//     item id compares exactly.
//   - An empty (or zero-value) rule set hides nothing.
func (rs RuleSet) Hidden(e storage.KnowledgeEntry) bool {
	// Own-entry exemption.
	if len(rs.owners) > 0 && e.Author != "" {
		if _, ok := rs.owners[strings.ToLower(e.Author)]; ok {
			return false
		}
	}

	if len(rs.items) > 0 {
		if _, ok := rs.items[e.ID]; ok {
			return true
		}
	}
	if len(rs.authors) > 0 && e.Author != "" {
		if _, ok := rs.authors[strings.ToLower(e.Author)]; ok {
			return true
		}
	}
	if len(rs.domains) > 0 && e.Domain != "" {
		if _, ok := rs.domains[strings.ToLower(e.Domain)]; ok {
			return true
		}
	}
	if len(rs.tags) > 0 {
		for _, t := range e.Tags {
			if _, ok := rs.tags[strings.ToLower(t)]; ok {
				return true
			}
		}
		for _, t := range e.AutoTags {
			if _, ok := rs.tags[strings.ToLower(t)]; ok {
				return true
			}
		}
	}
	return false
}

// empty reports whether the rule set contains no suppression rules. A rule set
// with no rules can never hide anything, so callers can skip filtering.
func (rs RuleSet) empty() bool {
	return len(rs.items) == 0 && len(rs.authors) == 0 &&
		len(rs.tags) == 0 && len(rs.domains) == 0
}

// FilterEntries returns the visible subset of entries (order preserved).
func FilterEntries(rs RuleSet, entries []storage.KnowledgeEntry) []storage.KnowledgeEntry {
	if rs.empty() {
		return entries
	}
	out := entries[:0:0] // new backing array; preserves nil-vs-empty distinction loosely
	for _, e := range entries {
		if !rs.Hidden(e) {
			out = append(out, e)
		}
	}
	return out
}

// FilterResults returns the visible subset of search results (order preserved).
func FilterResults(rs RuleSet, results []storage.SearchResult) []storage.SearchResult {
	if rs.empty() {
		return results
	}
	out := results[:0:0]
	for _, r := range results {
		if !rs.Hidden(r.Entry) {
			out = append(out, r)
		}
	}
	return out
}
