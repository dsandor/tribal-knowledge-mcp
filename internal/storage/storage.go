package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

type KnowledgeType string

const (
	KTPrompt      KnowledgeType = "prompt"
	KTPattern     KnowledgeType = "pattern"
	KTWorkflow    KnowledgeType = "workflow"
	KTDomainFact  KnowledgeType = "domain_fact"
	KTAntiPattern KnowledgeType = "anti_pattern"
)

// ErrNotFound is returned when a requested entry does not exist.
var ErrNotFound = errors.New("entry not found")

// ErrBadTarget is returned by DeleteTeamMigrate when the target team does not exist.
var ErrBadTarget = errors.New("target team not found")

type KnowledgeEntry struct {
	ID          string
	Type        KnowledgeType
	Title       string
	Content     string
	Description string
	Domain      string
	Tags        []string
	AutoTags    []string // LLM-assigned category tags; never user-edited
	Author      string
	Team        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Version     int
	Rating      float64
	UsageCount  int
	Status      string // "pending" | "approved" | "rejected"
	TeamID      string
}

type SearchResult struct {
	Entry KnowledgeEntry
	Score float64
}

// EntryChunk is one embedded slice of a knowledge entry's content.
// Index 0 is the representative chunk (used for pipeline clustering and the
// legacy per-entry vector). An entry that fits in one chunk has exactly one.
type EntryChunk struct {
	Index         int
	Content       string
	TokenEstimate int
	Embedding     []float32 // len must equal the store's embeddingDim, or nil
}

type ListFilter struct {
	Domain string
	Type   KnowledgeType
	// Limit is the maximum number of entries to return. Zero means use implementation default (typically 50).
	Limit  int
	Offset int    // skip this many entries (pagination)
	Search string // substring match on Title or Content (case-insensitive); empty = no filter
	Status string // filter by entry status; empty = no filter
	TeamID string // filter by team_id; empty = no filter
	Tag    string // exact-match against any user tag or auto tag; empty = no filter
}

