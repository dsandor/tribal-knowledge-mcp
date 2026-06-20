package backup

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dsandor/memory/internal/storage"
)

func embItem(id string) []storage.EmbeddingItem {
	return []storage.EmbeddingItem{{EntryID: id, Vector: []float32{1, 2, 3, 4}}}
}

func TestImportRoundTripViaFake(t *testing.T) {
	src := newFakeStore()
	src.tables["teams"] = []map[string]any{{"id": "t1", "name": "Team"}}
	src.tables["entries"] = []map[string]any{{"id": "e1", "title": "Hi"}}
	src.embeddings = embItem("e1")

	var buf bytes.Buffer
	if _, err := Export(context.Background(), src, &buf, "v", 4, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}

	dst := newFakeStore()
	rep, err := Import(context.Background(), dst, &buf, ImportOptions{}, 4)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if rep.TablesRestored["entries"] != 1 || rep.EmbeddingsRestored != 1 {
		t.Errorf("report = %+v", rep)
	}
	if len(dst.tables["entries"]) != 1 || len(dst.embeddings) != 1 {
		t.Errorf("dst not populated: %+v", dst.tables)
	}
}

// TestImportFullReplaceIntoBootstrapTarget verifies that a restore succeeds into
// a target that already contains a bootstrap row (e.g. the "unassigned" team
// seeded at server startup) but no entries. The bootstrap rows must be cleared
// by TruncateAll so the archive's rows load cleanly without duplication, and a
// non-force restore must be allowed because the target has no entries.
func TestImportFullReplaceIntoBootstrapTarget(t *testing.T) {
	src := newFakeStore()
	src.tables["teams"] = []map[string]any{{"id": "team-a", "name": "Team A"}}
	src.tables["entries"] = []map[string]any{{"id": "e1", "title": "Hi"}}
	src.embeddings = embItem("e1")

	var buf bytes.Buffer
	if _, err := Export(context.Background(), src, &buf, "v", 4, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}

	// Target has a bootstrap "unassigned" team but no entries -> empty == true.
	dst := newFakeStore()
	dst.tables["teams"] = []map[string]any{{"id": "unassigned"}}
	dst.empty = true

	rep, err := Import(context.Background(), dst, &buf, ImportOptions{}, 4)
	if err != nil {
		t.Fatalf("Import into bootstrap target: %v", err)
	}
	if rep.TablesRestored["teams"] != 1 || rep.TablesRestored["entries"] != 1 {
		t.Errorf("report = %+v", rep)
	}
	// The archive's contents must replace the bootstrap rows, not duplicate them.
	if len(dst.tables["teams"]) != 1 {
		t.Fatalf("expected exactly 1 team after restore, got %d: %+v", len(dst.tables["teams"]), dst.tables["teams"])
	}
	if dst.tables["teams"][0]["id"] != "team-a" {
		t.Errorf("expected archive team-a, got %+v", dst.tables["teams"][0])
	}
	if len(dst.tables["entries"]) != 1 || len(dst.embeddings) != 1 {
		t.Errorf("dst not populated correctly: %+v", dst.tables)
	}
}

func TestImportRefusesNonEmptyWithoutForce(t *testing.T) {
	var buf bytes.Buffer
	Export(context.Background(), newFakeStore(), &buf, "v", 4, time.Unix(0, 0))
	dst := newFakeStore()
	dst.empty = false
	_, err := Import(context.Background(), dst, &buf, ImportOptions{}, 4)
	if err == nil {
		t.Fatal("expected refusal on non-empty target")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error should mention --force, got: %v", err)
	}
}

func TestImportRejectsDimMismatch(t *testing.T) {
	var buf bytes.Buffer
	Export(context.Background(), newFakeStore(), &buf, "v", 4, time.Unix(0, 0))
	if _, err := Import(context.Background(), newFakeStore(), &buf, ImportOptions{}, 8); err == nil {
		t.Fatal("expected embedding_dim mismatch error")
	}
}
