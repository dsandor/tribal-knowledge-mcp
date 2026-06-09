package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	pgvector "github.com/pgvector/pgvector-go"
)

// migrateAnalysis creates the analysis-related tables (clusters, pipeline_runs, dataset_snapshots).
func (s *PostgresStore) migrateAnalysis(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS clusters (
			id              TEXT PRIMARY KEY,
			domain          TEXT NOT NULL DEFAULT '',
			title           TEXT NOT NULL DEFAULT '',
			summary         TEXT NOT NULL DEFAULT '',
			entry_ids       JSONB NOT NULL DEFAULT '[]',
			quality_score   FLOAT NOT NULL DEFAULT 0,
			pipeline_run_id TEXT NOT NULL DEFAULT '',
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("create clusters table: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS pipeline_runs (
			id                TEXT PRIMARY KEY,
			status            TEXT NOT NULL DEFAULT 'running',
			trigger           TEXT NOT NULL DEFAULT '',
			entries_processed INT NOT NULL DEFAULT 0,
			clusters_found    INT NOT NULL DEFAULT 0,
			errors            JSONB NOT NULL DEFAULT '[]',
			started_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			completed_at      TIMESTAMPTZ NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("create pipeline_runs table: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS dataset_snapshots (
			id              TEXT PRIMARY KEY,
			version         INT NOT NULL,
			cluster_count   INT NOT NULL DEFAULT 0,
			entry_count     INT NOT NULL DEFAULT 0,
			data            TEXT NOT NULL DEFAULT '{}',
			pipeline_run_id TEXT NOT NULL DEFAULT '',
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("create dataset_snapshots table: %w", err)
	}

	return nil
}

// CountEntries returns the total number of knowledge entries.
func (s *PostgresStore) CountEntries(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count entries: %w", err)
	}
	return count, nil
}

// GetAllEmbeddings returns a map from entry_id to embedding vector for all stored embeddings.
func (s *PostgresStore) GetAllEmbeddings(ctx context.Context) (map[string][]float32, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT entry_id, embedding FROM embeddings`)
	if err != nil {
		return nil, fmt.Errorf("query embeddings: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]float32)
	for rows.Next() {
		var entryID string
		var vec pgvector.Vector
		if err := rows.Scan(&entryID, &vec); err != nil {
			return nil, fmt.Errorf("scan embedding row: %w", err)
		}
		result[entryID] = vec.Slice()
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return result, nil
}

// ListClusters returns all clusters ordered by created_at descending.
func (s *PostgresStore) ListClusters(ctx context.Context) ([]Cluster, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, domain, title, summary, entry_ids, quality_score, pipeline_run_id, created_at, updated_at
		FROM clusters
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	defer rows.Close()

	var clusters []Cluster
	for rows.Next() {
		var c Cluster
		var entryIDsRaw []byte
		var createdAt, updatedAt time.Time
		if err := rows.Scan(
			&c.ID, &c.Domain, &c.Title, &c.Summary,
			&entryIDsRaw, &c.QualityScore, &c.PipelineRunID,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan cluster: %w", err)
		}
		if err := json.Unmarshal(entryIDsRaw, &c.EntryIDs); err != nil {
			c.EntryIDs = []string{}
		}
		c.CreatedAt = createdAt
		c.UpdatedAt = updatedAt
		clusters = append(clusters, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return clusters, nil
}

// StoreCluster inserts a new cluster and returns its assigned ID.
func (s *PostgresStore) StoreCluster(ctx context.Context, c Cluster) (string, error) {
	c.ID = uuid.NewString()
	entryIDsJSON, err := json.Marshal(c.EntryIDs)
	if err != nil {
		return "", fmt.Errorf("marshal entry_ids: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO clusters (id, domain, title, summary, entry_ids, quality_score, pipeline_run_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, c.ID, c.Domain, c.Title, c.Summary, string(entryIDsJSON), c.QualityScore, c.PipelineRunID)
	if err != nil {
		return "", fmt.Errorf("insert cluster: %w", err)
	}
	return c.ID, nil
}

// DeleteClustersByRunID deletes all clusters associated with the given pipeline run ID.
func (s *PostgresStore) DeleteClustersByRunID(ctx context.Context, runID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM clusters WHERE pipeline_run_id = $1`, runID); err != nil {
		return fmt.Errorf("delete clusters by run id: %w", err)
	}
	return nil
}

