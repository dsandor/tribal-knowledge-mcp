package embedding_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dsandor/memory/internal/embedding"
)

func TestOllamaEmbedder_Embed(t *testing.T) {
	wantEmbedding := []float32{0.1, 0.2, 0.3}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", 404)
			return
		}
		var body struct {
			Model  string `json:"model"`
			Prompt string `json:"prompt"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		if body.Model != "nomic-embed-text" {
			t.Errorf("model: got %q, want nomic-embed-text", body.Model)
		}
		if body.Prompt != "hello world" {
			t.Errorf("prompt: got %q, want hello world", body.Prompt)
		}
		json.NewEncoder(w).Encode(map[string]any{"embedding": wantEmbedding})
	}))
	defer srv.Close()

	e := embedding.NewOllamaEmbedder(srv.URL, "nomic-embed-text")
	got, err := e.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != len(wantEmbedding) {
		t.Fatalf("len: got %d, want %d", len(got), len(wantEmbedding))
	}
	for i := range wantEmbedding {
		if got[i] != wantEmbedding[i] {
			t.Errorf("[%d]: got %f, want %f", i, got[i], wantEmbedding[i])
		}
	}
}

func TestOllamaEmbedder_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", 500)
	}))
	defer srv.Close()

	e := embedding.NewOllamaEmbedder(srv.URL, "nomic-embed-text")
	_, err := e.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error on 500 response, got nil")
	}
}

func TestOllamaEmbedder_EmptyEmbedding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"embedding": []float32{}})
	}))
	defer srv.Close()

	e := embedding.NewOllamaEmbedder(srv.URL, "nomic-embed-text")
	_, err := e.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error on empty embedding, got nil")
	}
}
