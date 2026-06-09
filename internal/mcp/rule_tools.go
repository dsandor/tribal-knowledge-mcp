package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// HandleRuleStore returns a handler that stores a new rule.
func HandleRuleStore(store storage.RuleStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		title := req.GetString("title", "")
		content := req.GetString("content", "")
		if title == "" || content == "" {
			return mcplib.NewToolResultError("title and content are required"), nil
		}

		scope := req.GetString("scope", "team")
		if scope != "team" && scope != "category" && scope != "user" {
			return mcplib.NewToolResultError(fmt.Sprintf("invalid scope %q: must be team, category, or user", scope)), nil
		}

		rule := storage.Rule{
			Title:      title,
			Content:    content,
			Scope:      storage.RuleScope(scope),
			ScopeValue: req.GetString("scope_value", ""),
			Priority:   req.GetInt("priority", 0),
			Author:     req.GetString("author", ""),
			Enabled:    true,
		}

		id, err := store.StoreRule(ctx, rule)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("store rule failed: %v", err)), nil
		}

		return mcplib.NewToolResultText(fmt.Sprintf("stored rule with id=%s", id)), nil
	}
}

// HandleRuleGet returns a handler that fetches a rule by ID.
func HandleRuleGet(store storage.RuleStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		if id == "" {
			return mcplib.NewToolResultError("id is required"), nil
		}

		rule, err := store.GetRule(ctx, id)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("rule not found: %v", err)), nil
		}

		data, err := json.Marshal(rule)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("marshal rule: %v", err)), nil
		}
		return mcplib.NewToolResultText(string(data)), nil
	}
}

// HandleRuleList returns a handler that lists rules with optional filtering.
func HandleRuleList(store storage.RuleStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		filter := storage.RuleFilter{
			Scope:      storage.RuleScope(req.GetString("scope", "")),
			ScopeValue: req.GetString("scope_value", ""),
			Limit:      req.GetInt("limit", 20),
		}

		rules, err := store.ListRules(ctx, filter)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("list rules failed: %v", err)), nil
		}

		// Guarantee a JSON array (never null) for an empty result.
		if rules == nil {
			rules = []storage.Rule{}
		}

		data, err := json.Marshal(rules)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("marshal rules: %v", err)), nil
		}
		return mcplib.NewToolResultText(string(data)), nil
	}
}

// HandleRuleUpdate returns a handler that updates an existing rule.
func HandleRuleUpdate(store storage.RuleStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		title := req.GetString("title", "")
		content := req.GetString("content", "")
		if id == "" || title == "" || content == "" {
			return mcplib.NewToolResultError("id, title, and content are required"), nil
		}

		scope := req.GetString("scope", "team")
		if scope != "team" && scope != "category" && scope != "user" {
			return mcplib.NewToolResultError(fmt.Sprintf("invalid scope %q: must be team, category, or user", scope)), nil
		}

		enabledStr := req.GetString("enabled", "true")
		enabled := enabledStr != "false" && enabledStr != "0"

		rule := storage.Rule{
			ID:         id,
			Title:      title,
			Content:    content,
			Scope:      storage.RuleScope(scope),
			ScopeValue: req.GetString("scope_value", ""),
			Priority:   req.GetInt("priority", 0),
			Author:     req.GetString("author", ""),
			Enabled:    enabled,
		}

		if err := store.UpdateRule(ctx, rule); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("update rule failed: %v", err)), nil
		}

		return mcplib.NewToolResultText(fmt.Sprintf("updated rule %s", id)), nil
	}
}

// HandleRuleDelete returns a handler that deletes a rule by ID.
func HandleRuleDelete(store storage.RuleStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		if id == "" {
			return mcplib.NewToolResultError("id is required"), nil
		}

		if err := store.DeleteRule(ctx, id); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("delete rule failed: %v", err)), nil
		}

		return mcplib.NewToolResultText("rule deleted"), nil
	}
}

// HandlePromptEnhance returns a handler that enhances a prompt with applicable rules.
func HandlePromptEnhance(store storage.RuleStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		prompt := req.GetString("prompt", "")
		if prompt == "" {
			return mcplib.NewToolResultError("prompt is required"), nil
		}

		team := req.GetString("team", "")
		category := req.GetString("category", "")
		user := req.GetString("user", "")

		rules, err := store.GetApplicableRules(ctx, team, category, user)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("get applicable rules failed: %v", err)), nil
		}

		if len(rules) == 0 {
			return mcplib.NewToolResultText(prompt), nil
		}

		return mcplib.NewToolResultText(buildEnhancedPrompt(prompt, rules)), nil
	}
}

// buildEnhancedPrompt prepends applicable rules as a numbered preamble before the original prompt.
func buildEnhancedPrompt(prompt string, rules []storage.Rule) string {
	var b strings.Builder
	b.WriteString("The following rules apply to this request:\n\n")
	for i, r := range rules {
		fmt.Fprintf(&b, "%d. [%s] %s: %s\n", i+1, ruleScopeLabel(r), r.Title, r.Content)
	}
	b.WriteString("\n---\n\n")
	b.WriteString(prompt)
	return b.String()
}

// ruleScopeLabel returns a human-readable scope label for a rule.
func ruleScopeLabel(r storage.Rule) string {
	switch r.Scope {
	case storage.RuleScopeTeam:
		return "team"
	case storage.RuleScopeCategory:
		return "category"
	case storage.RuleScopeUser:
		return "user"
	default:
		return string(r.Scope)
	}
}
