package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/web"
)

// newReembedAllServer builds a server wired with the stub AI sources (so the
// configured embedder resolves to the deterministic stubEmbedder) and the given
// mock store as both the storage backend and the embedding-config source.
func newReembedAllServer(t *testing.T, store *mockStore) *web.Server {
	t.Helper()
	staticFS := fstest.MapFS{
		"index.html": {Data: []byte("<html><body>app</body></html>"), Mode: 0444, ModTime: time.Now()},
	}
	return web.NewServer(staticFS, store).
		WithAISources(newReembedSources(store))
}

func TestReembedAll_NonSuperadminForbidden(t *testing.T) {
	// Default role is "admin" — not superadmin — so the route group rejects it.
	store := &mockStore{apiKeyRole: "admin"}
	srv := newReembedAllServer(t, store)

	req := authRequest("POST", "/api/admin/reembed", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReembedAll_SuperadminReembedsEntries(t *testing.T) {
	store := &mockStore{
		apiKeyRole: "superadmin",
		// Provider openai with a model that has a known dimension (1536).
		embeddingCfg: &storage.EmbeddingConfig{
			Provider:      "openai",
			Model:         "text-embedding-3-small",
			OpenAIBaseURL: "https://api.openai.com",
			Dimension:     1536,
		},
		entries: []storage.KnowledgeEntry{
			{ID: "e1", Title: "One", Content: "alpha content about apples", TeamID: "team-a"},
			{ID: "e2", Title: "Two", Content: "bravo content about zebras", TeamID: "team-b"},
		},
	}
	srv := newReembedAllServer(t, store)

	req := authRequest("POST", "/api/admin/reembed", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Reembedded int `json:"reembedded"`
		Skipped    int `json:"skipped"`
		Dimension  int `json:"dimension"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Reembedded != 2 {
		t.Fatalf("want reembedded=2, got %d (skipped=%d)", resp.Reembedded, resp.Skipped)
	}
	if resp.Skipped != 0 {
		t.Fatalf("want skipped=0, got %d", resp.Skipped)
	}
	if resp.Dimension != 1536 {
		t.Fatalf("want dimension=1536 (from ModelDimension), got %d", resp.Dimension)
	}
}

// embeddingConfigResp mirrors the masked GET/PUT response shape.
type embeddingConfigResp struct {
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	OpenAIAPIKey     string `json:"openai_api_key"`
	OpenAIBaseURL    string `json:"openai_base_url"`
	OllamaURL        string `json:"ollama_url"`
	CurrentDimension int    `json:"current_dimension"`
	ModelDimension   int    `json:"model_dimension"`
}

func TestGetEmbeddingConfig_NonSuperadminForbidden(t *testing.T) {
	store := &mockStore{apiKeyRole: "admin"}
	srv := newReembedAllServer(t, store)

	req := authRequest("GET", "/api/admin/embedding-config", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetEmbeddingConfig_MasksStoredKey(t *testing.T) {
	store := &mockStore{
		apiKeyRole: "superadmin",
		embeddingCfg: &storage.EmbeddingConfig{
			Provider:      "openai",
			Model:         "text-embedding-3-small",
			OpenAIAPIKey:  "sk-secret-value",
			OpenAIBaseURL: "https://api.openai.com",
			Dimension:     1536,
		},
	}
	srv := newReembedAllServer(t, store)

	req := authRequest("GET", "/api/admin/embedding-config", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp embeddingConfigResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.OpenAIAPIKey != "stored" {
		t.Fatalf("want openai_api_key masked to %q, got %q", "stored", resp.OpenAIAPIKey)
	}
	if resp.Provider != "openai" || resp.Model != "text-embedding-3-small" {
		t.Fatalf("unexpected provider/model: %+v", resp)
	}
	if resp.CurrentDimension != 1536 {
		t.Fatalf("want current_dimension=1536, got %d", resp.CurrentDimension)
	}
	if resp.ModelDimension != 1536 {
		t.Fatalf("want model_dimension=1536, got %d", resp.ModelDimension)
	}
}

func TestGetEmbeddingConfig_EmptyKeyMasksToEmpty(t *testing.T) {
	store := &mockStore{
		apiKeyRole: "superadmin",
		embeddingCfg: &storage.EmbeddingConfig{
			Provider:      "ollama",
			Model:         "nomic-embed-text",
			OpenAIBaseURL: "https://api.openai.com",
			OllamaURL:     "http://localhost:11434",
			Dimension:     768,
		},
	}
	srv := newReembedAllServer(t, store)

	req := authRequest("GET", "/api/admin/embedding-config", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp embeddingConfigResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.OpenAIAPIKey != "" {
		t.Fatalf("want empty openai_api_key, got %q", resp.OpenAIAPIKey)
	}
	if resp.ModelDimension != 0 {
		t.Fatalf("want model_dimension=0 (unknown), got %d", resp.ModelDimension)
	}
	if resp.CurrentDimension != 768 {
		t.Fatalf("want current_dimension=768, got %d", resp.CurrentDimension)
	}
}

func TestPutEmbeddingConfig_NonSuperadminForbidden(t *testing.T) {
	store := &mockStore{apiKeyRole: "admin"}
	srv := newReembedAllServer(t, store)

	req := authRequest("PUT", "/api/admin/embedding-config",
		`{"provider":"openai","model":"text-embedding-3-large","openai_base_url":"https://api.openai.com","ollama_url":""}`)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPutEmbeddingConfig_PreservesStoredKeyOnSentinel(t *testing.T) {
	store := &mockStore{
		apiKeyRole: "superadmin",
		embeddingCfg: &storage.EmbeddingConfig{
			Provider:      "openai",
			Model:         "text-embedding-3-small",
			OpenAIAPIKey:  "sk-original",
			OpenAIBaseURL: "https://api.openai.com",
			Dimension:     1536,
		},
	}
	srv := newReembedAllServer(t, store)

	// PUT echoing the masked "stored" sentinel must not clobber the stored key,
	// and must preserve the existing Dimension.
	req := authRequest("PUT", "/api/admin/embedding-config",
		`{"provider":"openai","model":"text-embedding-3-large","openai_api_key":"stored","openai_base_url":"https://api.openai.com","ollama_url":""}`)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if store.embeddingCfg.OpenAIAPIKey != "sk-original" {
		t.Fatalf("want stored key preserved (sk-original), got %q", store.embeddingCfg.OpenAIAPIKey)
	}
	if store.embeddingCfg.Model != "text-embedding-3-large" {
		t.Fatalf("want model updated, got %q", store.embeddingCfg.Model)
	}
	if store.embeddingCfg.Dimension != 1536 {
		t.Fatalf("want Dimension preserved at 1536, got %d", store.embeddingCfg.Dimension)
	}

	// And the masked response should report the key as stored.
	var resp embeddingConfigResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.OpenAIAPIKey != "stored" {
		t.Fatalf("want response key masked to stored, got %q", resp.OpenAIAPIKey)
	}
}

func TestPutEmbeddingConfig_EmptyKeyPreservesStoredKey(t *testing.T) {
	store := &mockStore{
		apiKeyRole: "superadmin",
		embeddingCfg: &storage.EmbeddingConfig{
			Provider:     "openai",
			Model:        "text-embedding-3-small",
			OpenAIAPIKey: "sk-original",
			Dimension:    1536,
		},
	}
	srv := newReembedAllServer(t, store)

	// Omitting openai_api_key entirely (decodes to "") preserves the stored key.
	req := authRequest("PUT", "/api/admin/embedding-config",
		`{"provider":"openai","model":"text-embedding-3-small","openai_base_url":"https://api.openai.com","ollama_url":""}`)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if store.embeddingCfg.OpenAIAPIKey != "sk-original" {
		t.Fatalf("want stored key preserved, got %q", store.embeddingCfg.OpenAIAPIKey)
	}
}

func TestPutEmbeddingConfig_RealKeyUpdates(t *testing.T) {
	store := &mockStore{
		apiKeyRole: "superadmin",
		embeddingCfg: &storage.EmbeddingConfig{
			Provider:     "openai",
			Model:        "text-embedding-3-small",
			OpenAIAPIKey: "sk-original",
			Dimension:    1536,
		},
	}
	srv := newReembedAllServer(t, store)

	req := authRequest("PUT", "/api/admin/embedding-config",
		`{"provider":"openai","model":"text-embedding-3-small","openai_api_key":"sk-new-value","openai_base_url":"https://api.openai.com","ollama_url":""}`)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if store.embeddingCfg.OpenAIAPIKey != "sk-new-value" {
		t.Fatalf("want key updated to sk-new-value, got %q", store.embeddingCfg.OpenAIAPIKey)
	}
	if store.embeddingCfg.Dimension != 1536 {
		t.Fatalf("want Dimension preserved at 1536, got %d", store.embeddingCfg.Dimension)
	}
}
