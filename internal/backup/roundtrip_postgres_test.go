package backup_test

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/dsandor/memory/internal/backup"
	"github.com/dsandor/memory/internal/storage"
)

func TestCrossEngineSQLiteToPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping cross-engine test")
	}
	ctx := context.Background()

	src := newSQLite(t)
	id, err := src.StoreEntry(ctx, storage.KnowledgeEntry{
		Type: "note", Title: "X-Engine", Content: "Body", Tags: []string{"k"},
	}, []float32{0.1, 0.2, 0.3, 0.4})
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if _, err := backup.Export(ctx, src, &buf, "test", 4, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}

	pg, err := storage.NewPostgresStore(dsn, 4)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer pg.Close()
	// Always force so the test is repeatable against a shared DB.
	if _, err := backup.Import(ctx, pg, &buf, backup.ImportOptions{Force: true}, 4); err != nil {
		t.Fatalf("import into postgres: %v", err)
	}

	got, err := pg.GetEntry(ctx, id)
	if err != nil {
		t.Fatalf("get from postgres: %v", err)
	}
	if got.Title != "X-Engine" {
		t.Errorf("title = %q", got.Title)
	}
	res, err := pg.SearchSimilar(ctx, []float32{0.1, 0.2, 0.3, 0.4}, 1)
	if err != nil || len(res) != 1 {
		t.Fatalf("pg vector search failed: res=%d err=%v", len(res), err)
	}
}