// StartPipelineRun inserts a new pipeline run with status='running' and returns its ID.
func (s *PostgresStore) StartPipelineRun(ctx context.Context, trigger string) (string, error) {
	id := uuid.NewString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pipeline_runs (id, status, trigger)
		VALUES ($1, 'running', $2)
	`, id, trigger)
	if err != nil {
		return "", fmt.Errorf("start pipeline run: %w", err)
	}
	return id, nil
}

// FinishPipelineRun updates the pipeline run record with completion details.
func (s *PostgresStore) FinishPipelineRun(ctx context.Context, id, status string, entriesProcessed, clustersFound int, errs []string) error {
	if errs == nil {
		errs = []string{}
	}
	errJSON, err := json.Marshal(errs)
	if err != nil {
		return fmt.Errorf("marshal errors: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE pipeline_runs
		SET status = $1, entries_processed = $2, clusters_found = $3, errors = $4, completed_at = NOW()
		WHERE id = $5
	`, status, entriesProcessed, clustersFound, string(errJSON), id)
	if err != nil {
		return fmt.Errorf("finish pipeline run: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("pipeline run %q: %w", id, ErrNotFound)
	}
	return nil
}

// GetLatestPipelineRun returns the most recently started pipeline run, or nil if none exist.
func (s *PostgresStore) GetLatestPipelineRun(ctx context.Context) (*PipelineRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, status, trigger, entries_processed, clusters_found, errors, started_at, completed_at
		FROM pipeline_runs
		ORDER BY started_at DESC
		LIMIT 1
	`)
	var r PipelineRun
	var errsRaw []byte
	var startedAt time.Time
	var completedAt sql.NullTime
	err := row.Scan(
		&r.ID, &r.Status, &r.Trigger,
		&r.EntriesProcessed, &r.ClustersFound,
		&errsRaw, &startedAt, &completedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest pipeline run: %w", err)
	}
	if err := json.Unmarshal(errsRaw, &r.Errors); err != nil {
		r.Errors = []string{}
	}
	r.StartedAt = startedAt
	if completedAt.Valid {
		t := completedAt.Time
		r.CompletedAt = &t
	}
	return &r, nil
}

// ListPipelineRuns returns the most recent pipeline runs ordered by started_at descending.
func (s *PostgresStore) ListPipelineRuns(ctx context.Context, limit int) ([]PipelineRun, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, status, trigger, entries_processed, clusters_found, errors, started_at, completed_at
		FROM pipeline_runs
		ORDER BY started_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list pipeline runs: %w", err)
	}
	defer rows.Close()
	var result []PipelineRun
	for rows.Next() {
		var r PipelineRun
		var errsRaw []byte
		var startedAt time.Time
		var completedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.Status, &r.Trigger, &r.EntriesProcessed, &r.ClustersFound, &errsRaw, &startedAt, &completedAt); err != nil {
			return nil, fmt.Errorf("scan pipeline run: %w", err)
		}
		if err := json.Unmarshal(errsRaw, &r.Errors); err != nil {
			r.Errors = []string{}
		}
		r.StartedAt = startedAt
		if completedAt.Valid {
			t := completedAt.Time
			r.CompletedAt = &t
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// StoreSnapshot inserts a new dataset snapshot and returns its assigned ID.
func (s *PostgresStore) StoreSnapshot(ctx context.Context, snap DatasetSnapshot) (string, error) {
	snap.ID = uuid.NewString()
	data := snap.Data
	if data == "" {
		data = "{}"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO dataset_snapshots (id, version, cluster_count, entry_count, data, pipeline_run_id)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, snap.ID, snap.Version, snap.ClusterCount, snap.EntryCount, data, snap.PipelineRunID)
	if err != nil {
		return "", fmt.Errorf("insert snapshot: %w", err)
	}
	return snap.ID, nil
}

// GetLatestSnapshot returns the snapshot with the highest version, or nil if none exist.
func (s *PostgresStore) GetLatestSnapshot(ctx context.Context) (*DatasetSnapshot, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, version, cluster_count, entry_count, data, pipeline_run_id, created_at
		FROM dataset_snapshots
		ORDER BY version DESC, created_at DESC
		LIMIT 1
	`)
	var snap DatasetSnapshot
	var createdAt time.Time
	err := row.Scan(
		&snap.ID, &snap.Version, &snap.ClusterCount, &snap.EntryCount,
		&snap.Data, &snap.PipelineRunID, &createdAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest snapshot: %w", err)
	}
	snap.CreatedAt = createdAt
	return &snap, nil
}

// ListSnapshots returns all dataset snapshots ordered by version descending.
func (s *PostgresStore) ListSnapshots(ctx context.Context) ([]DatasetSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, version, cluster_count, entry_count, data, pipeline_run_id, created_at
		FROM dataset_snapshots
		ORDER BY version DESC, created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	defer rows.Close()

	var snaps []DatasetSnapshot
	for rows.Next() {
		var snap DatasetSnapshot
		var createdAt time.Time
		if err := rows.Scan(
			&snap.ID, &snap.Version, &snap.ClusterCount, &snap.EntryCount,
			&snap.Data, &snap.PipelineRunID, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan snapshot: %w", err)
		}
		snap.CreatedAt = createdAt
		snaps = append(snaps, snap)
	}
	return snaps, rows.Err()
}
