package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/enrich"
	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/visibility"
)

// enrichmentPrefsBody is the request body for PUT /api/enrichment/prefs and the
// optional prefs_override on POST /api/enrichment/preview. Scalars are pointers
// so a null/omitted value reverts the scalar to the deployment default.
type enrichmentPrefsBody struct {
	MinRelevance  *float64 `json:"min_relevance"`
	MaxMemories   *int     `json:"max_memories"`
	LLMRewrite    *bool    `json:"llm_rewrite"`
	AllowDomains  []string `json:"allow_domains"`
	DenyDomains   []string `json:"deny_domains"`
	AllowTags     []string `json:"allow_tags"`
	DenyTags      []string `json:"deny_tags"`
	PinnedEntries []string `json:"pinned_entries"`
}

// enrichmentPrefsResponse is the JSON shape returned by GET/PUT prefs. The
// *_default booleans tell the UI when a scalar is using the deployment default
// rather than a per-user override.
type enrichmentPrefsResponse struct {
	MinRelevance        float64 `json:"min_relevance"`
	MaxMemories         int     `json:"max_memories"`
	LLMRewrite          bool    `json:"llm_rewrite"`
	MinRelevanceDefault bool    `json:"min_relevance_default"`
	MaxMemoriesDefault  bool    `json:"max_memories_default"`
	LLMRewriteDefault   bool    `json:"llm_rewrite_default"`
	Defaults            struct {
		MinRelevance float64 `json:"min_relevance"`
		MaxMemories  int     `json:"max_memories"`
	} `json:"defaults"`
	AllowDomains  []string `json:"allow_domains"`
	DenyDomains   []string `json:"deny_domains"`
	AllowTags     []string `json:"allow_tags"`
	DenyTags      []string `json:"deny_tags"`
	PinnedEntries []string `json:"pinned_entries"`
}

// resolvePrefs fills any scalar the user has not overridden with the deployment
// default (mutating in place). The *Set flags are preserved so callers can
// still report default-vs-override state.
func (s *Server) resolvePrefs(p *storage.EnrichmentPrefs) {
	if !p.MinRelevanceSet {
		p.MinRelevance = s.enrichMinRelevance
	}
	if !p.MaxMemoriesSet {
		p.MaxMemories = s.enrichMaxMemories
	}
	if !p.LLMRewriteSet {
		p.LLMRewrite = true
	}
}

// prefsResponse builds the GET/PUT JSON shape from a resolved-or-unresolved
// prefs struct. It applies defaults to the returned scalars and computes the
// *_default booleans from the *Set flags.
func (s *Server) prefsResponse(p storage.EnrichmentPrefs) enrichmentPrefsResponse {
	resp := enrichmentPrefsResponse{
		MinRelevanceDefault: !p.MinRelevanceSet,
		MaxMemoriesDefault:  !p.MaxMemoriesSet,
		LLMRewriteDefault:   !p.LLMRewriteSet,
		AllowDomains:        nonNil(p.AllowDomains),
		DenyDomains:         nonNil(p.DenyDomains),
		AllowTags:           nonNil(p.AllowTags),
		DenyTags:            nonNil(p.DenyTags),
		PinnedEntries:       nonNil(p.PinnedEntries),
	}
	resp.Defaults.MinRelevance = s.enrichMinRelevance
	resp.Defaults.MaxMemories = s.enrichMaxMemories
	s.resolvePrefs(&p)
	resp.MinRelevance = p.MinRelevance
	resp.MaxMemories = p.MaxMemories
	resp.LLMRewrite = p.LLMRewrite
	return resp
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// loadPrefs reads the caller's stored prefs, returning a zero-value (all unset)
// struct on any store error so the endpoints degrade to deployment defaults.
func (s *Server) loadPrefs(ctx context.Context, actorID string) storage.EnrichmentPrefs {
	p, err := s.store.GetEnrichmentPrefs(ctx, actorID)
	if err != nil || p == nil {
		return storage.EnrichmentPrefs{}
	}
	return *p
}

func (s *Server) handleGetEnrichmentPrefs(w http.ResponseWriter, r *http.Request) {
	actorID := auth.GetTeamContext(r.Context()).EffectiveActorID()
	prefs := s.loadPrefs(r.Context(), actorID)
	writeJSON(w, s.prefsResponse(prefs))
}

func (s *Server) handlePutEnrichmentPrefs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	actorID := auth.GetTeamContext(ctx).EffectiveActorID()

	var body enrichmentPrefsBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body")
		return
	}

	// Scalars: nil pointer reverts to default (writes SQL NULL).
	if err := s.store.PutEnrichmentPrefs(ctx, actorID, body.MinRelevance, body.MaxMemories, body.LLMRewrite); err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("put enrichment prefs: %v", err))
		return
	}

	// Rule lists: a full replace per kind. A nil slice clears the kind.
	ruleKinds := []struct {
		kind   string
		values []string
	}{
		{"allow_domain", body.AllowDomains},
		{"deny_domain", body.DenyDomains},
		{"allow_tag", body.AllowTags},
		{"deny_tag", body.DenyTags},
		{"pin_entry", body.PinnedEntries},
	}
	for _, rk := range ruleKinds {
		if err := s.store.ReplaceEnrichmentRules(ctx, actorID, rk.kind, rk.values); err != nil {
			writeError(w, 500, "internal_error", fmt.Sprintf("replace %s rules: %v", rk.kind, err))
			return
		}
	}

	prefs := s.loadPrefs(ctx, actorID)
	writeJSON(w, s.prefsResponse(prefs))
}

