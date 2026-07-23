// Package backup provides engine-neutral logical backup and restore of the
// entire tribal-knowledge database, enabling migration across storage engines
// (SQLite <-> PostgreSQL).
package backup

import "time"

// FormatVersion is the archive format version. Bump on incompatible changes.
const FormatVersion = 1

// coveredTables lists every backed-up table in dependency order (parents first).
// Used forward for insert, reversed for truncate. Embeddings travel via the
// dedicated embeddings path, not as a generic table. sessions are excluded
// (ephemeral); vec_entries is rebuilt from embeddings on restore.
var coveredTables = []string{
	"teams",
	"users",
	"api_keys",
	"auth_config",
	"team_settings",
	"entries",
	"clusters",
	"pipeline_runs",
	"dataset_snapshots",
	"analysis_cache",
	"rules",
	"agents",
	"agent_versions",
	"activity_log",
	"usage_events",
	"outcome_ratings",
	"feed_activity",
	"ft_sessions",
	"ft_turns",
	"ft_preferences",
	"ft_session_knowledge",
}

// CoveredTables returns the ordered list of backed-up tables (a copy).
func CoveredTables() []string {
	out := make([]string, len(coveredTables))
	copy(out, coveredTables)
	return out
}

// Manifest describes an archive's provenance and contents.
type Manifest struct {
	FormatVersion int            `json:"format_version"`
	ToolVersion   string         `json:"tool_version"`
	CreatedAt     time.Time      `json:"created_at"`
	SourceEngine  string         `json:"source_engine"` // "sqlite" | "postgres"
	EmbeddingDim  int            `json:"embedding_dim"`
	Tables        map[string]int `json:"tables"` // table name -> row count
	Embeddings    int            `json:"embeddings"`
}

// Report summarizes a completed restore.
type Report struct {
	TablesRestored     map[string]int
	EmbeddingsRestored int
}

// ImportOptions controls restore behavior.
type ImportOptions struct {
	Force bool // truncate a non-empty target before restoring
}
