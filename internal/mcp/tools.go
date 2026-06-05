package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func HandleKnowledgeStore(store storage.Store, embedder embedding.Embedder) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		title := req.GetString("title", "")
		content := req.GetString("content", "")
		entryType := req.GetString("type", "")

		if title == "" || content == "" || entryType == "" {
			return mcplib.NewToolResultError("title, content, and type are required"), nil
		}

		entry := storage.KnowledgeEntry{
			Type:        storage.KnowledgeType(entryType),
			Title:       title,
			Content:     content,
			Description: req.GetString("description", ""),
			Domain:      req.GetString("domain", ""),
			Author:      req.GetString("author", ""),
			Team:        req.GetString("team", ""),
			Tags:        tagsFromArgs(req.GetArguments(), "tags"),
		}

		emb, err := embedder.Embed(ctx, content)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("embedding failed: %v", err)), nil
		}

		id, err := store.StoreEntry(ctx, entry, emb)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("store failed: %v", err)), nil
		}

		return mcplib.NewToolResultText(fmt.Sprintf("stored entry with id=%s", id)), nil
	}
}

func HandleKnowledgeGet(store storage.Store) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		if id == "" {
			return mcplib.NewToolResultError("id is required"), nil
		}

		entry, err := store.GetEntry(ctx, id)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("entry not found: %v", err)), nil
		}

		data, _ := json.Marshal(entry)
		return mcplib.NewToolResultText(string(data)), nil
	}
}

func HandleKnowledgeList(store storage.Store) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		filter := storage.ListFilter{
			Domain: req.GetString("domain", ""),
			Type:   storage.KnowledgeType(req.GetString("type", "")),
			Limit:  req.GetInt("limit", 20),
		}

		entries, err := store.ListEntries(ctx, filter)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("list failed: %v", err)), nil
		}

		data, _ := json.Marshal(entries)
		return mcplib.NewToolResultText(string(data)), nil
	}
}

func HandleKnowledgeDelete(store storage.Store) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		if id == "" {
			return mcplib.NewToolResultError("id is required"), nil
		}

		if err := store.DeleteEntry(ctx, id); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("delete failed: %v", err)), nil
		}

		return mcplib.NewToolResultText("entry deleted"), nil
	}
}

func tagsFromArgs(args map[string]any, key string) []string {
	raw, ok := args[key].([]any)
	if !ok {
		return []string{}
	}
	tags := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			tags = append(tags, s)
		}
	}
	return tags
}