func (s *Server) handlePinEntry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	actorID := auth.GetTeamContext(ctx).EffectiveActorID()
	id := chi.URLParam(r, "id")
	if err := s.store.AddEnrichmentRule(ctx, actorID, "pin_entry", id); err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("pin entry: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleUnpinEntry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	actorID := auth.GetTeamContext(ctx).EffectiveActorID()
	id := chi.URLParam(r, "id")
	if err := s.store.RemoveEnrichmentRule(ctx, actorID, "pin_entry", id); err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("unpin entry: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// --- Preview ---

// applicableRuleReader is the minimal slice of storage.RuleStore the preview
// needs to load the rules that apply to a request. Stores without rule support
// simply return no applicable rules.
type applicableRuleReader interface {
	GetApplicableRules(ctx context.Context, team, category, user string) ([]storage.Rule, error)
}

type previewRequest struct {
	Prompt        string               `json:"prompt"`
	PrefsOverride *enrichmentPrefsBody `json:"prefs_override"`
}

type previewIncluded struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Domain    string  `json:"domain"`
	Relevance float64 `json:"relevance"`
	Pinned    bool    `json:"pinned"`
}

type previewExcluded struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Domain    string  `json:"domain"`
	Relevance float64 `json:"relevance"`
	Reason    string  `json:"reason"`
}

type previewRule struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Scope   string `json:"scope"`
}

type previewResponse struct {
	Included        []previewIncluded `json:"included"`
	Excluded        []previewExcluded `json:"excluded"`
	ApplicableRules []previewRule     `json:"applicable_rules"`
	ImprovedPrompt  string            `json:"improved_prompt"`
}

// prefsFromOverride converts a PUT-shaped body into a storage.EnrichmentPrefs
// with the *Set flags set for any explicitly-provided scalar. Defaults are NOT
// applied here — the caller applies them via resolvePrefs.
func prefsFromOverride(b enrichmentPrefsBody) storage.EnrichmentPrefs {
	p := storage.EnrichmentPrefs{
		AllowDomains:  b.AllowDomains,
		DenyDomains:   b.DenyDomains,
		AllowTags:     b.AllowTags,
		DenyTags:      b.DenyTags,
		PinnedEntries: b.PinnedEntries,
	}
	if b.MinRelevance != nil {
		p.MinRelevance, p.MinRelevanceSet = *b.MinRelevance, true
	}
	if b.MaxMemories != nil {
		p.MaxMemories, p.MaxMemoriesSet = *b.MaxMemories, true
	}
	if b.LLMRewrite != nil {
		p.LLMRewrite, p.LLMRewriteSet = *b.LLMRewrite, true
	}
	return p
}

