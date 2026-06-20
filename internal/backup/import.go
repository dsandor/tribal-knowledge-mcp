package backup

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/dsandor/memory/internal/storage"
)

// Import restores an archive (read from r) into store. targetDim is the target
// store's configured embedding dimension; the archive's embedding_dim must match.
//
// Restore semantics are "full replace": the target is always truncated of all
// covered tables before loading. This clears any bootstrap rows the server seeds
// at startup (e.g. the "unassigned" team) that would otherwise collide with the
// archive's rows. A non-empty target (one that already holds entries) is refused
// unless ImportOptions.Force is set.
func Import(ctx context.Context, store storage.BackupStore, r io.Reader, opts ImportOptions, targetDim int) (Report, error) {
	rep := Report{TablesRestored: map[string]int{}}

	// Load the whole archive into memory (acceptable for v1 DB sizes).
	gz, err := gzip.NewReader(r)
	if err != nil {
		return rep, fmt.Errorf("gzip: %w", err)
	}
	tr := tar.NewReader(gz)
	files := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return rep, fmt.Errorf("tar: %w", err)
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			return rep, err
		}
		files[h.Name] = b
	}

	// Validate manifest.
	mb, ok := files["manifest.json"]
	if !ok {
		return rep, fmt.Errorf("archive missing manifest.json")
	}
	var man Manifest
	if err := json.Unmarshal(mb, &man); err != nil {
		return rep, fmt.Errorf("parse manifest: %w", err)
	}
	if man.FormatVersion != FormatVersion {
		return rep, fmt.Errorf("unsupported archive format_version %d (this build supports %d)", man.FormatVersion, FormatVersion)
	}
	if man.EmbeddingDim != targetDim {
		return rep, fmt.Errorf("embedding_dim mismatch: archive=%d target=%d; cannot restore", man.EmbeddingDim, targetDim)
	}

	// Emptiness / force check. IsEmpty == "no entries".
	empty, err := store.IsEmpty(ctx)
	if err != nil {
		return rep, err
	}
	if !empty && !opts.Force {
		return rep, fmt.Errorf("target database is not empty; re-run with --force to overwrite")
	}

	// Full replace: always truncate before loading so bootstrap rows (seeded at
	// server startup) are cleared and cannot collide with the archive's rows.
	// Truncating a target that has no entries is safe and idempotent.
	if err := store.TruncateAll(ctx, CoveredTables()); err != nil {
		return rep, fmt.Errorf("truncate target: %w", err)
	}

	// Restore tables in dependency order.
	for _, table := range CoveredTables() {
		body, ok := files["tables/"+table+".jsonl"]
		if !ok {
			continue
		}
		rows, err := decodeRows(body)
		if err != nil {
			return rep, fmt.Errorf("decode %s: %w", table, err)
		}
		if err := store.LoadTable(ctx, table, rows); err != nil {
			return rep, fmt.Errorf("load %s: %w", table, err)
		}
		rep.TablesRestored[table] = len(rows)
	}

	// Restore embeddings.
	if body, ok := files["tables/entry_embeddings.jsonl"]; ok {
		var items []storage.EmbeddingItem
		sc := bufio.NewScanner(bytes.NewReader(body))
		sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
		for sc.Scan() {
			line := sc.Bytes()
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var it storage.EmbeddingItem
			if err := json.Unmarshal(line, &it); err != nil {
				return rep, fmt.Errorf("decode embedding: %w", err)
			}
			items = append(items, it)
		}
		if err := sc.Err(); err != nil {
			return rep, err
		}
		if err := store.LoadEmbeddings(ctx, items); err != nil {
			return rep, fmt.Errorf("load embeddings: %w", err)
		}
		rep.EmbeddingsRestored = len(items)
	}

	return rep, nil
}

func decodeRows(body []byte) ([]map[string]any, error) {
	var rows []map[string]any
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, sc.Err()
}
