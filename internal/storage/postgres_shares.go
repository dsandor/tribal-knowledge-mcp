package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// migrateShares creates the cross-team knowledge sharing table and its index.
func (s *PostgresStore) migrateShares(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS knowledge_shares (
			id                TEXT PRIMARY KEY,
			entry_id          TEXT NOT NULL,
			source_team_id    TEXT NOT NULL DEFAULT '',
			created_by        TEXT NOT NULL DEFAULT '',
			used_at           TIMESTAMPTZ,
			used_by           TEXT NOT NULL DEFAULT '',
			imported_entry_id TEXT NOT NULL DEFAULT '',
			revoked_at        TIMESTAMPTZ,
			created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("create knowledge_shares table: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_shares_entry ON knowledge_shares(entry_id)`); err != nil {
		return fmt.Errorf("create idx_shares_entry index: %w", err)
	}
	return nil
}

// CreateShare mints a single-use, cross-team share token for entryID and
// returns the inserted row.
func (s *PostgresStore) CreateShare(ctx context.Context, entryID, sourceTeamID, createdBy string) (KnowledgeShare, error) {
	id, err := newShareToken()
	if err != nil {
		return KnowledgeShare{}, err
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO knowledge_shares (id, entry_id, source_team_id, created_by)
		VALUES ($1, $2, $3, $4)
	`, id, entryID, sourceTeamID, createdBy); err != nil {
		return KnowledgeShare{}, fmt.Errorf("insert knowledge share: %w", err)
	}
	sh, err := s.GetShare(ctx, id)
	if err != nil {
		return KnowledgeShare{}, fmt.Errorf("read back knowledge share: %w", err)
	}
	return *sh, nil
}

// GetShare returns the share with the given id, or ErrNotFound if absent.
func (s *PostgresStore) GetShare(ctx context.Context, id string) (*KnowledgeShare, error) {
	var (
		sh        KnowledgeShare
		usedAt    sql.NullTime
		revokedAt sql.NullTime
		createdAt time.Time
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, entry_id, source_team_id, created_by,
		       used_at, used_by, imported_entry_id, revoked_at, created_at
		FROM knowledge_shares
		WHERE id = $1
	`, id).Scan(
		&sh.ID, &sh.EntryID, &sh.SourceTeamID, &sh.CreatedBy,
		&usedAt, &sh.UsedBy, &sh.ImportedEntryID, &revokedAt, &createdAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query knowledge share: %w", err)
	}
	if usedAt.Valid {
		t := usedAt.Time
		sh.UsedAt = &t
	}
	if revokedAt.Valid {
		t := revokedAt.Time
		sh.RevokedAt = &t
	}
	sh.CreatedAt = createdAt
	return &sh, nil
}

// MarkShareUsed records an import against the share. The guarded UPDATE only
// matches a share that is neither used nor revoked, so it is safe under races
// and guarantees single-use: a second call (or a call on a revoked share)
// affects zero rows and returns an error.
func (s *PostgresStore) MarkShareUsed(ctx context.Context, id, usedBy, importedEntryID string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE knowledge_shares
		SET used_at = now(), used_by = $1, imported_entry_id = $2
		WHERE id = $3 AND used_at IS NULL AND revoked_at IS NULL
	`, usedBy, importedEntryID, id)
	if err != nil {
		return fmt.Errorf("mark share used: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mark share used rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("share %q is not available (already used or revoked)", id)
	}
	return nil
}

// RevokeShare kills a share token so it can no longer be imported. Revoking a
// missing or already-revoked share is a no-op (no error).
func (s *PostgresStore) RevokeShare(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE knowledge_shares
		SET revoked_at = now()
		WHERE id = $1
	`, id); err != nil {
		return fmt.Errorf("revoke share: %w", err)
	}
	return nil
}
