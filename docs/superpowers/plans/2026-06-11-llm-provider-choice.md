# Selectable LLM Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **PROJECT RULE — NO GIT COMMITS.** Never run `git add`/`git commit`. Every task ends at "tests pass".

**Goal:** Teams choose Anthropic or Ollama for all LLM work via a per-team `llm_provider` setting (with env default), dispatched inside `aiconfig.Sources` so no call sites change.

**Architecture:** New `OllamaClient` behind the existing one-method `llm.Client` interface; cached `Provider.Ollama(url, model)` factory; two new config fields (`llm_provider`, `ollama_llm_model`) threaded through team_settings → aiconfig resolver → Sources dispatch → settings API/UI. Unconfigured Ollama yields a nil client (same skip-and-log contract as a missing Anthropic key); no silent fallback.

**Tech Stack:** Go, SQLite/Postgres dual adapters, React+MUI settings page.

**Spec:** `docs/superpowers/specs/2026-06-11-llm-provider-choice-design.md`

**Key facts:**
- `llm.Client` interface: `internal/llm/client.go` — `Complete(ctx, prompt) (string, error)`.
- `llm.Provider` cache: `internal/llm/provider.go` — `Client(apiKey, model)` returns cached AnthropicClient, nil on empty key. Anthropic retry pattern + httptest tests: `internal/llm/anthropic.go` / `anthropic_test.go`.
- `aiconfig`: `internal/aiconfig/aiconfig.go` (EffectiveConfig/EnvDefaults/resolve), `sources.go` (AnalysisLLM/AgentLLM/ImprovementLLM; `LLMProvider` interface at :13 — `Client(apiKey, model) llm.Client`; ImprovementLLM pins `claude-haiku-4-5-20251001`).
- `storage.TeamSettings`: `internal/storage/teams.go:54-65`; team_settings columns + idempotent ALTERs in `internal/storage/sqlite.go:286-298` (teamSettingsAlters) and the Postgres teams migration (`internal/storage/postgres_teams.go` — find the team_settings DDL/alters); Get/PutTeamSettings in `teams_sqlite.go` + `postgres_teams.go` (read both — column lists must gain the new fields).
- Env config: `internal/config/config.go` (:12-18 fields, :93-99 envOrDefault wiring); `cmd/server/main.go` builds `aiconfig.EnvDefaults` from cfg (grep `EnvDefaults{`).
- Settings API: `internal/web/ai_settings.go` — GET builds map at :89-93 via `maskKeyFieldValue`/`plainFieldValue`; PUT allowlist at :306-311 with switch at :342-359; import-env response at :394-398. `internal/web/settings.go` PUT handler persists TeamSettings.
- Settings UI: `web/src/pages/Settings.tsx` (read it; uses `AISettings` from api.ts :282-288 — five fields with `AIFieldValue` shape) and `fetchModelOptions()` returns `{anthropic: ModelOption[], ollama: ModelOption[]}` already.
- `.env.example` documents env vars.

---

### Task 1: `llm.OllamaClient` + `Provider.Ollama`

**Files:**
- Create: `internal/llm/ollama.go`
- Modify: `internal/llm/provider.go`
- Test: `internal/llm/ollama_test.go`

- [ ] **Step 1: Write failing tests** — model them on `internal/llm/anthropic_test.go` (read it first; reuse its helper style):

```go
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
```

- [ ] **Step 2:** `cd /Users/dsandor/Projects/memory && go test ./internal/llm/ -run Ollama -v` — FAIL (undefined).

