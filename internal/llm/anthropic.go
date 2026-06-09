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

const anthropicMessagesPath = "/v1/messages"
const anthropicAPIVersion = "2023-06-01"
const maxRetries = 3

// AnthropicClient calls the Anthropic Messages API.
type AnthropicClient struct {
	apiKey     string
	model      string
	client     *http.Client
	baseURL    string
	retryDelay func(attempt int) time.Duration
}

// NewAnthropicClient creates a client with default retry backoff.
func NewAnthropicClient(apiKey, model string) *AnthropicClient {
	return &AnthropicClient{
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: 60 * time.Second},
		baseURL: "https://api.anthropic.com",
		retryDelay: func(attempt int) time.Duration {
			return time.Duration(1<<attempt) * time.Second
		},
	}
}

type anthropicRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type apiError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type anthropicResponse struct {
	Content []contentBlock `json:"content"`
	Error   *apiError      `json:"error"`
}

// Complete sends prompt to the Anthropic API and returns the text response.
// Retries on 429 and 5xx with exponential backoff.
func (c *AnthropicClient) Complete(ctx context.Context, prompt string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	body, err := json.Marshal(anthropicRequest{
		Model:     c.model,
		MaxTokens: 1024,
		Messages:  []message{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + anthropicMessagesPath
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(c.retryDelay(attempt)):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", c.apiKey)
		req.Header.Set("anthropic-version", anthropicAPIVersion)

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("http: %w", err)
			continue
		}
		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read body: %w", err)
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("http %d: %s", resp.StatusCode, string(respBody))
			continue
		}

		var result anthropicResponse
		if err := json.Unmarshal(respBody, &result); err != nil {
			return "", fmt.Errorf("unmarshal response: %w", err)
		}
		if result.Error != nil {
			return "", fmt.Errorf("anthropic %s: %s", result.Error.Type, result.Error.Message)
		}
		for _, block := range result.Content {
			if block.Type == "text" {
				return block.Text, nil
			}
		}
		return "", fmt.Errorf("no text block in response")
	}
	return "", fmt.Errorf("max retries exceeded: %w", lastErr)
}

var _ Client = (*AnthropicClient)(nil)
