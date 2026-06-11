package tags

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dsandor/memory/internal/llm"
	"github.com/dsandor/memory/internal/storage"
)

// AutoTagger assigns LLM-generated category tags to stored entries.
// Failures are logged and dropped — callers never block on or observe them.
type AutoTagger struct {
	Store storage.Store
	// LLMFor resolves a client per call so saved team settings apply
	// immediately. Wire to (*aiconfig.Sources).ImprovementLLM.
	LLMFor func(ctx context.Context, teamID string) llm.Client
}

const autoTagTimeout = 30 * time.Second

// maxPromptContentLen caps how much entry content is sent to the LLM for
// categorization — enough signal for tags, bounded cost for huge entries.
const maxPromptContentLen = 2000

// TagEntry asks the LLM for 3-5 category tags, dedupes them against the
// entry's user tags (case-insensitive), and persists them via UpdateAutoTags.
func (a *AutoTagger) TagEntry(ctx context.Context, entry storage.KnowledgeEntry, teamID string) {
	client := a.LLMFor(ctx, teamID)
	if client == nil {
		return // no API key configured — skip silently
	}

	ctx, cancel := context.WithTimeout(ctx, autoTagTimeout)
	defer cancel()

	// Categorization needs only the opening of the content; capping keeps the
	// prompt small for large entries (transcripts, long documents).
	content := entry.Content
	if len(content) > maxPromptContentLen {
		content = content[:maxPromptContentLen]
		// Back off to a rune boundary so we never send a split UTF-8 char.
		for len(content) > 0 && !utf8.ValidString(content) {
			content = content[:len(content)-1]
		}
	}

	prompt := fmt.Sprintf(`Categorize this knowledge entry with 3-5 short lowercase topic tags (single words or hyphenated phrases). Tags should describe WHAT the entry is about, useful for browsing a team knowledge base.

Title: %s
Type: %s
Domain: %s
Content: %s

Return ONLY valid JSON: {"tags": ["...", "..."]}`,
		entry.Title, entry.Type, entry.Domain, content)

	raw, err := client.Complete(ctx, prompt)
	if err != nil {
		slog.Warn("autotag: llm complete", "entry", entry.ID, "err", err)
		return
	}

	var result struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		slog.Warn("autotag: parse llm response", "entry", entry.ID, "err", err)
		return
	}

	userTags := make(map[string]bool, len(entry.Tags))
	for _, t := range entry.Tags {
		userTags[strings.ToLower(strings.TrimSpace(t))] = true
	}
	seen := make(map[string]bool, len(result.Tags))
	autoTags := make([]string, 0, len(result.Tags))
	for _, t := range result.Tags {
		tag := strings.ToLower(strings.TrimSpace(t))
		if tag == "" || userTags[tag] || seen[tag] {
			continue
		}
		seen[tag] = true
		autoTags = append(autoTags, tag)
	}
	if len(autoTags) == 0 {
		return
	}

	if err := a.Store.UpdateAutoTags(ctx, entry.ID, autoTags); err != nil {
		slog.Warn("autotag: update auto tags", "entry", entry.ID, "err", err)
	}
}

// TagEntryAsync runs TagEntry in a goroutine on a context detached from the
// request, so the caller's response is never delayed and request cancellation
// does not abort tagging.
func (a *AutoTagger) TagEntryAsync(ctx context.Context, entry storage.KnowledgeEntry, teamID string) {
	detached := context.WithoutCancel(ctx)
	go a.TagEntry(detached, entry, teamID)
}
