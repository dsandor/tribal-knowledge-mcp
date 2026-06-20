package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/dsandor/memory/internal/storage"
)

// Export streams a .tar.gz logical backup of store to w and returns the manifest.
// createdAt is supplied by the caller (CLI/web) since this package avoids time.Now.
func Export(ctx context.Context, store storage.BackupStore, w io.Writer, toolVersion string, embeddingDim int, createdAt time.Time) (Manifest, error) {
	man := Manifest{
		FormatVersion: FormatVersion,
		ToolVersion:   toolVersion,
		CreatedAt:     createdAt,
		SourceEngine:  store.EngineName(),
		EmbeddingDim:  embeddingDim,
		Tables:        map[string]int{},
	}

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	// Per-table JSONL files. We must know the byte length before writing a tar
	// header, so each table is buffered to memory. Tables are small relative to
	// embeddings; embeddings are also buffered here for the same reason.
	// (Acceptable: full DB easily fits in memory for v1.)
	writeFile := func(name string, body []byte) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body))}); err != nil {
			return err
		}
		_, err := tw.Write(body)
		return err
	}

	for _, table := range CoveredTables() {
		var buf bufferWriter
		count := 0
		enc := json.NewEncoder(&buf)
		if err := store.DumpTable(ctx, table, func(row map[string]any) error {
			count++
			return enc.Encode(row)
		}); err != nil {
			return man, fmt.Errorf("dump %s: %w", table, err)
		}
		man.Tables[table] = count
		if err := writeFile("tables/"+table+".jsonl", buf.Bytes()); err != nil {
			return man, err
		}
	}

	// Embeddings.
	var ebuf bufferWriter
	eenc := json.NewEncoder(&ebuf)
	ecount := 0
	if err := store.DumpEmbeddings(ctx, func(it storage.EmbeddingItem) error {
		ecount++
		return eenc.Encode(it)
	}); err != nil {
		return man, fmt.Errorf("dump embeddings: %w", err)
	}
	man.Embeddings = ecount
	if err := writeFile("tables/entry_embeddings.jsonl", ebuf.Bytes()); err != nil {
		return man, err
	}

	// Manifest last (so counts are final). Tar order does not matter for reads.
	mb, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return man, err
	}
	if err := writeFile("manifest.json", mb); err != nil {
		return man, err
	}

	if err := tw.Close(); err != nil {
		return man, err
	}
	if err := gz.Close(); err != nil {
		return man, err
	}
	return man, nil
}

// bufferWriter is a minimal bytes buffer wrapper.
type bufferWriter struct{ b []byte }

func (w *bufferWriter) Write(p []byte) (int, error) { w.b = append(w.b, p...); return len(p), nil }
func (w *bufferWriter) Bytes() []byte               { return w.b }
