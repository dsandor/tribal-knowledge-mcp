package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// CountEntries returns the total number of entries in the store.
func (s *SQLiteStore) CountEntries(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entries").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count entries: %w", err)
	}
	return count, nil
}

// GetAllEmbeddings returns a map from entry ID to embedding vector for all entries.
func (s *SQLiteStore) GetAllEmbeddings(ctx context.Context) (map[string][]float32, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, ee.embedding
		FROM entries e
		JOIN entry_embeddings ee ON ee.rowid = e.rowid
	`)
	if err != nil {
		return nil, fmt.Errorf("query embeddings: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]float32)
	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, fmt.Errorf("scan embedding row: %w", err)
		}
		v, err := deserializeFloat32(blob, s.embeddingDim)
		if err != nil {
			return nil, fmt.Errorf("deserialize embedding for %s: %w", id, err)
		}
		result[id] = v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return result, nil
}

func deserializeFloat32(blob []byte, dim int) ([]float32, error) {
	if len(blob) != dim*4 {
		return nil, fmt.Errorf("blob size %d != expected %d for dim %d", len(blob), dim*4, dim)
	}
	v := make([]float32, dim)
	if err := binary.Read(bytes.NewReader(blob), binary.LittleEndian, v); err != nil {
		return nil, err
	}
	return v, nil
}

// ListClusters returns all clusters in the store.
func (s *SQLiteStore) ListClusters(ctx context.Context) ([]Cluster, error) {
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
		var entryIDsJSON string
		var createdAt, updatedAt string
		if err := rows.Scan(
			&c.ID, &c.Domain, &c.Title, &c.Summary,
			&entryIDsJSON, &c.QualityScore, &c.PipelineRunID,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan cluster: %w", err)
		}
		if err := json.Unmarshal([]byte(entryIDsJSON), &c.EntryIDs); err != nil {
			c.EntryIDs = []string{}
		}
		c.CreatedAt = parseTimestamp(createdAt)
		c.UpdatedAt = parseTimestamp(updatedAt)
		clusters = append(clusters, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return clusters, nil
}

// StoreCluster inserts a new cluster and returns its assigned ID.
func (s *SQLiteStore) StoreCluster(ctx context.Context, c Cluster) (string, error) {
	c.ID = uuid.NewString()
	entryIDsJSON, err := json.Marshal(c.EntryIDs)
	if err != nil {
		return "", fmt.Errorf("marshal entry_ids: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO clusters (id, domain, title, summary, entry_ids, quality_score, pipeline_run_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, c.ID, c.Domain, c.Title, c.Summary, string(entryIDsJSON), c.QualityScore, c.PipelineRunID)
	if err != nil {
		return "", fmt.Errorf("insert cluster: %w", err)
	}
	return c.ID, nil
}

// DeleteClustersByRunID deletes all clusters associated with a given pipeline run ID.
func (s *SQLiteStore) DeleteClustersByRunID(ctx context.Context, runID string) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM clusters WHERE pipeline_run_id = ?", runID); err != nil {
		return fmt.Errorf("delete clusters by run id: %w", err)
	}
	return nil
}

// StartPipelineRun inserts a new pipeline run with status='running' and returns its ID.
func (s *SQLiteStore) StartPipelineRun(ctx context.Context, trigger string) (string, error) {
	id := uuid.NewString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pipeline_runs (id, status, trigger)
		VALUES (?, 'running', ?)
	`, id, trigger)
	if err != nil {
		return "", fmt.Errorf("start pipeline run: %w", err)
	}
	return id, nil
}

