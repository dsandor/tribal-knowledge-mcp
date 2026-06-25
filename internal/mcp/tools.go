package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/dsandor/memory/internal/aiconfig"
	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/live"
	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/tags"
	"github.com/dsandor/memory/internal/visibility"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// storeResult is the JSON shape returned by the knowledge_store tool.
type storeResult struct {
	ID                 string `json:"id"`
	Status             string `json:"status"`
	Message            string `json:"message,omitempty"`
	ChunkCount         int    `json:"chunk_count,omitempty"`
	EmbeddingMaxTokens int    `json:"embedding_max_tokens,omitempty"`
	EmbeddingSkipped   bool   `json:"embedding_skipped,omitempty"`
}

func HandleKnowledgeStore(store storage.Store, src *aiconfig.Sources, bus ...live.EventBus) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	var eventBus live.EventBus
	if len(bus) > 0 {
		eventBus = bus[0]
	}
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		title := req.GetString("title", "")
		content := req.GetString("content", "")
		entryType := req.GetString("type", "")

		if title == "" || content == "" || entryType == "" {
			return mcplib.NewToolResultError("title, content, and type are required"), nil
		}

		// Dedup check: skip embedding if identical content already exists.
		hash := contentHash(title + content)
		existing, err := store.GetEntryByContentHash(ctx, hash)
		if err != nil {
			// log content hash lookup failures but continue — StoreEntry will surface any real DB errors
			_ = err
		}
		if existing != nil {
			out, _ := json.Marshal(storeResult{ID: existing.ID, Status: "already_exists", Message: "Entry already exists with this title and content."})
			return mcplib.NewToolResultText(string(out)), nil
		}

		// dry_run: validate and preview without storing (runs after dedup so duplicates still surface as already_exists).
		domain := req.GetString("domain", "")
		description := req.GetString("description", "")
		author := req.GetString("author", "")
		team := req.GetString("team", "")
		explicitTags := tagsFromArgs(req.GetArguments(), "tags")
		entryTags := tags.Merge(explicitTags, tags.ExtractHashtags(title+" "+content))

		if req.GetBool("dry_run", false) {
			preview := map[string]any{
				"status":       "preview",
				"title":        title,
				"content":      content,
				"type":         entryType,
				"domain":       domain,
				"description":  description,
				"author":       author,
				"team":         team,
				"tags":         entryTags,
				"content_hash": hash,
			}
			out, _ := json.Marshal(preview)
			return mcplib.NewToolResultText(string(out)), nil
		}

		// Resolve actor/team once; reused for the entry literal, embedder call,
		// and the live event so we never call resolveActorTeam twice.
		_, actor := resolveActorTeam(ctx)

		// Resolve the team this entry should land in. A superadmin in see-all
		// mode (empty tc.TeamID) falls back to their home team rather than
		// writing a team-less record. With no user/home (stdio), this stays
		// empty — preserving stdio behavior.
		tc := auth.GetTeamContext(ctx)
		home := ""
		if tc.UserID != "" {
			if us, ok := store.(interface {
				GetUserByID(context.Context, string) (*storage.User, error)
			}); ok {
				if u, err := us.GetUserByID(ctx, tc.UserID); err == nil {
					home = u.TeamID
				}
			}
		}
		teamID := tc.WriteTargetTeamID(home)

		entry := storage.KnowledgeEntry{
			Type:        storage.KnowledgeType(entryType),
			Title:       title,
			Content:     content,
			Description: description,
			Domain:      domain,
			Author:      author,
			Team:        team,
			TeamID:      teamID,
			Tags:        entryTags,
		}

		// Split large content into per-chunk text. StoreEntryChunked tolerates
		// chunks with a nil Embedding (it inserts the text row and skips the
		// vector), so embedding is fail-soft: a missing embedder or an Embed
		// error stores the entry text WITHOUT vectors rather than failing the
		// tool call. Re-embedding later backfills the vectors.
		cfg := src.ChunkConfig(ctx, teamID)
		chunks := embedding.Chunk(content, cfg)
		entryChunks := make([]storage.EntryChunk, 0, len(chunks))

		// Resolve embedder per call so saved team settings take effect immediately.
		embedder := src.Embedder(ctx, teamID)
		embeddingSkipped := false
		if embedder == nil {
			embeddingSkipped = true
			slog.Warn("knowledge store: embedding skipped", "team", teamID, "err", "embedding not configured")
		}

		for _, c := range chunks {
			ec := storage.EntryChunk{
				Index:         c.Index,
				Content:       c.Content,
				TokenEstimate: c.TokenEstimate,
			}
			if embedder != nil && !embeddingSkipped {
				emb, err := embedder.Embed(ctx, c.Content)
				if err != nil {
					// Fail-soft: drop any vectors already gathered and store
					// text-only chunks so the entry still persists.
					slog.Warn("knowledge store: embedding skipped", "team", teamID, "chunk", c.Index, "err", err)
					embeddingSkipped = true
					for i := range entryChunks {
						entryChunks[i].Embedding = nil
					}
				} else {
					ec.Embedding = emb
				}
			}
			entryChunks = append(entryChunks, ec)
		}

		id, err := store.StoreEntryChunked(ctx, entry, entryChunks)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("store failed: %v", err)), nil
		}

		// Fire-and-forget auto-categorization; never blocks the tool response.
		// src is always non-nil here — the embedder resolution above already
		// dereferenced it.
		entry.ID = id
		tagger := &tags.AutoTagger{Store: store, LLMFor: src.ImprovementLLM}
		tagger.TagEntryAsync(ctx, entry, teamID)

		// Best-effort live event — must not affect the tool response or panic
		// when eventBus is nil (publishEvent is nil-safe).
		publishEvent(eventBus, live.LiveEvent{
			Type:    live.TypeKnowledgeStored,
			TeamID:  teamID,
			Actor:   actor,
			EntryID: id,
			Title:   live.CapFragment(title),
		})

		out, _ := json.Marshal(storeResult{
			ID:                 id,
			Status:             "stored",
			ChunkCount:         len(entryChunks),
			EmbeddingMaxTokens: cfg.MaxTokens,
			EmbeddingSkipped:   embeddingSkipped,
		})
		return mcplib.NewToolResultText(string(out)), nil
	}
}

