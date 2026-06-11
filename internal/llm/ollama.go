package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const ollamaGeneratePath = "/api/generate"

// OllamaClient calls a local Ollama server's generate API.
type OllamaClient struct {
	url        string
	model      string
	client     *http.Client
	retryDelay func(attempt int) time.Duration
}

// NewOllamaClient creates a client with default retry backoff. The timeout is
// generous because local models can be slow to load and generate.
func NewOllamaClient(url, model string) *OllamaClient {
	return &OllamaClient{
		url:    url,
		model:  model,
		client: &http.Client{Timeout: 120 * time.Second},
		retryDelay: func(attempt int) time.Duration {
			return time.Duration(1<<attempt) * time.Second
		},
	}
}

type ollamaGenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type ollamaGenerateResponse struct {
	Response string `json:"response"`
	Error    string `json:"error"`
}

// Complete sends prompt to Ollama's generate endpoint and returns the text.
// Retries on 5xx and network errors with exponential backoff (max 3 attempts,
// same shape as AnthropicClient).
func (c *OllamaClient) Complete(ctx context.Context, prompt string) (string, error) {
	body, err := json.Marshal(ollamaGenerateRequest{Model: c.model, Prompt: prompt, Stream: false})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(c.retryDelay(attempt)):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+ollamaGeneratePath, bytes.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("ollama request: %w", err)
			continue // network error — retry
		}
		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read response: %w", err)
			continue
		}

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("ollama server error %d: %.200s", resp.StatusCode, respBody)
			continue // retry
		}

		var out ollamaGenerateResponse
		if err := json.Unmarshal(respBody, &out); err != nil {
			return "", fmt.Errorf("parse ollama response: %w (raw: %.200s)", err, respBody)
		}
		if resp.StatusCode != http.StatusOK || out.Error != "" {
			return "", fmt.Errorf("ollama error (status %d): %s", resp.StatusCode, out.Error)
		}
		return out.Response, nil
	}
	return "", fmt.Errorf("ollama: retries exhausted: %w", lastErr)
}

var _ Client = (*OllamaClient)(nil)
