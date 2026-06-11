package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestOllama(t *testing.T, handler http.HandlerFunc) *OllamaClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewOllamaClient(srv.URL, "llama3.1")
	c.retryDelay = func(int) time.Duration { return 0 }
	return c
}

func TestOllamaClient_Complete(t *testing.T) {
	c := newTestOllama(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("path = %s, want /api/generate", r.URL.Path)
		}
		w.Write([]byte(`{"response": "hello from ollama"}`))
	})
	got, err := c.Complete(context.Background(), "hi")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if got != "hello from ollama" {
		t.Fatalf("got %q", got)
	}
}

func TestOllamaClient_APIError(t *testing.T) {
	c := newTestOllama(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error": "model not found"}`))
	})
	if _, err := c.Complete(context.Background(), "hi"); err == nil {
		t.Fatal("expected error for 400 response")
	}
}

func TestOllamaClient_RetryOn500(t *testing.T) {
	attempts := 0
	c := newTestOllama(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(500)
			return
		}
		w.Write([]byte(`{"response": "ok"}`))
	})
	got, err := c.Complete(context.Background(), "hi")
	if err != nil || got != "ok" {
		t.Fatalf("got %q err=%v after %d attempts", got, err, attempts)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestProviderOllama_CachingAndNil(t *testing.T) {
	p := NewProvider()
	a := p.Ollama("http://localhost:11434", "llama3.1")
	b := p.Ollama("http://localhost:11434", "llama3.1")
	if a == nil || a != b {
		t.Fatal("expected identical cached client instance")
	}
	if p.Ollama("", "llama3.1") != nil {
		t.Fatal("expected nil for empty url")
	}
	if p.Ollama("http://localhost:11434", "") != nil {
		t.Fatal("expected nil for empty model")
	}
	if c := p.Client("key", "model"); c == a {
		t.Fatal("anthropic and ollama cache entries must not collide")
	}
}
