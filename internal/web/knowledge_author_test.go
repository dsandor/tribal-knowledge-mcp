package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dsandor/memory/internal/storage"
)

// TestKnowledgeUpdate_SetAuthorOnlyWhenEmpty verifies that PUT
// /api/knowledge/{id} can set an entry's Author only when it is currently
// empty. An incoming author for an entry that already has one is silently
// ignored, protecting real authorship.
func TestKnowledgeUpdate_SetAuthorOnlyWhenEmpty(t *testing.T) {
	ctx := context.Background()
	store, _ := newReembedStore(t)
	srv := newReembedServer(t, *store)

	put := func(t *testing.T, id, author string) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"author": author})
		req := httptest.NewRequest("PUT", "/api/knowledge/"+id, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		return w
	}

	t.Run("sets author when empty", func(t *testing.T) {
		id, err := store.StoreEntry(ctx, storage.KnowledgeEntry{
			Type:   storage.KTPrompt,
			Title:  "No author yet",
			Status: "approved",
			TeamID: "test-team",
			Author: "",
		}, nil)
		if err != nil {
			t.Fatalf("StoreEntry: %v", err)
		}

		w := put(t, id, "alice")
		if w.Code != http.StatusOK {
			t.Fatalf("update: want 200, got %d: %s", w.Code, w.Body.String())
		}

		got, err := store.GetEntry(ctx, id)
		if err != nil {
			t.Fatalf("GetEntry: %v", err)
		}
		if got.Author != "alice" {
			t.Fatalf("author = %q, want %q", got.Author, "alice")
		}
	})

	t.Run("ignores author when already set", func(t *testing.T) {
		id, err := store.StoreEntry(ctx, storage.KnowledgeEntry{
			Type:   storage.KTPrompt,
			Title:  "Has author",
			Status: "approved",
			TeamID: "test-team",
			Author: "bob",
		}, nil)
		if err != nil {
			t.Fatalf("StoreEntry: %v", err)
		}

		w := put(t, id, "alice")
		if w.Code != http.StatusOK {
			t.Fatalf("update: want 200, got %d: %s", w.Code, w.Body.String())
		}

		got, err := store.GetEntry(ctx, id)
		if err != nil {
			t.Fatalf("GetEntry: %v", err)
		}
		if got.Author != "bob" {
			t.Fatalf("author = %q, want %q (must not overwrite)", got.Author, "bob")
		}
	})
}
