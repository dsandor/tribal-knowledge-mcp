package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// BulkImport inserts many entries in a single transaction for PostgreSQL,
// skipping any whose title (case-insensitive) already exists in the same team.
func (s *PostgresStore) BulkImport(ctx context.Context, entries []KnowledgeEntry) (imported int, skipped int, errs []string, err error) {
	if len(entries) == 0 {
		return 0, 0, nil, nil
	}

	tx, txErr := s.db.BeginTx(ctx, nil)
	if txErr != nil {
		return 0, 0, nil, fmt.Errorf("bulk import begin tx: %w", txErr)
	}
	defer tx.Rollback() //nolint:errcheck

	for i, entry := range entries {
		titleTrimmed := strings.TrimSpace(entry.Title)
		if titleTrimmed == "" {
			errs = append(errs, fmt.Sprintf("entry[%d]: title is required", i))
			continue
		}

		// Check for an existing entry with the same lower-cased title in the same team.
		var count int
		checkErr := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM entries WHERE lower(trim(title)) = lower(trim($1)) AND team_id = $2`,
			titleTrimmed, entry.TeamID,
		).Scan(&count)
		if checkErr != nil {
			errs = append(errs, fmt.Sprintf("entry[%d] %q: check exists: %v", i, titleTrimmed, checkErr))
			continue
		}
		if count > 0 {
			skipped++
			continue
		}

		entry.ID = uuid.NewString()
		if entry.Status == "" {
			entry.Status = "approved"
		}
		tagsJSON, jsonErr := json.Marshal(entry.Tags)
		if jsonErr != nil {
			errs = append(errs, fmt.Sprintf("entry[%d] %q: marshal tags: %v", i, titleTrimmed, jsonErr))
			continue
		}
		autoTagsJSON, jsonErr := json.Marshal(entry.AutoTags)
		if jsonErr != nil {
			errs = append(errs, fmt.Sprintf("entry[%d] %q: marshal auto tags: %v", i, titleTrimmed, jsonErr))
			continue
		}

		_, insErr := tx.ExecContext(ctx, `
			INSERT INTO entries (id, type, title, content, description, domain, tags, auto_tags, author, team, team_id, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT DO NOTHING
		`, entry.ID, string(entry.Type), titleTrimmed, entry.Content,
			entry.Description, entry.Domain, string(tagsJSON), string(autoTagsJSON),
			entry.Author, entry.Team, entry.TeamID, entry.Status,
		)
		if insErr != nil {
			errs = append(errs, fmt.Sprintf("entry[%d] %q: insert: %v", i, titleTrimmed, insErr))
			continue
		}
		imported++
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return 0, 0, errs, fmt.Errorf("bulk import commit: %w", commitErr)
	}
	return imported, skipped, errs, nil
}
