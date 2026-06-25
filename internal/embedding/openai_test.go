package embedding

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIEmbedder_Embed(t *testing.T) {
	var gotPath, gotAuth, gotModel, gotInput string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
			Input string `json:"input"`
		}
		_ = json.Unmarshal(body, &req)
		gotModel = req.Model
		gotInput = req.Input
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3]}]}`))
	}))
	defer srv.Close()

	e := NewOpenAIEmbedder(srv.URL, "k", "text-embedding-3-small")
	vec, err := e.Embed(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	want := []float32{0.1, 0.2, 0.3}
	if len(vec) != len(want) {
		t.Fatalf("got %d values, want %d (%v)", len(vec), len(want), vec)
	}
	for i := range want {
		if vec[i] != want[i] {
			t.Fatalf("vec[%d]=%v, want %v", i, vec[i], want[i])
		}
	}
	if gotPath != "/v1/embeddings" {
		t.Errorf("path = %q, want /v1/embeddings", gotPath)
	}
	if gotAuth != "Bearer k" {
		t.Errorf("auth = %q, want Bearer k", gotAuth)
	}
	if gotModel != "text-embedding-3-small" {
		t.Errorf("model = %q", gotModel)
	}
	if gotInput != "hi" {
		t.Errorf("input = %q", gotInput)
	}
}

func TestOpenAIEmbedder_Embed_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()

	e := NewOpenAIEmbedder(srv.URL, "bad", "text-embedding-3-small")
	_, err := e.Embed(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
}

func TestOpenAIEmbedder_DefaultBaseURL(t *testing.T) {
	e := NewOpenAIEmbedder("", "k", "text-embedding-3-small")
	if e.baseURL != "https://api.openai.com" {
		t.Errorf("baseURL = %q, want https://api.openai.com", e.baseURL)
	}
}

func TestModelDimension(t *testing.T) {
	cases := map[string]int{
		"text-embedding-3-small": 1536,
		"text-embedding-3-large": 3072,
		"text-embedding-ada-002": 1536,
		"unknown-model":          0,
		"":                       0,
	}
	for model, want := range cases {
		if got := ModelDimension(model); got != want {
			t.Errorf("ModelDimension(%q) = %d, want %d", model, got, want)
		}
	}
}

func TestProvider_OpenAIEmbedder_Cache(t *testing.T) {
	p := NewProvider()
	a := p.OpenAIEmbedder("https://api.openai.com", "key1", "text-embedding-3-small")
	b := p.OpenAIEmbedder("https://api.openai.com", "key1", "text-embedding-3-small")
	if a != b {
		t.Error("expected identical cached embedder for same (base, key, model)")
	}
	c := p.OpenAIEmbedder("https://api.openai.com", "key2", "text-embedding-3-small")
	if a == c {
		t.Error("expected distinct embedder when api key differs")
	}
	if a == nil {
		t.Fatal("expected non-nil embedder")
	}
}