// contentHash returns the lowercase hex-encoded SHA-256 digest of s.
// mirrors storage.sha256Hex — keep in sync.
func contentHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// fetchEntryForCaller fetches an entry by id and enforces team access for the
// caller. The second return is a non-nil tool error result when the entry is
// missing or belongs to another team; callers must return it as-is.
func fetchEntryForCaller(ctx context.Context, store storage.Store, id string) (*storage.KnowledgeEntry, *mcplib.CallToolResult) {
	entry, err := store.GetEntry(ctx, id)
	if err != nil {
		return nil, mcplib.NewToolResultError(fmt.Sprintf("entry not found: %v", err))
	}
	tc := auth.GetTeamContext(ctx)
	if !auth.CanAccess(tc, entry.TeamID) {
		return nil, mcplib.NewToolResultError("forbidden: entry belongs to another team")
	}
	return entry, nil
}

func HandleKnowledgeGet(store storage.Store) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		if id == "" {
			return mcplib.NewToolResultError("id is required"), nil
		}

		entry, errResult := fetchEntryForCaller(ctx, store, id)
		if errResult != nil {
			return errResult, nil
		}

		data, _ := json.Marshal(entry)
		return mcplib.NewToolResultText(string(data)), nil
	}
}

func HandleKnowledgeList(store storage.Store) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		// List reads are scoped to the caller's team; superadmins see all teams.
		teamID := auth.GetTeamContext(ctx).ListScopeTeamID()
		filter := storage.ListFilter{
			Domain: req.GetString("domain", ""),
			Type:   storage.KnowledgeType(req.GetString("type", "")),
			Limit:  req.GetInt("limit", 20),
			TeamID: teamID,
		}

		entries, err := store.ListEntries(ctx, filter)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("list failed: %v", err)), nil
		}

		// Per-user suppression (no-op for team tokens / stdio).
		entries = visibility.FilterEntries(callerVisibility(ctx, store), entries)

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

		if _, errResult := fetchEntryForCaller(ctx, store, id); errResult != nil {
			return errResult, nil
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
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
