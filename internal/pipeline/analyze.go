package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dsandor/memory/internal/llm"
	"github.com/dsandor/memory/internal/storage"
)

// SummarizeResult is the LLM-generated title and summary for a cluster.
type SummarizeResult struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

// SummarizeCluster asks the LLM to produce a title and summary for a group of entries.
func SummarizeCluster(ctx context.Context, client llm.Client, entries []storage.KnowledgeEntry) (SummarizeResult, error) {
	var sb strings.Builder
	for i, e := range entries {
		fmt.Fprintf(&sb, "%d. Title: %s\nContent: %s\n\n", i+1, e.Title, truncate(e.Content, 200))
	}
	prompt := fmt.Sprintf(
		"Summarize this group of related knowledge entries into a concise title and 2-3 sentence summary.\n"+
			"Return ONLY valid JSON: {\"title\": \"...\", \"summary\": \"...\"}\n\nEntries:\n%s",
		sb.String(),
	)
	resp, err := client.Complete(ctx, prompt)
	if err != nil {
		return SummarizeResult{}, fmt.Errorf("llm: %w", err)
	}
	var result SummarizeResult
	if err := json.Unmarshal([]byte(llm.ExtractJSON(resp)), &result); err != nil {
		return SummarizeResult{}, fmt.Errorf("parse summarize response %q: %w", resp, err)
	}
	return result, nil
}

// QualityScore holds the LLM-assessed quality metrics for an entry.
type QualityScore struct {
	Coherence   float64 `json:"coherence"`
	Specificity float64 `json:"specificity"`
	Total       float64
}

// ScoreEntry asks the LLM to evaluate the coherence and specificity of an entry.
func ScoreEntry(ctx context.Context, client llm.Client, entry storage.KnowledgeEntry) (QualityScore, error) {
	prompt := fmt.Sprintf(
		"Evaluate this knowledge entry on two dimensions from 0.0 to 1.0:\n"+
			"- coherence: how clear, well-structured, and self-consistent the content is\n"+
			"- specificity: how actionable and domain-specific (vs. generic) the content is\n"+
			"Return ONLY valid JSON: {\"coherence\": 0.0, \"specificity\": 0.0}\n\n"+
			"Title: %s\nContent: %s",
		entry.Title, truncate(entry.Content, 300),
	)
	resp, err := client.Complete(ctx, prompt)
	if err != nil {
		return QualityScore{}, fmt.Errorf("llm: %w", err)
	}
	var score QualityScore
	if err := json.Unmarshal([]byte(llm.ExtractJSON(resp)), &score); err != nil {
		return QualityScore{}, fmt.Errorf("parse score response %q: %w", resp, err)
	}
	score.Total = score.Coherence + score.Specificity
	return score, nil
}

// DomainGap represents a domain with insufficient or missing knowledge coverage.
type DomainGap struct {
	Domain         string `json:"domain"`
	Description    string `json:"description"`
	EntryCount     int    `json:"entry_count"`
	Recommendation string `json:"recommendation"`
}

// DetectGaps asks the LLM to identify domains with insufficient coverage.
func DetectGaps(ctx context.Context, client llm.Client, domainCounts map[string]int) ([]DomainGap, error) {
	var sb strings.Builder
	for d, n := range domainCounts {
		fmt.Fprintf(&sb, "- %s: %d entries\n", d, n)
	}
	prompt := fmt.Sprintf(
		"Analyze this domain coverage for a team knowledge base and identify gaps — domains with insufficient entries or missing valuable domains.\n"+
			"Return ONLY valid JSON: {\"gaps\": [{\"domain\": \"...\", \"description\": \"...\", \"entry_count\": 0, \"recommendation\": \"...\"}]}\n"+
			"If no gaps found, return {\"gaps\": []}.\n\nDomain coverage:\n%s",
		sb.String(),
	)
	resp, err := client.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}
	var result struct {
		Gaps []DomainGap `json:"gaps"`
	}
	if err := json.Unmarshal([]byte(llm.ExtractJSON(resp)), &result); err != nil {
		return nil, fmt.Errorf("parse gaps response %q: %w", resp, err)
	}
	return result.Gaps, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
