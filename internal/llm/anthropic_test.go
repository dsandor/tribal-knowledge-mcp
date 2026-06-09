package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *AnthropicClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &AnthropicClient{
		apiKey:     "test-key",
		model:      "test-model",
		client:     srv.Client(),
		baseURL:    srv.URL,
		retryDelay: func(int) time.Duration { return 0 },
	}
}

func okHandler(t *testing.T, text string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("missing or wrong x-api-key header")
		}
		if r.Header.Get("anthropic-version") != anthropicAPIVersion {
			t.Errorf("anthropic-version: got %q, want %q", r.Header.Get("anthropic-version"), anthropicAPIVersion)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{
			Content: []contentBlock{{Type: "text", Text: text}},
		})
	}
}

func TestAnthropicClient_Complete(t *testing.T) {
	c := newTestClient(t, okHandler(t, "hello world"))
	got, err := c.Complete(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestAnthropicClient_RetryOn429(t *testing.T) {
	attempts := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		okHandler(t, "ok")(w, r)
	})
	got, err := c.Complete(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ok" {
		t.Errorf("got %q, want %q", got, "ok")
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestAnthropicClient_ErrorResponse(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{
			Error: &apiError{Type: "invalid_request_error", Message: "bad request"},
		})
	})
	_, err := c.Complete(context.Background(), "prompt")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAnthropicClient_RetryOnNetworkError(t *testing.T) {
	attempts := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("httptest server does not support hijacking")
			}
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		okHandler(t, "ok")(w, r)
	})
	got, err := c.Complete(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ok" {
		t.Errorf("got %q, want %q", got, "ok")
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}