- [ ] **Step 3: Implement `internal/llm/ollama.go`** (mirror anthropic.go's structure — read it for the retry loop shape and reuse its style; key elements):

```go
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
		url:   url,
		model: model,
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
```

(`maxRetries` already exists in anthropic.go — same package.)

**`internal/llm/provider.go`** — add below `Client`:

```go
// Ollama returns a cached OllamaClient for the given (url, model) pair.
// Returns nil when url or model is empty (Ollama LLM not configured).
func (p *Provider) Ollama(url, model string) Client {
	if url == "" || model == "" {
		return nil
	}
	key := "ollama|" + url + "|" + model

	p.mu.Lock()
	defer p.mu.Unlock()

	if c, ok := p.cache[key]; ok {
		return c
	}
	c := NewOllamaClient(url, model)
	p.cache[key] = c
	return c
}
```

- [ ] **Step 4:** `go test ./internal/llm/ -v 2>&1 | grep -E "^(--- |ok|FAIL)"` — all PASS. gofmt clean.

---

### Task 2: Config plumbing — TeamSettings columns, env vars, resolver fields

**Files:**
- Modify: `internal/storage/teams.go:54-65` (struct), `internal/storage/sqlite.go:286-298` (teamSettingsAlters), `internal/storage/teams_sqlite.go` (Get/PutTeamSettings column lists), `internal/storage/postgres_teams.go` (DDL/alters + Get/Put)
- Modify: `internal/aiconfig/aiconfig.go`, `internal/config/config.go`, `cmd/server/main.go` (EnvDefaults literal), `.env.example`
- Test: `internal/aiconfig/aiconfig_test.go` (extend), storage team-settings round-trip test (find the existing TeamSettings test and extend)

- [ ] **Step 1: Failing tests.** Extend the existing resolver tests in `internal/aiconfig/aiconfig_test.go` (read for the fake-store pattern):

```go
// TestEffectiveLLMProviderAndOllamaModel: saved TeamSettings{LLMProvider: "ollama",
// OllamaLLMModel: "llama3.1"} + env defaults {LLMProvider: "anthropic", OllamaLLMModel: ""}
// → cfg.LLMProvider.Effective == "ollama" (Source "saved"),
//   cfg.OllamaLLMModel.Effective == "llama3.1".
// Empty saved + env LLMProvider "ollama" → Effective "ollama" Source "env".
// Both empty → Effective "" Source "none".
```

Extend the existing TeamSettings round-trip storage test (grep `PutTeamSettings` in `internal/storage/*_test.go`): save settings with `LLMProvider: "ollama", OllamaLLMModel: "llama3.1"`, re-Get, assert both fields round-trip.

- [ ] **Step 2:** Run those two test files — FAIL (unknown fields).

- [ ] **Step 3: Implement.**
- `TeamSettings` struct adds:
```go
	LLMProvider    string `json:"llm_provider"`     // "" | "anthropic" | "ollama"; empty means anthropic
	OllamaLLMModel string `json:"ollama_llm_model"` // chat model; distinct from OllamaModel (embeddings)
```
- SQLite `teamSettingsAlters` adds:
```go
		"ALTER TABLE team_settings ADD COLUMN llm_provider     TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE team_settings ADD COLUMN ollama_llm_model TEXT NOT NULL DEFAULT ''",
```
- Postgres: matching `ADD COLUMN IF NOT EXISTS` in the team_settings migration.
- Get/PutTeamSettings in BOTH adapters: add the two columns to SELECT/INSERT-UPSERT lists + scans (read each function; keep positions consistent between query and scan).
- `aiconfig.EffectiveConfig` adds `LLMProvider FieldValue \`json:"llm_provider"\`` and `OllamaLLMModel FieldValue \`json:"ollama_llm_model"\``; `EnvDefaults` adds `LLMProvider`, `OllamaLLMModel` strings; `Effective()` adds two `resolve(...)` lines.
- `internal/config/config.go`: fields `LLMProvider`, `OllamaLLMModel`; wiring `LLMProvider: envOrDefault("LLM_PROVIDER", "")`, `OllamaLLMModel: os.Getenv("OLLAMA_LLM_MODEL")` (empty default — anthropic remains the implicit default; do NOT default LLM_PROVIDER to "anthropic" so the settings UI shows Source "none" until chosen).
- `cmd/server/main.go`: add both to the `aiconfig.EnvDefaults{...}` literal.
- `.env.example`: document `LLM_PROVIDER=` (`anthropic` (default) or `ollama`) and `OLLAMA_LLM_MODEL=` (chat model used when LLM_PROVIDER=ollama, e.g. llama3.1).

- [ ] **Step 4:** `go build ./... && go test ./internal/aiconfig/ ./internal/storage/ 2>&1 | tail -3` — PASS.

---

### Task 3: Sources dispatch

**Files:**
- Modify: `internal/aiconfig/sources.go`
- Test: `internal/aiconfig/aiconfig_test.go` or a new `sources_test.go` (check where Sources tests live; create `internal/aiconfig/sources_test.go` if none)

- [ ] **Step 1: Failing tests** (fake LLMProvider records which factory was called):

```go
type fakeLLMProvider struct {
	anthropicCalls []string // "key|model"
	ollamaCalls    []string // "url|model"
	ret            llm.Client
}

func (f *fakeLLMProvider) Client(apiKey, model string) llm.Client {
	f.anthropicCalls = append(f.anthropicCalls, apiKey+"|"+model)
	if apiKey == "" {
		return nil
	}
	return f.ret
}
func (f *fakeLLMProvider) Ollama(url, model string) llm.Client {
	f.ollamaCalls = append(f.ollamaCalls, url+"|"+model)
	if url == "" || model == "" {
		return nil
	}
	return f.ret
}

// Table tests (build Sources with a resolver whose store returns the given saved settings):
// 1. Default (no provider saved/env): AnalysisLLM uses Client with anthropic model; no Ollama call.
// 2. Saved LLMProvider "ollama" + OllamaURL + OllamaLLMModel: AnalysisLLM/AgentLLM call
//    Ollama("url|llama3.1"); Client NOT called.
// 3. Provider "ollama" but OllamaLLMModel empty: returns nil (skip contract).
// 4. ImprovementLLM: anthropic → Client called with model "claude-haiku-4-5-20251001";
//    ollama → Ollama called with the team's chat model.
```

(`ret` can be any non-nil `llm.Client` — define a tiny fake with Complete returning "", nil.)

- [ ] **Step 2:** Run — FAIL (`Ollama` missing from LLMProvider interface).

- [ ] **Step 3: Implement in `sources.go`.** Extend the interface:

```go
type LLMProvider interface {
	Client(apiKey, model string) llm.Client
	Ollama(url, model string) llm.Client
}
```

Add the dispatch helper + rewrite the three methods to use it:

```go
// clientFor returns the LLM client for cfg's effective provider. anthropicModel
// is the model used when the provider is Anthropic (each resolver role pins its
// own). Provider "ollama" uses the team's Ollama chat model; anything else
// (including empty) means Anthropic for backward compatibility.
func (s *Sources) clientFor(cfg *EffectiveConfig, anthropicModel string) llm.Client {
	if cfg.LLMProvider.Effective == "ollama" {
		return s.LLM.Ollama(cfg.OllamaURL.Effective, cfg.OllamaLLMModel.Effective)
	}
	return s.LLM.Client(cfg.AnthropicAPIKey.Effective, anthropicModel)
}
```

`AnalysisLLM` → `return s.clientFor(cfg, cfg.AnthropicModel.Effective)`; `AgentLLM` → `cfg.AgentModel.Effective`; `ImprovementLLM` → `s.clientFor(cfg, "claude-haiku-4-5-20251001")` (keep its doc comment, noting the pin applies to the Anthropic side only). Keep the existing DefaultTeam fallback + error logging in each method.

- [ ] **Step 4:** `go build ./... && go test ./... 2>&1 | tail -5` — ALL pass (the `llm.Provider` concrete type already satisfies the extended interface from Task 1; any aiconfig/pipeline test fakes implementing LLMProvider need the new method — compiler will say).

---

### Task 4: Settings API (GET/PUT/import-env)

**Files:**
- Modify: `internal/web/ai_settings.go` (GET map :89-93, savedFieldValue-style builder :118-121, PUT allowlist :306-311 + switch :342-359, import-env response :394-398 — read the whole file first)
- Modify: `internal/web/settings.go` if it persists AI fields (read it)
- Test: extend the existing ai_settings tests (`internal/web/ai_settings_test.go`)

- [ ] **Step 1: Failing tests** (follow the file's existing GET/PUT test patterns):
```go
// TestAISettingsIncludesProviderFields: GET settings → response JSON has
// "llm_provider" and "ollama_llm_model" FieldValue objects.
// TestAISettingsPutProvider: PUT {"ai": {"llm_provider": "ollama", "ollama_llm_model": "llama3.1"}}
// (match the actual PUT body shape used by existing tests) → 200; re-GET shows
// effective "ollama"/"llama3.1" with source "saved".
// TestAISettingsPutInvalidProvider: PUT llm_provider "openai" → 400.
```

- [ ] **Step 2:** Run — FAIL.

- [ ] **Step 3: Implement.** Add both fields everywhere the existing five appear in ai_settings.go (GET response map via `plainFieldValue`, the saved-values builder, the PUT allowlist set, the PUT switch storing into TeamSettings, import-env handling + response). Validation in the PUT path before persisting:
```go
	if v, ok := body["llm_provider"]; ok {
		if v != "" && v != "anthropic" && v != "ollama" {
			writeError(w, 400, "bad_request", "llm_provider must be anthropic or ollama")
			return
		}
	}
```
(Adapt to the handler's actual decode shape — read how other fields are validated/coerced.)

- [ ] **Step 4:** `go test ./internal/web/ 2>&1 | tail -2` — PASS.

---

### Task 5: Settings UI

**Files:**
- Modify: `web/src/lib/api.ts` (`AISettings` :282-288 gains `llm_provider: AIFieldValue` and `ollama_llm_model: AIFieldValue`)
- Modify: `web/src/pages/Settings.tsx` (read fully first — mirror how existing AI fields render/save)

- [ ] **Step 1: Implement.**
- `AISettings` interface: add both fields.
- Settings page AI section: add an "LLM Provider" `Select` with options Anthropic (`anthropic`) / Ollama (`ollama`), value = the field's effective (empty → show "anthropic"); saving writes through the page's existing save flow. Below it, an "Ollama chat model" field rendered like the existing model dropdowns, fed from `fetchModelOptions().ollama` (the page already fetches this for the embedding model — reuse), shown always but visually secondary (helperText: "Used when provider is Ollama; separate from the embedding model"). Follow the page's existing source-chip/effective-value presentation exactly.

- [ ] **Step 2: Build:** `cd /Users/dsandor/Projects/memory/web && npm run build` — clean.

---

### Task 6: Final verification

- [ ] **Step 1:** `cd /Users/dsandor/Projects/memory && go build ./... && go test ./...` — all packages PASS.
- [ ] **Step 2:** `cd web && npm run build` — clean.
- [ ] **Step 3:** Grep sanity: `grep -rn "NewAnthropicClient\|\.Client(" internal/pipeline/ internal/mcp/ internal/tags/ internal/agent/ --include="*.go" | grep -v _test | grep -v aiconfig` — confirm no consumer constructs clients directly (all go through Sources). Report any hit.
- [ ] **Step 4:** Report. **Do not commit.**