func (s *Server) handlePreviewEnrichment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tc := auth.GetTeamContext(ctx)
	actorID := tc.EffectiveActorID()

	var body previewRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	if strings.TrimSpace(body.Prompt) == "" {
		writeError(w, 400, "bad_request", "prompt is required")
		return
	}

	// Resolve prefs: explicit override (unsaved form values) else saved prefs.
	var prefs storage.EnrichmentPrefs
	if body.PrefsOverride != nil {
		prefs = prefsFromOverride(*body.PrefsOverride)
	} else {
		prefs = s.loadPrefs(ctx, actorID)
	}
	s.resolvePrefs(&prefs)

	// Applicable rules: reuse the same store method enrich_context uses. The web
	// caller has no team/category/user tool args, so scope by the resolved team
	// and actor identity. Degrade gracefully if the store lacks rule support.
	applicableRules := []previewRule{}
	var rawRules []storage.Rule
	if ruleStore, ok := s.store.(applicableRuleReader); ok {
		if rules, err := ruleStore.GetApplicableRules(ctx, tc.TeamID, "", actorID); err == nil {
			rawRules = rules
			for _, rl := range rules {
				applicableRules = append(applicableRules, previewRule{
					ID:      rl.ID,
					Title:   rl.Title,
					Content: rl.Content,
					Scope:   string(rl.Scope),
				})
			}
		}
	}

	// Improved prompt: the rule-enhanced preamble only. Preview never calls the
	// LLM so it stays fast and deterministic.
	improvedPrompt := buildRulePreamble(body.Prompt, rawRules)

	included := []previewIncluded{}
	excluded := []previewExcluded{}

	// Embed the prompt. A nil embedder (unconfigured) yields empty memory lists
	// but still returns rules + the improved prompt (no 500).
	var embedder = embedderFor(s, ctx, tc.TeamID)
	if embedder != nil {
		if vec, err := embedder.Embed(ctx, body.Prompt); err == nil {
			fetchK := prefs.MaxMemories * 3
			if fetchK < 30 {
				fetchK = 30
			}
			if fetchK > 100 {
				fetchK = 100
			}
			if results, err := s.store.SearchSimilar(ctx, vec, fetchK); err == nil {
				// Team-access filter.
				filtered := results[:0]
				for _, res := range results {
					if auth.CanAccess(tc, res.Entry.TeamID) {
						filtered = append(filtered, res)
					}
				}
				results = filtered
				// Per-user visibility suppression (reuses the web list filter).
				results = visibility.FilterResults(s.callerVisibility(ctx, tc), results)

				scored := make([]enrich.ScoredEntry, 0, len(results))
				for _, res := range results {
					scored = append(scored, enrich.ScoredEntry{Entry: res.Entry, Distance: res.Score})
				}
				inc, exc := enrich.Select(scored, prefs)
				for _, c := range inc {
					included = append(included, previewIncluded{
						ID:        c.Entry.ID,
						Title:     c.Entry.Title,
						Domain:    c.Entry.Domain,
						Relevance: c.Relevance,
						Pinned:    c.Pinned,
					})
				}
				for _, c := range exc {
					excluded = append(excluded, previewExcluded{
						ID:        c.Entry.ID,
						Title:     c.Entry.Title,
						Domain:    c.Entry.Domain,
						Relevance: c.Relevance,
						Reason:    c.Reason,
					})
				}
			}
		}
	}

	writeJSON(w, previewResponse{
		Included:        included,
		Excluded:        excluded,
		ApplicableRules: applicableRules,
		ImprovedPrompt:  improvedPrompt,
	})
}

// embedderFor resolves a per-request embedder, returning nil when AI sources
// are unconfigured.
func embedderFor(s *Server, ctx context.Context, teamID string) embedderEmbed {
	if s.aiSrc == nil {
		return nil
	}
	e := s.aiSrc.Embedder(ctx, teamID)
	if e == nil {
		return nil
	}
	return e
}

// embedderEmbed is the minimal embedding capability the preview needs.
type embedderEmbed interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// buildRulePreamble prepends applicable rules as a numbered preamble before the
// original prompt — the same shape as the MCP buildEnhancedPrompt, replicated
// here so the web package need not import internal/mcp.
func buildRulePreamble(prompt string, rules []storage.Rule) string {
	if len(rules) == 0 {
		return prompt
	}
	var b strings.Builder
	b.WriteString("The following rules apply to this request:\n\n")
	for i, rl := range rules {
		fmt.Fprintf(&b, "%d. [%s] %s: %s\n", i+1, ruleScopeLabel(rl), rl.Title, rl.Content)
	}
	b.WriteString("\n---\n\n")
	b.WriteString(prompt)
	return b.String()
}

// ruleScopeLabel returns a human-readable scope label for a rule.
func ruleScopeLabel(rl storage.Rule) string {
	switch rl.Scope {
	case storage.RuleScopeTeam:
		return "team"
	case storage.RuleScopeCategory:
		return "category"
	case storage.RuleScopeUser:
		return "user"
	default:
		return string(rl.Scope)
	}
}