type Cluster struct {
	ID            string
	Domain        string
	Title         string
	Summary       string
	EntryIDs      []string
	QualityScore  float64
	PipelineRunID string
	TeamID        string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type PipelineRun struct {
	ID               string
	Status           string
	Trigger          string
	EntriesProcessed int
	ClustersFound    int
	Errors           []string
	TeamID           string
	StartedAt        time.Time
	CompletedAt      *time.Time
}

type DatasetSnapshot struct {
	ID            string
	Version       int
	ClusterCount  int
	EntryCount    int
	Data          string
	PipelineRunID string
	TeamID        string
	CreatedAt     time.Time
}

// UsageEvent records that a knowledge entry was selected/used by a client.
type UsageEvent struct {
	ID            string
	EntryID       string
	UserID        string
	Tool          string // e.g. "prompt_suggest", "enhance_with_context"
	SelectedIndex int    // which suggestion index was chosen (0-based)
	CreatedAt     time.Time
}

// OutcomeRating captures a user's post-use rating of an entry's effectiveness.
type OutcomeRating struct {
	ID        string
	EntryID   string
	UserID    string
	Rating    int // 1–5
	Note      string
	CreatedAt time.Time
}

// TrendingEntry is a knowledge entry with its computed signal score.
type TrendingEntry struct {
	KnowledgeEntry
	SignalScore   float64 `json:"signal_score"`
	UsageCount7d  int     `json:"usage_count_7d"`
	UsageCount30d int     `json:"usage_count_30d"`
	AvgOutcome    float64 `json:"avg_outcome"`
}

// ActivityEvent is a single item in the team activity feed.
type ActivityEvent struct {
	ID        string
	EventType string // "stored", "rated", "approved", "rejected", "pipeline_complete", "agent_generated"
	ActorID   string
	EntryID   string // may be empty for pipeline events
	Metadata  map[string]string
	CreatedAt time.Time
}

// AnalysisStore extends Store with methods needed by the analysis pipeline.
type AnalysisStore interface {
	Store
	CountEntries(ctx context.Context, teamID string) (int, error)
	GetAllEmbeddings(ctx context.Context, teamID string) (map[string][]float32, error)
	ListTeams(ctx context.Context) ([]Team, error)
	ListClusters(ctx context.Context, teamID string) ([]Cluster, error)
	StoreCluster(ctx context.Context, c Cluster) (string, error)
	DeleteClustersByRunID(ctx context.Context, runID string) error
	StartPipelineRun(ctx context.Context, trigger, teamID string) (string, error)
	FinishPipelineRun(ctx context.Context, id, status string, entriesProcessed, clustersFound int, errs []string) error
	GetLatestPipelineRun(ctx context.Context, teamID string) (*PipelineRun, error)
	ListPipelineRuns(ctx context.Context, teamID string, limit int) ([]PipelineRun, error)
	StoreSnapshot(ctx context.Context, snap DatasetSnapshot) (string, error)
	// GetLatestSnapshot returns the snapshot with the highest version for the given team
	// (or globally when teamID is ""), or nil if none exist.
	GetLatestSnapshot(ctx context.Context, teamID string) (*DatasetSnapshot, error)
	// ListSnapshots returns all dataset snapshots ordered by version descending.
	ListSnapshots(ctx context.Context, teamID string) ([]DatasetSnapshot, error)
	// MarkInterruptedRuns marks every pipeline run still in status "running"
	// as failed with an "interrupted by restart" error. Called at startup —
	// only one process runs the pipeline, so any running row at boot is dead.
	// Returns the number of runs marked.
	MarkInterruptedRuns(ctx context.Context) (int, error)
	// GetAnalysisCache returns the cached LLM result for (kind, key).
	GetAnalysisCache(ctx context.Context, kind, key string) (value string, ok bool, err error)
	// PutAnalysisCache upserts the cached LLM result for (kind, key).
	PutAnalysisCache(ctx context.Context, kind, key, value, teamID string) error
	// PruneAnalysisCache deletes cache rows older than olderThan. Returns rows deleted.
	PruneAnalysisCache(ctx context.Context, olderThan time.Duration) (int, error)
}

type Store interface {
	// StoreEntry always creates a new entry, assigning a fresh UUID as ID.
	// The ID field on the passed entry is ignored. Returns the assigned ID.
	StoreEntry(ctx context.Context, entry KnowledgeEntry, embedding []float32) (string, error)
	// StoreEntryChunked creates a new entry whose content is represented by one
	// or more embedding vectors (chunks). chunks[0] is the representative chunk.
	// Assigns a fresh UUID; entry.ID is ignored. Returns the new ID.
	StoreEntryChunked(ctx context.Context, entry KnowledgeEntry, chunks []EntryChunk) (string, error)
	// ReplaceEntryChunks atomically replaces all chunks (and vectors) for an
	// existing entry. Used when content is edited. Returns ErrNotFound if absent.
	ReplaceEntryChunks(ctx context.Context, entryID string, chunks []EntryChunk) error
	GetEntry(ctx context.Context, id string) (*KnowledgeEntry, error)
	ListEntries(ctx context.Context, filter ListFilter) ([]KnowledgeEntry, error)
	DeleteEntry(ctx context.Context, id string) error
	SearchSimilar(ctx context.Context, embedding []float32, topK int) ([]SearchResult, error)
	// RateEntry updates the rating for an existing entry.
	// Returns ErrNotFound if the entry does not exist.
	RateEntry(ctx context.Context, id string, rating float64) error
	// ApproveEntry sets an entry's status to "approved".
	ApproveEntry(ctx context.Context, id string) error
	// RejectEntry sets an entry's status to "rejected".
	RejectEntry(ctx context.Context, id string) error
	// UpdateEntry updates the mutable fields of an existing entry (title, content, description, domain, tags).
	UpdateEntry(ctx context.Context, entry KnowledgeEntry) error
	// UpdateAutoTags replaces the auto-generated tags for an entry without
	// touching user tags or bumping version. Returns ErrNotFound if missing.
	UpdateAutoTags(ctx context.Context, id string, tags []string) error
	// BackfillTeamID stamps teamID onto rows whose team_id is empty across
	// entries, clusters, agents, agent_versions, dataset_snapshots, and
	// pipeline_runs. Idempotent; used by single-team deployments at startup.
	BackfillTeamID(ctx context.Context, teamID string) error
	// Ping verifies the storage connection is alive. Returns nil on success.
	Ping(ctx context.Context) error
	Close() error

	// Usage tracking
	RecordUsage(ctx context.Context, event UsageEvent) error
	RecordOutcome(ctx context.Context, rating OutcomeRating) error

	// Signal-based queries
	GetTrendingEntries(ctx context.Context, teamID string, days int, limit int) ([]TrendingEntry, error)
	GetWeakSignalEntries(ctx context.Context, teamID string, minRatings int, maxAvgOutcome float64) ([]KnowledgeEntry, error)

	// Activity feed
	RecordActivity(ctx context.Context, event ActivityEvent) error
	ListActivity(ctx context.Context, teamID string, limit int, offset int) ([]ActivityEvent, error)

	// SearchHybrid combines full-text and vector similarity search.
	// mode: "hybrid" | "semantic" | "keyword"
	// embedding may be nil when mode == "keyword"
	SearchHybrid(ctx context.Context, teamID string, query string, embedding []float32, mode string, limit int) ([]KnowledgeEntry, error)

	// BulkImport inserts multiple entries in a single transaction.
	// Entries whose title already exists (case-insensitive) within the same team are skipped.
	// Returns imported count, skipped count, per-entry error strings, and a top-level error.
	BulkImport(ctx context.Context, entries []KnowledgeEntry) (imported int, skipped int, errs []string, err error)

	// GetEntryByContentHash returns the first entry whose content_hash matches SHA256(title+content).
	// Returns nil, nil if no match.
	GetEntryByContentHash(ctx context.Context, hash string) (*KnowledgeEntry, error)
}

// sha256Hex returns the lowercase hex-encoded SHA-256 digest of s.
// Both SQLiteStore and PostgresStore use this; the mcp package has a local
// mirror (contentHash) to avoid a cross-package import cycle.
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
