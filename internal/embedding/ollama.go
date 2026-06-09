package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type OllamaEmbedder struct {
	url    string
	model  string
	client *http.Client
}

func NewOllamaEmbedder(url, model string) *OllamaEmbedder {
	return &OllamaEmbedder{
		url:    url,
		model:  model,
		client: &http.Client{},
	}
}

// Ping checks whether the Ollama endpoint is reachable by calling GET /api/tags.
// Returns nil if the HTTP response status is 2xx within 3 seconds.
func (e *OllamaEmbedder) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.url+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("ollama ping: create request: %w", err)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("ollama ping: status %d", resp.StatusCode)
	}
	return nil
}

func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body, _ := json.Marshal(map[string]string{
		"model":  e.model,
		"prompt": text,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama status %d", resp.StatusCode)
	}

	var result struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("ollama returned empty embedding")
	}
	return result.Embedding, nil
}
