// Package sharing implements the cross-team knowledge share + import service.
// The logic here is pure (depends only on storage, aiconfig, and embedding) so
// both the MCP tool layer and the web layer can drive the same import flow
// without risking an import cycle through internal/mcp.
package sharing

import (
	"context"
	"errors"
	"fmt"

	"github.com/dsandor/memory/internal/aiconfig"
	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/storage"
)

// ErrSameTeam is returned by Import when the share's source team equals the
// destination team — the entry is already visible to that team, so there is
// nothing to import. Callers should treat this as a friendly no-op, not a hard
// failure.
var ErrSameTeam = errors.New("share already belongs to the destination team; no import needed")

// ErrShareNotFound is returned by Import when the share token does not exist.
// Callers should map this to a 404; it never embeds the token value.
var ErrShareNotFound = errors.New("share not found")

// CreateShare creates a single-use cross-team share token for entryID owned by
// sourceTeamID. The caller is expected to have already verified that createdBy
// can access the entry; we still pass sourceTeamID (the entry's team) through so
// the share records the correct provenance.
func CreateShare(ctx context.Context, store storage.Store, entryID, sourceTeamID, createdBy string) (storage.KnowledgeShare, error) {
	return store.CreateShare(ctx, entryID, sourceTeamID, createdBy)
}

// Import copies the entry behind shareID into destTeamID as a new, pending,
// re-embedded entry and burns the single-use token. It returns the new entry id.
//
// Ordering: after share validation we run a pre-flight that resolves the
// destination team's embedder. A misconfigured destination (no embedder) is a
// fail-fast condition that returns BEFORE MarkShareUsed, so the single-use token
// is NOT burned — the share stays importable once an embedder is configured.
//
// Only after the pre-flight passes do we MarkShareUsed FIRST (as an atomic
// claim) and then store the copy. MarkShareUsed errors if the token is already
// used or revoked, so this makes single-use enforcement race-free at the storage
// layer — two concurrent imports cannot both pass the claim. The trade-off is
// that if the subsequent store fails the token is already burned (a "lost"
// share). We deliberately prefer burning a share over the alternative
// (store-first) risk of two callers both creating duplicate copies from one
// token. Marking before we know the new entry id means we pass an empty
// importedEntryID; storage records used_at/used_by which is sufficient to
// enforce single-use.
func Import(ctx context.Context, store storage.Store, src *aiconfig.Sources, shareID, destTeamID, destUserID string) (string, error) {
	share, err := store.GetShare(ctx, shareID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return "", ErrShareNotFound
		}
		return "", fmt.Errorf("load share: %w", err)
	}
	if share.UsedAt != nil || share.RevokedAt != nil {
		return "", fmt.Errorf("share is no longer available")
	}
	if share.SourceTeamID == destTeamID {
		return "", ErrSameTeam
	}

	// Pre-flight: resolve the destination team's embedder BEFORE burning the
	// token. CopyEntryToTeam resolves the embedder again and errors with this
	// same message if it is nil; doing it here first means a misconfigured
	// destination does not consume the single-use share. We mirror the exact
	// error CopyEntryToTeam returns so callers see a consistent message.
	if src.Embedder(ctx, destTeamID) == nil {
		return "", fmt.Errorf("embedding not configured for your team")
	}

	// Claim the token first (single-use enforcement). See ordering note above.
	if err := store.MarkShareUsed(ctx, shareID, destUserID, ""); err != nil {
		return "", fmt.Errorf("share is no longer available: %w", err)
	}

	newID, err := CopyEntryToTeam(ctx, store, src, share.EntryID, destTeamID, "")
	if err != nil {
		return "", err
	}
	return newID, nil
}

// CopyEntryToTeam duplicates entryID into destTeamID as a new, pending,
// re-embedded entry and returns the new entry id. Unlike Import it involves no
// share token — it is the token-free copy core shared by Import and the
// superadmin "copy knowledge into teams" flow.
//
// The new entry preserves the original's content, metadata, and author
// (provenance) but is given a fresh UUID by the storage layer and lands as
// "pending" so the destination team can review it. The createdBy argument
// records who initiated the copy; it is currently informational and does not
// override the preserved author.
func CopyEntryToTeam(ctx context.Context, store storage.Store, src *aiconfig.Sources, entryID, destTeamID, createdBy string) (string, error) {
	srcEntry, err := store.GetEntry(ctx, entryID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return "", fmt.Errorf("shared entry no longer exists")
		}
		return "", fmt.Errorf("load shared entry: %w", err)
	}
	if srcEntry == nil {
		return "", fmt.Errorf("shared entry no longer exists")
	}

	// Resolve the destination team's embedder.
	embedder := src.Embedder(ctx, destTeamID)
	if embedder == nil {
		return "", fmt.Errorf("embedding not configured for your team")
	}

	// Build the destination copy. Do NOT carry the source ID — StoreEntryChunked
	// assigns a fresh UUID. Author is preserved (provenance of the original).
	entry := storage.KnowledgeEntry{
		Type:        srcEntry.Type,
		Title:       srcEntry.Title,
		Content:     srcEntry.Content,
		Description: srcEntry.Description + "\n\n(Imported from shared knowledge.)",
		Domain:      srcEntry.Domain,
		Tags:        srcEntry.Tags,
		Author:      srcEntry.Author,
		Team:        destTeamID,
		TeamID:      destTeamID,
		Status:      "pending", // set explicitly; storage backends default differently
	}

	cfg := src.ChunkConfig(ctx, destTeamID)
	chunks := embedding.Chunk(entry.Content, cfg)
	entryChunks := make([]storage.EntryChunk, 0, len(chunks))
	for _, c := range chunks {
		emb, err := embedder.Embed(ctx, c.Content)
		if err != nil {
			return "", fmt.Errorf("embedding failed: %w", err)
		}
		entryChunks = append(entryChunks, storage.EntryChunk{
			Index:         c.Index,
			Content:       c.Content,
			TokenEstimate: c.TokenEstimate,
			Embedding:     emb,
		})
	}

	newID, err := store.StoreEntryChunked(ctx, entry, entryChunks)
	if err != nil {
		return "", fmt.Errorf("store imported entry: %w", err)
	}
	return newID, nil
}
