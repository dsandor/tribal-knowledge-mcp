// Package enrich holds the shared prompt-enrichment selection logic used by
// both the enrich_context MCP tool and the web preview endpoint.
package enrich

import (
	"sort"
	"strings"

	"github.com/dsandor/memory/internal/storage"
)

// Relevance maps a raw vector distance (lower = closer) to a 0..1 relevance
// (higher = better). For the cosine-distance embedders in use this approximates
// cosine similarity. Clamped to [0,1].
func Relevance(distance float64) float64 {
	r := 1 - distance
	if r < 0 {
		return 0
	}
	if r > 1 {
		return 1
	}
	return r
}

// EnrichDefaults are the deployment-wide fallbacks for unset per-user prefs.
type EnrichDefaults struct {
	MinRelevance float64
	MaxMemories  int
}

// Candidate is a knowledge entry evaluated against the enrichment preferences.
type Candidate struct {
	Entry     storage.KnowledgeEntry
	Relevance float64
	Included  bool
	Reason    string // "" if included; else below_threshold|denied|not_in_allowlist|over_max
	Pinned    bool
}

// ScoredEntry is the input: an entry plus its raw vector distance.
type ScoredEntry struct {
	Entry    storage.KnowledgeEntry
	Distance float64
}

// Select applies the per-user enrichment preferences to already-retrieved,
// already-team/visibility-filtered candidates. It is a pure function.
//
// Pipeline order:
//  1. deny: drop entries whose Domain is in DenyDomains or whose tags intersect
//     DenyTags (reason "denied").
//  2. allow-list: if AllowDomains or AllowTags is non-empty, keep only entries
//     matching one of them; pins are exempt (reason "not_in_allowlist").
//  3. threshold: drop entries whose Relevance < MinRelevance; pins are exempt
//     and always kept (reason "below_threshold").
//  4. sort: pinned first, then Relevance descending.
//  5. cap: keep at most MaxMemories; overflow is dropped (reason "over_max").
//     Pins count toward the cap but are prioritized so they survive.
//
// Domain/tag comparisons are case-insensitive; tag checks consider both
// Entry.Tags and Entry.AutoTags. Returns included (kept, ordered) and excluded
// (dropped, with the first applicable reason).
func Select(scored []ScoredEntry, prefs storage.EnrichmentPrefs) (included, excluded []Candidate) {
	denyDomains := toLowerSet(prefs.DenyDomains)
	denyTags := toLowerSet(prefs.DenyTags)
	allowDomains := toLowerSet(prefs.AllowDomains)
	allowTags := toLowerSet(prefs.AllowTags)
	pinned := toRawSet(prefs.PinnedEntries)
	hasAllowList := len(allowDomains) > 0 || len(allowTags) > 0

	var kept []Candidate

	for _, s := range scored {
		c := Candidate{
			Entry:     s.Entry,
			Relevance: Relevance(s.Distance),
			Pinned:    pinned[s.Entry.ID],
		}

		// 1. deny — applies to everyone, including pins.
		if denyDomains[strings.ToLower(s.Entry.Domain)] || hasDeniedTag(s.Entry, denyTags) {
			c.Reason = "denied"
			excluded = append(excluded, c)
			continue
		}

		// 2. allow-list — pins are exempt.
		if hasAllowList && !c.Pinned && !matchesAllowList(s.Entry, allowDomains, allowTags) {
			c.Reason = "not_in_allowlist"
			excluded = append(excluded, c)
			continue
		}

		// 3. threshold — pins are exempt (always kept).
		if !c.Pinned && c.Relevance < prefs.MinRelevance {
			c.Reason = "below_threshold"
			excluded = append(excluded, c)
			continue
		}

		c.Included = true
		kept = append(kept, c)
	}

	// 4. sort: pinned first, then relevance descending (stable to preserve input
	// order on ties).
	sort.SliceStable(kept, func(i, j int) bool {
		if kept[i].Pinned != kept[j].Pinned {
			return kept[i].Pinned
		}
		return kept[i].Relevance > kept[j].Relevance
	})

	// 5. cap to MaxMemories; overflow is excluded over_max.
	max := prefs.MaxMemories
	for i := range kept {
		if max > 0 && i >= max {
			kept[i].Included = false
			kept[i].Reason = "over_max"
			excluded = append(excluded, kept[i])
			continue
		}
		included = append(included, kept[i])
	}

	return included, excluded
}

func toLowerSet(vals []string) map[string]bool {
	if len(vals) == 0 {
		return nil
	}
	m := make(map[string]bool, len(vals))
	for _, v := range vals {
		m[strings.ToLower(strings.TrimSpace(v))] = true
	}
	return m
}

func toRawSet(vals []string) map[string]bool {
	if len(vals) == 0 {
		return nil
	}
	m := make(map[string]bool, len(vals))
	for _, v := range vals {
		m[v] = true
	}
	return m
}

func hasDeniedTag(e storage.KnowledgeEntry, denyTags map[string]bool) bool {
	if len(denyTags) == 0 {
		return false
	}
	return anyTagIn(e, denyTags)
}

func matchesAllowList(e storage.KnowledgeEntry, allowDomains, allowTags map[string]bool) bool {
	if len(allowDomains) > 0 && allowDomains[strings.ToLower(e.Domain)] {
		return true
	}
	if len(allowTags) > 0 && anyTagIn(e, allowTags) {
		return true
	}
	return false
}

// anyTagIn reports whether any of the entry's user tags or auto tags is in set.
func anyTagIn(e storage.KnowledgeEntry, set map[string]bool) bool {
	for _, t := range e.Tags {
		if set[strings.ToLower(t)] {
			return true
		}
	}
	for _, t := range e.AutoTags {
		if set[strings.ToLower(t)] {
			return true
		}
	}
	return false
}