// FinishPipelineRun updates the pipeline run with completion details.
func (s *SQLiteStore) FinishPipelineRun(ctx context.Context, id, status string, entriesProcessed, clustersFound int, errs []string) error {
	if errs == nil {
		errs = []string{}
	}
	errJSON, err := json.Marshal(errs)
	if err != nil {
		return fmt.Errorf("marshal errors: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE pipeline_runs
		SET status = ?, entries_processed = ?, clusters_found = ?, errors = ?, completed_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, status, entriesProcessed, clustersFound, string(errJSON), id)
	if err != nil {
		return fmt.Errorf("finish pipeline run: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("pipeline run %q: %w", id, ErrNotFound)
	}
	return nil
}

// GetLatestPipelineRun returns the most recent pipeline run, or nil if none exist.
func (s *SQLiteStore) GetLatestPipelineRun(ctx context.Context) (*PipelineRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, status, trigger, entries_processed, clusters_found, errors, started_at, completed_at
		FROM pipeline_runs
		ORDER BY started_at DESC
		LIMIT 1
	`)
	var r PipelineRun
	var errJSON string
	var startedAt string
	var completedAt sql.NullString
	err := row.Scan(
		&r.ID, &r.Status, &r.Trigger,
		&r.EntriesProcessed, &r.ClustersFound,
		&errJSON, &startedAt, &completedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest pipeline run: %w", err)
	}
	if err := json.Unmarshal([]byte(errJSON), &r.Errors); err != nil {
		r.Errors = []string{}
	}
	r.StartedAt = parseTimestamp(startedAt)
	if completedAt.Valid && completedAt.String != "" {
		t := parseTimestamp(completedAt.String)
		r.CompletedAt = &t
	}
	return &r, nil
}

// ListPipelineRuns returns the most recent pipeline runs ordered by started_at descending.
func (s *SQLiteStore) ListPipelineRuns(ctx context.Context, limit int) ([]PipelineRun, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, status, trigger, entries_processed, clusters_found, errors, started_at, completed_at
		FROM pipeline_runs
		ORDER BY started_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list pipeline runs: %w", err)
	}
	defer rows.Close()
	var result []PipelineRun
	for rows.Next() {
		var r PipelineRun
		var errJSON string
		var startedAt string
		var completedAt sql.NullString
		if err := rows.Scan(&r.ID, &r.Status, &r.Trigger, &r.EntriesProcessed, &r.ClustersFound, &errJSON, &startedAt, &completedAt); err != nil {
			return nil, fmt.Errorf("scan pipeline run: %w", err)
		}
		if err := json.Unmarshal([]byte(errJSON), &r.Errors); err != nil {
			r.Errors = []string{}
		}
		r.StartedAt = parseTimestamp(startedAt)
		if completedAt.Valid && completedAt.String != "" {
			t := parseTimestamp(completedAt.String)
			r.CompletedAt = &t
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// StoreSnapshot inserts a new dataset snapshot and returns its assigned ID.
func (s *SQLiteStore) StoreSnapshot(ctx context.Context, snap DatasetSnapshot) (string, error) {
	snap.ID = uuid.NewString()
	data := snap.Data
	if data == "" {
		data = "{}"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO dataset_snapshots (id, version, cluster_count, entry_count, data, pipeline_run_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, snap.ID, snap.Version, snap.ClusterCount, snap.EntryCount, data, snap.PipelineRunID)
	if err != nil {
		return "", fmt.Errorf("insert snapshot: %w", err)
	}
	return snap.ID, nil
}

// GetLatestSnapshot returns the snapshot with the highest version, or nil if none exist.
func (s *SQLiteStore) GetLatestSnapshot(ctx context.Context) (*DatasetSnapshot, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, version, cluster_count, entry_count, data, pipeline_run_id, created_at
		FROM dataset_snapshots
		ORDER BY version DESC, created_at DESC
		LIMIT 1
	`)
	var snap DatasetSnapshot
	var createdAt string
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
	snap.CreatedAt = parseTimestamp(createdAt)
	return &snap, nil
}

// ListSnapshots returns all dataset snapshots ordered by version descending.
func (s *SQLiteStore) ListSnapshots(ctx context.Context) ([]DatasetSnapshot, error) {
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
		var createdAt string
		if err := rows.Scan(
			&snap.ID, &snap.Version, &snap.ClusterCount, &snap.EntryCount,
			&snap.Data, &snap.PipelineRunID, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan snapshot: %w", err)
		}
		snap.CreatedAt = parseTimestamp(createdAt)
		snaps = append(snaps, snap)
	}
	return snaps, rows.Err()
}

// compile-time check that SQLiteStore implements AnalysisStore.
var _ AnalysisStore = (*SQLiteStore)(nil)
