package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
)

// newShareToken returns a URL-safe, unguessable token built from 32 bytes of
// crypto/rand entropy (base64url, no padding → 43 chars). The auth package has
// its own token helper, but it lives in the web layer; importing it here would
// create a cross-package cycle, so storage keeps this small local generator.
func newShareToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate share token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CreateShare mints a single-use, cross-team share token for entryID and
// returns the inserted row.
func (s *SQLiteStore) CreateShare(ctx context.Context, entryID, sourceTeamID, createdBy string) (KnowledgeShare, error) {
	id, err := newShareToken()
	if err != nil {
		return KnowledgeShare{}, err
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO knowledge_shares (id, entry_id, source_team_id, created_by)
		VALUES (?, ?, ?, ?)
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
func (s *SQLiteStore) GetShare(ctx context.Context, id string) (*KnowledgeShare, error) {
	var (
		sh        KnowledgeShare
		usedAt    sql.NullString
		revokedAt sql.NullString
		createdAt string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, entry_id, source_team_id, created_by,
		       used_at, used_by, imported_entry_id, revoked_at, created_at
		FROM knowledge_shares
		WHERE id = ?
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
		t := parseTimestamp(usedAt.String)
		sh.UsedAt = &t
	}
	if revokedAt.Valid {
		t := parseTimestamp(revokedAt.String)
		sh.RevokedAt = &t
	}
	sh.CreatedAt = parseTimestamp(createdAt)
	return &sh, nil
}

// MarkShareUsed records an import against the share. The guarded UPDATE only
// matches a share that is neither used nor revoked, so it is safe under races
// and guarantees single-use: a second call (or a call on a revoked share)
// affects zero rows and returns an error.
func (s *SQLiteStore) MarkShareUsed(ctx context.Context, id, usedBy, importedEntryID string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE knowledge_shares
		SET used_at = CURRENT_TIMESTAMP, used_by = ?, imported_entry_id = ?
		WHERE id = ? AND used_at IS NULL AND revoked_at IS NULL
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
func (s *SQLiteStore) RevokeShare(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE knowledge_shares
		SET revoked_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, id); err != nil {
		return fmt.Errorf("revoke share: %w", err)
	}
	return nil
}
