package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/dsandor/memory/internal/storage"
)

func TestExportWritesManifestAndTables(t *testing.T) {
	fake := newFakeStore()
	fake.tables["entries"] = []map[string]any{{"id": "e1", "title": "Hello"}}
	fake.tables["teams"] = []map[string]any{{"id": "t1", "name": "Team"}}
	fake.embeddings = []storage.EmbeddingItem{{EntryID: "e1", Vector: []float32{1, 2, 3, 4}}}

	var buf bytes.Buffer
	man, err := Export(context.Background(), fake, &buf, "test-1.0", 4, time.Unix(0, 0))
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if man.EmbeddingDim != 4 || man.SourceEngine != "fake" {
		t.Errorf("manifest = %+v", man)
	}
	if man.Tables["entries"] != 1 || man.Embeddings != 1 {
		t.Errorf("counts wrong: %+v", man)
	}
	if !man.CreatedAt.Equal(time.Unix(0, 0)) {
		t.Errorf("CreatedAt = %v, want %v", man.CreatedAt, time.Unix(0, 0))
	}

	files := readTarGz(t, &buf)
	if _, ok := files["manifest.json"]; !ok {
		t.Fatal("manifest.json missing")
	}
	if _, ok := files["tables/entries.jsonl"]; !ok {
		t.Fatal("tables/entries.jsonl missing")
	}
	if !strings.Contains(files["tables/entry_embeddings.jsonl"], "e1") {
		t.Fatal("embeddings jsonl missing entry")
	}
	var gotMan Manifest
	if err := json.Unmarshal([]byte(files["manifest.json"]), &gotMan); err != nil {
		t.Fatalf("manifest parse: %v", err)
	}
	if gotMan.Tables["entries"] != 1 {
		t.Errorf("gotMan.Tables[entries] = %d, want 1", gotMan.Tables["entries"])
	}
	if gotMan.Embeddings != 1 {
		t.Errorf("gotMan.Embeddings = %d, want 1", gotMan.Embeddings)
	}
}

func readTarGz(t *testing.T, r io.Reader) map[string]string {
	t.Helper()
	gz, err := gzip.NewReader(r)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	out := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		out[h.Name] = string(b)
	}
	return out
}
