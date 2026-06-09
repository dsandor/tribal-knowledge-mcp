package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/dsandor/memory/internal/llm"
	"github.com/dsandor/memory/internal/storage"
)

var jsonFenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)```")

func extractJSON(s string) string {
	if m := jsonFenceRe.FindStringSubmatch(s); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(s)
}

type generateResponse struct {
	SystemPrompt string `json:"system_prompt"`
	Instructions string `json:"instructions"`
	AntiPatterns string `json:"anti_patterns"`
}

// Generate creates a draft Agent from a cluster and its knowledge entries using an LLM.
// The returned Agent is not persisted; the caller must call AgentStore.UpsertAgent.
func Generate(ctx context.Context, client llm.Client, cluster storage.Cluster, entries []storage.KnowledgeEntry) (*storage.Agent, error) {
	var sb strings.Builder
	for i, e := range entries {
		fmt.Fprintf(&sb, "%d. [%s] %s\n   %s\n\n", i+1, e.Type, e.Title, truncate(e.Content, 300))
	}

	prompt := fmt.Sprintf(
		"You are building a specialized AI agent definition from a knowledge cluster.\n"+
			"Cluster: %s\nSummary: %s\n\nKnowledge entries:\n%s\n"+
			"Generate a specialized AI agent that embodies this domain expertise.\n"+
			"Return ONLY valid JSON with these three fields:\n"+
			"- system_prompt: 2-4 sentences defining this agent's role and expertise\n"+
			"- instructions: step-by-step guidelines the agent should follow (newline-separated)\n"+
			"- anti_patterns: behaviors this agent should avoid (newline-separated)\n\n"+
			"JSON only, no other text.",
		cluster.Title, cluster.Summary, sb.String(),
	)

	resp, err := client.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}

	var result generateResponse
	if err := json.Unmarshal([]byte(extractJSON(resp)), &result); err != nil {
		return nil, fmt.Errorf("parse agent response: %w", err)
	}

	return &storage.Agent{
		Domain:       cluster.Domain,
		Version:      1,
		Status:       storage.AgentStatusDraft,
		SystemPrompt: result.SystemPrompt,
		Instructions: result.Instructions,
		AntiPatterns: result.AntiPatterns,
		SourceRefs:   []string{cluster.ID},
		ClusterID:    cluster.ID,
	}, nil
}

// Refactor rewrites an agent based on user feedback, the current agent definition,
// and the relevant knowledge entries for context.
func Refactor(ctx context.Context, client llm.Client, current *storage.Agent, entries []storage.KnowledgeEntry, userFeedback string) (*storage.Agent, error) {
	var sb strings.Builder
	for i, e := range entries {
		fmt.Fprintf(&sb, "%d. [%s] %s\n   %s\n\n", i+1, e.Type, e.Title, truncate(e.Content, 300))
	}

	entriesSection := sb.String()
	if entriesSection == "" {
		entriesSection = "(no knowledge entries available)\n"
	}

	prompt := fmt.Sprintf(
		"You are refactoring an AI agent definition based on user feedback.\n\n"+
			"## Current Agent Definition\n"+
			"Domain: %s\n\n"+
			"System Prompt:\n%s\n\n"+
			"Instructions:\n%s\n\n"+
			"Anti-Patterns:\n%s\n\n"+
			"## Relevant Knowledge Entries\n%s\n"+
			"## User Feedback\n%s\n\n"+
			"Revise the agent definition to address the feedback. "+
			"Keep accurate information from the knowledge entries. "+
			"Do not narrow the agent's scope based solely on previously-seen examples — "+
			"generalise appropriately based on the domain and the feedback.\n"+
			"Return ONLY valid JSON with these three fields:\n"+
			"- system_prompt: 2-4 sentences defining this agent's role and expertise\n"+
			"- instructions: step-by-step guidelines the agent should follow (newline-separated)\n"+
			"- anti_patterns: behaviors this agent should avoid (newline-separated)\n\n"+
			"JSON only, no other text.",
		current.Domain, current.SystemPrompt, current.Instructions, current.AntiPatterns,
		entriesSection, userFeedback,
	)

	resp, err := client.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}

	var result generateResponse
	if err := json.Unmarshal([]byte(extractJSON(resp)), &result); err != nil {
		return nil, fmt.Errorf("parse refactor response: %w", err)
	}

	return &storage.Agent{
		ID:           current.ID,
		Domain:       current.Domain,
		Version:      current.Version + 1,
		Status:       storage.AgentStatusDraft,
		SystemPrompt: result.SystemPrompt,
		Instructions: result.Instructions,
		AntiPatterns: result.AntiPatterns,
		SourceRefs:   current.SourceRefs,
		ClusterID:    current.ClusterID,
	}, nil
}

func truncate(s string, maxLen int) string {
	if len([]rune(s)) <= maxLen {
		return s
	}
	return string([]rune(s)[:maxLen]) + "..."
}
