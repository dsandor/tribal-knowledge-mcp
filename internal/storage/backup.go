package storage

import (
	"context"
	"sort"
)

// EmbeddingItem is an engine-neutral embedding keyed by the stable entry UUID.
type EmbeddingItem struct {
	EntryID string    `json:"entry_id"`
	Vector  []float32 `json:"embedding"`
}

// BackupStore is implemented by every storage engine to support logical
// backup and restore. Row values are exchanged as ordered column->value maps;
// implementations MUST return text columns as Go strings (never []byte) so the
// JSON encoder does not base64-encode them.
type BackupStore interface {
	// EngineName returns "sqlite" or "postgres".
	EngineName() string

	// DumpTable streams every row of table as a column->value map.
	DumpTable(ctx context.Context, table string, fn func(row map[string]any) error) error

	// LoadTable inserts rows (parameterized) into table. No-op if rows is empty.
	LoadTable(ctx context.Context, table string, rows []map[string]any) error

	// DumpEmbeddings streams every embedding keyed by entry UUID.
	DumpEmbeddings(ctx context.Context, fn func(item EmbeddingItem) error) error

	// LoadEmbeddings writes embeddings in the engine's native format.
	LoadEmbeddings(ctx context.Context, items []EmbeddingItem) error

	// IsEmpty reports whether the target has no entries and no teams beyond the
	// bootstrap "unassigned" team (i.e. safe to restore into without --force).
	IsEmpty(ctx context.Context) (bool, error)

	// TruncateAll deletes all covered tables (and embeddings) in FK-safe order.
	TruncateAll(ctx context.Context, tablesInInsertOrder []string) error
}

// validTableName guards string-concatenated table names against injection.
// Only covered tables plus the embedding tables are permitted.
func validTableName(t string) bool {
	switch t {
	case "teams", "users", "api_keys", "auth_config", "team_settings",
		"entries", "clusters", "pipeline_runs", "dataset_snapshots",
		"analysis_cache", "rules", "agents", "agent_versions",
		"activity_log", "usage_events", "outcome_ratings", "feed_activity",
		"ft_sessions", "ft_turns", "ft_preferences", "ft_session_knowledge",
		"vec_entries", "entry_embeddings", "embeddings",
		"entry_chunks", "vec_chunks", "chunk_embeddings":
		return true
	}
	return false
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
