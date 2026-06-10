package web_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dsandor/memory/internal/aiconfig"
	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/web"
)

// settingsStore extends mockStore with configurable team settings.
// It embeds mockStore and overrides GetTeamSettings / PutTeamSettings.
type settingsStore struct {
	mockStore
	settings storage.TeamSettings
	putCalls []storage.TeamSettings
}

func (s *settingsStore) GetTeamSettings(_ context.Context, _ string) (*storage.TeamSettings, error) {
	cp := s.settings
	return &cp, nil
}

func (s *settingsStore) PutTeamSettings(_ context.Context, ts storage.TeamSettings) error {
	s.settings = ts
	s.putCalls = append(s.putCalls, ts)
	return nil
}

func newAITestServer(t *testing.T, store *settingsStore, src *aiconfig.Sources) *web.Server {
	t.Helper()
	staticFS := fstest.MapFS{
		"index.html": {Data: []byte("<html><body>app</body></html>"), Mode: 0444, ModTime: time.Now()},
	}
	srv := web.NewServer(staticFS, store)
	if src != nil {
		srv.WithAISources(src)
	}
	return srv
}

func newAISources(store *settingsStore, env aiconfig.EnvDefaults) *aiconfig.Sources {
	r := aiconfig.NewResolver(store, env)
	return &aiconfig.Sources{
		Resolver:    r,
		DefaultTeam: "test-team",
	}
}

// TestGetSettings_AIBlock verifies that GET /api/settings includes an "ai" block
// with correct source values, and that raw API key material is never in the response.
func TestGetSettings_AIBlock(t *testing.T) {
	store := &settingsStore{
		settings: storage.TeamSettings{
			TeamID:          "test-team",
			AnthropicAPIKey: "sk-ant-super-secret",
			AnthropicModel:  "claude-sonnet-4-6",
			OllamaURL:       "http://localhost:11434",
			OllamaModel:     "nomic-embed-text",
			AgentModel:      "claude-haiku-4-5-20251001",
		},
	}
	env := aiconfig.EnvDefaults{
		AnthropicAPIKey: "env-key-also-secret",
		AnthropicModel:  "env-model",
		OllamaURL:       "http://env-ollama",
	}
	// Make GetAPIKeyByHash return admin role (already done by mockStore default)
	store.mockStore = mockStore{}

	src := newAISources(store, env)
	srv := newAITestServer(t, store, src)

	req := authRequest("GET", "/api/settings", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()

	// Raw key material must NOT appear anywhere in the response.
	if strings.Contains(body, "sk-ant-super-secret") {
		t.Error("raw anthropic API key leaked in response body")
	}
	if strings.Contains(body, "env-key-also-secret") {
		t.Error("raw env API key leaked in response body")
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Top-level anthropic_api_key should be "stored" (existing masking).
	if resp["anthropic_api_key"] != "stored" {
		t.Errorf("top-level anthropic_api_key = %v, want stored", resp["anthropic_api_key"])
	}

	// "ai" block must exist.
	aiRaw, ok := resp["ai"]
	if !ok {
		t.Fatal("response missing 'ai' block")
	}
	ai, ok := aiRaw.(map[string]any)
	if !ok {
		t.Fatalf("ai block is not an object: %T", aiRaw)
	}

	// Helper to extract a nested field.
	getField := func(fieldName string) map[string]any {
		t.Helper()
		raw, ok := ai[fieldName]
		if !ok {
			t.Fatalf("ai block missing field %q", fieldName)
		}
		m, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("ai.%s is not an object: %T", fieldName, raw)
		}
		return m
	}

	// anthropic_api_key: saved wins; Effective/Saved/Env must all be "stored".
	keyField := getField("anthropic_api_key")
	for _, sub := range []string{"effective", "saved", "env"} {
		v := keyField[sub]
		if v != "stored" && v != "" {
			t.Errorf("ai.anthropic_api_key.%s = %v, want 'stored' or '' (never raw key)", sub, v)
		}
	}
	// The saved key is non-empty, so effective and saved should be "stored".
	if keyField["effective"] != "stored" {
		t.Errorf("ai.anthropic_api_key.effective = %v, want 'stored'", keyField["effective"])
	}
	if keyField["source"] != "saved" {
		t.Errorf("ai.anthropic_api_key.source = %v, want 'saved'", keyField["source"])
	}

	// anthropic_model: saved value should be returned unmasked.
	modelField := getField("anthropic_model")
	if modelField["effective"] != "claude-sonnet-4-6" {
		t.Errorf("ai.anthropic_model.effective = %v, want claude-sonnet-4-6", modelField["effective"])
	}
	if modelField["source"] != "saved" {
		t.Errorf("ai.anthropic_model.source = %v, want 'saved'", modelField["source"])
	}

	// ollama_url: saved value unmasked.
	ollamaField := getField("ollama_url")
	if ollamaField["effective"] != "http://localhost:11434" {
		t.Errorf("ai.ollama_url.effective = %v, want http://localhost:11434", ollamaField["effective"])
	}
}

// TestGetSettings_AIBlock_EnvFallback verifies that when no saved key exists, the
// env key is used (and masked) in the ai block.
func TestGetSettings_AIBlock_EnvFallback(t *testing.T) {
	store := &settingsStore{
		settings: storage.TeamSettings{TeamID: "test-team"},
	}
	store.mockStore = mockStore{}

	env := aiconfig.EnvDefaults{
		AnthropicAPIKey: "env-key-secret",
	}
	src := newAISources(store, env)
	srv := newAITestServer(t, store, src)

	req := authRequest("GET", "/api/settings", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if strings.Contains(body, "env-key-secret") {
		t.Error("raw env API key leaked in GET /api/settings response")
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	ai := resp["ai"].(map[string]any)
	keyField := ai["anthropic_api_key"].(map[string]any)

	if keyField["source"] != "env" {
		t.Errorf("source = %v, want env", keyField["source"])
	}
	if keyField["effective"] != "stored" {
		t.Errorf("effective = %v, want 'stored' (env key should be masked)", keyField["effective"])
	}
	if keyField["env"] != "stored" {
		t.Errorf("env = %v, want 'stored'", keyField["env"])
	}
}

// TestGetSettings_NoAISrc verifies backward-compatible response when aiSrc is nil.
func TestGetSettings_NoAISrc(t *testing.T) {
	store := &settingsStore{
		settings: storage.TeamSettings{
			TeamID:          "test-team",
			AnthropicAPIKey: "sk-ant-key",
		},
	}
	store.mockStore = mockStore{}
	srv := newAITestServer(t, store, nil) // no aiSrc

	req := authRequest("GET", "/api/settings", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// No "ai" block when aiSrc is nil.
	if _, ok := resp["ai"]; ok {
		t.Error("ai block present but aiSrc is nil")
	}
	// Top-level key still masked.
	if resp["anthropic_api_key"] != "stored" {
		t.Errorf("anthropic_api_key = %v, want stored", resp["anthropic_api_key"])
	}
}

// TestGetModels_OllamaHTTPTest verifies ollama model listing via a fake /api/tags server.
func TestGetModels_OllamaHTTPTest(t *testing.T) {
	// Fake Ollama server.
	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"nomic-embed-text"},{"name":"llama3.2"}]}`))
	}))
	defer ollamaServer.Close()

	store := &settingsStore{
		settings: storage.TeamSettings{
			TeamID:    "test-team",
			OllamaURL: ollamaServer.URL,
		},
	}
	store.mockStore = mockStore{}

	// No anthropic key → fallback.
	src := newAISources(store, aiconfig.EnvDefaults{})
	srv := newAITestServer(t, store, src)

	req := authRequest("GET", "/api/settings/models", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Ollama should be from the fake server.
	if resp["ollama_source"] != "api" {
		t.Errorf("ollama_source = %v, want api", resp["ollama_source"])
	}
	ollamaModels, ok := resp["ollama"].([]any)
	if !ok {
		t.Fatalf("ollama field missing or wrong type: %T", resp["ollama"])
	}
	if len(ollamaModels) != 2 {
		t.Errorf("want 2 ollama models, got %d", len(ollamaModels))
	}
	firstModel := ollamaModels[0].(map[string]any)
	if firstModel["id"] != "nomic-embed-text" {
		t.Errorf("first ollama model id = %v, want nomic-embed-text", firstModel["id"])
	}
}

// TestGetModels_AnthropicFallback verifies that missing key → curated fallback list.
func TestGetModels_AnthropicFallback(t *testing.T) {
	store := &settingsStore{
		settings: storage.TeamSettings{TeamID: "test-team"},
	}
	store.mockStore = mockStore{}

	src := newAISources(store, aiconfig.EnvDefaults{}) // no key
	srv := newAITestServer(t, store, src)

	req := authRequest("GET", "/api/settings/models", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp["anthropic_source"] != "fallback" {
		t.Errorf("anthropic_source = %v, want fallback", resp["anthropic_source"])
	}

	anthModels, ok := resp["anthropic"].([]any)
	if !ok {
		t.Fatalf("anthropic field missing or wrong type")
	}
	if len(anthModels) == 0 {
		t.Error("expected non-empty anthropic fallback list")
	}

	// Verify the curated IDs are present.
	ids := make(map[string]bool)
	for _, m := range anthModels {
		entry := m.(map[string]any)
		ids[entry["id"].(string)] = true
	}
	for _, wantID := range []string{"claude-fable-5", "claude-sonnet-4-6", "claude-opus-4-8", "claude-haiku-4-5-20251001"} {
		if !ids[wantID] {
			t.Errorf("fallback list missing model %q", wantID)
		}
	}
}

// TestGetModels_AnthropicAPI verifies model listing from a fake Anthropic API server.
func TestGetModels_AnthropicAPI(t *testing.T) {
	// Fake Anthropic models server.
	anthropicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-real-model","display_name":"Real Model"}]}`))
	}))
	defer anthropicServer.Close()

	// Override the Anthropic models URL to point at the test server.
	orig := web.AnthropicModelsURL
	web.AnthropicModelsURL = anthropicServer.URL
	defer func() { web.AnthropicModelsURL = orig }()

	store := &settingsStore{
		settings: storage.TeamSettings{
			TeamID:          "test-team",
			AnthropicAPIKey: "sk-test-key",
		},
	}
	store.mockStore = mockStore{}

	src := newAISources(store, aiconfig.EnvDefaults{})
	srv := newAITestServer(t, store, src)

	req := authRequest("GET", "/api/settings/models", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp["anthropic_source"] != "api" {
		t.Errorf("anthropic_source = %v, want api", resp["anthropic_source"])
	}
	anthModels := resp["anthropic"].([]any)
	if len(anthModels) != 1 {
		t.Fatalf("want 1 anthropic model from fake api, got %d", len(anthModels))
	}
	m := anthModels[0].(map[string]any)
	if m["id"] != "claude-real-model" {
		t.Errorf("model id = %v, want claude-real-model", m["id"])
	}
	if m["label"] != "Real Model" {
		t.Errorf("model label = %v, want Real Model", m["label"])
	}
}

// TestGetModels_NoAISrc verifies 503 when aiSrc is nil.
func TestGetModels_NoAISrc(t *testing.T) {
	store := &settingsStore{}
	store.mockStore = mockStore{}
	srv := newAITestServer(t, store, nil)

	req := authRequest("GET", "/api/settings/models", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", w.Code)
	}
}

// TestGetModels_OllamaUnavailable verifies that a bad Ollama URL returns empty list + "unavailable".
func TestGetModels_OllamaUnavailable(t *testing.T) {
	store := &settingsStore{
		settings: storage.TeamSettings{
			TeamID:    "test-team",
			OllamaURL: "http://127.0.0.1:1", // nothing listening
		},
	}
	store.mockStore = mockStore{}

	src := newAISources(store, aiconfig.EnvDefaults{})
	srv := newAITestServer(t, store, src)

	req := authRequest("GET", "/api/settings/models", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 even on ollama failure, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["ollama_source"] != "unavailable" {
		t.Errorf("ollama_source = %v, want unavailable", resp["ollama_source"])
	}
	ollamaModels := resp["ollama"].([]any)
	if len(ollamaModels) != 0 {
		t.Errorf("want empty ollama list, got %d", len(ollamaModels))
	}
}

// errorStore wraps settingsStore and returns a configurable error from GetTeamSettings.
// It is used to trigger the resolveEffective error path in handleGetModels.
type errorStore struct {
	settingsStore
	getErr error
}

func (s *errorStore) GetTeamSettings(_ context.Context, _ string) (*storage.TeamSettings, error) {
	return nil, s.getErr
}

// TestGetModels_ResolverError verifies that GET /api/settings/models returns 500
// when Resolver.Effective propagates a store error.
func TestGetModels_ResolverError(t *testing.T) {
	inner := &settingsStore{
		settings: storage.TeamSettings{TeamID: "test-team"},
	}
	inner.mockStore = mockStore{}

	es := &errorStore{
		settingsStore: *inner,
		getErr:        errors.New("simulated store failure"),
	}

	// The resolver is built on es, so Effective will surface the store error.
	r := aiconfig.NewResolver(es, aiconfig.EnvDefaults{})
	src := &aiconfig.Sources{Resolver: r, DefaultTeam: "test-team"}
	srv := newAITestServer(t, &es.settingsStore, src)

	req := authRequest("GET", "/api/settings/models", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetModels_AnthropicFallback_On401 verifies that a 401 from the Anthropic
// API endpoint causes fetchAnthropicModels to fall back to the curated list.
func TestGetModels_AnthropicFallback_On401(t *testing.T) {
	// Fake Anthropic server that always returns 401.
	anthropicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	}))
	defer anthropicServer.Close()

	orig := web.AnthropicModelsURL
	web.AnthropicModelsURL = anthropicServer.URL
	defer func() { web.AnthropicModelsURL = orig }()

	store := &settingsStore{
		settings: storage.TeamSettings{
			TeamID:          "test-team",
			AnthropicAPIKey: "sk-bad-key", // key present so we attempt the call
		},
	}
	store.mockStore = mockStore{}

	src := newAISources(store, aiconfig.EnvDefaults{})
	srv := newAITestServer(t, store, src)

	req := authRequest("GET", "/api/settings/models", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp["anthropic_source"] != "fallback" {
		t.Errorf("anthropic_source = %v, want fallback (401 should trigger fallback)", resp["anthropic_source"])
	}

	anthModels, ok := resp["anthropic"].([]any)
	if !ok || len(anthModels) == 0 {
		t.Fatal("expected non-empty anthropic fallback list after 401")
	}

	ids := make(map[string]bool)
	for _, m := range anthModels {
		entry := m.(map[string]any)
		ids[entry["id"].(string)] = true
	}
	for _, wantID := range []string{"claude-fable-5", "claude-sonnet-4-6", "claude-opus-4-8", "claude-haiku-4-5-20251001"} {
		if !ids[wantID] {
			t.Errorf("fallback list (after 401) missing model %q", wantID)
		}
	}
}

// TestImportEnv_CopiesRequestedFields verifies that POST /api/settings/import-env
// copies only requested non-empty env fields into saved settings.
func TestImportEnv_CopiesRequestedFields(t *testing.T) {
	store := &settingsStore{
		settings: storage.TeamSettings{
			TeamID:         "test-team",
			AnthropicModel: "existing-model",
			OllamaURL:      "http://existing-ollama",
		},
	}
	store.mockStore = mockStore{}

	env := aiconfig.EnvDefaults{
		AnthropicAPIKey: "env-api-key",
		AnthropicModel:  "env-model",
		// OllamaURL intentionally empty
	}
	src := newAISources(store, env)
	srv := newAITestServer(t, store, src)

	// Request import of anthropic_api_key, anthropic_model, and ollama_url.
	// ollama_url has empty env → should be skipped.
	body := `{"fields":["anthropic_api_key","anthropic_model","ollama_url"]}`
	req := authRequest("POST", "/api/settings/import-env", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the stored settings were updated.
	if len(store.putCalls) != 1 {
		t.Fatalf("want 1 PutTeamSettings call, got %d", len(store.putCalls))
	}
	saved := store.putCalls[0]
	if saved.AnthropicAPIKey != "env-api-key" {
		t.Errorf("AnthropicAPIKey = %q, want env-api-key", saved.AnthropicAPIKey)
	}
	if saved.AnthropicModel != "env-model" {
		t.Errorf("AnthropicModel = %q, want env-model", saved.AnthropicModel)
	}
	// OllamaURL had empty env — existing value should be preserved.
	if saved.OllamaURL != "http://existing-ollama" {
		t.Errorf("OllamaURL = %q, want http://existing-ollama (unchanged)", saved.OllamaURL)
	}
}

// TestImportEnv_UnknownField returns 400.
func TestImportEnv_UnknownField(t *testing.T) {
	store := &settingsStore{
		settings: storage.TeamSettings{TeamID: "test-team"},
	}
	store.mockStore = mockStore{}

	src := newAISources(store, aiconfig.EnvDefaults{})
	srv := newAITestServer(t, store, src)

	body := `{"fields":["anthropic_api_key","not_a_real_field"]}`
	req := authRequest("POST", "/api/settings/import-env", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for unknown field, got %d: %s", w.Code, w.Body.String())
	}
}

// TestImportEnv_EmptyEnvFieldSkipped verifies that fields with empty env values
// are not written and are not considered errors.
func TestImportEnv_EmptyEnvFieldSkipped(t *testing.T) {
	store := &settingsStore{
		settings: storage.TeamSettings{
			TeamID:         "test-team",
			AnthropicModel: "kept-model",
		},
	}
	store.mockStore = mockStore{}

	// OllamaModel env is empty.
	src := newAISources(store, aiconfig.EnvDefaults{OllamaModel: ""})
	srv := newAITestServer(t, store, src)

	body := `{"fields":["ollama_model"]}`
	req := authRequest("POST", "/api/settings/import-env", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 for empty-env skip, got %d: %s", w.Code, w.Body.String())
	}

	// PutTeamSettings still called (read-modify-write preserves existing fields).
	if len(store.putCalls) != 1 {
		t.Fatalf("want 1 PutTeamSettings call, got %d", len(store.putCalls))
	}
	// AnthropicModel should still be preserved.
	if store.putCalls[0].AnthropicModel != "kept-model" {
		t.Errorf("AnthropicModel = %q, want kept-model", store.putCalls[0].AnthropicModel)
	}
	// OllamaModel should remain empty (env was empty).
	if store.putCalls[0].OllamaModel != "" {
		t.Errorf("OllamaModel = %q, want empty", store.putCalls[0].OllamaModel)
	}
}

// TestImportEnv_ResponseMasked verifies that the response ai block masks the API key.
func TestImportEnv_ResponseMasked(t *testing.T) {
	store := &settingsStore{
		settings: storage.TeamSettings{TeamID: "test-team"},
	}
	store.mockStore = mockStore{}

	env := aiconfig.EnvDefaults{
		AnthropicAPIKey: "env-secret-key",
	}
	src := newAISources(store, env)
	srv := newAITestServer(t, store, src)

	body := `{"fields":["anthropic_api_key"]}`
	req := authRequest("POST", "/api/settings/import-env", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	responseBody := w.Body.String()
	if strings.Contains(responseBody, "env-secret-key") {
		t.Error("raw API key leaked in import-env response")
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(responseBody), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	aiRaw, ok := resp["ai"]
	if !ok {
		t.Fatal("response missing 'ai' block")
	}
	ai := aiRaw.(map[string]any)
	keyField := ai["anthropic_api_key"].(map[string]any)

	// The saved key (imported from env) should be masked.
	if keyField["effective"] != "stored" {
		t.Errorf("effective = %v, want 'stored'", keyField["effective"])
	}
}

// TestImportEnv_NoAISrc returns 503.
func TestImportEnv_NoAISrc(t *testing.T) {
	store := &settingsStore{}
	store.mockStore = mockStore{}
	srv := newAITestServer(t, store, nil)

	req := authRequest("POST", "/api/settings/import-env", `{"fields":["anthropic_api_key"]}`)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", w.Code)
	}
}

// TestImportEnv_PreservesUntouchedFields verifies that fields not in the request
// are not modified in the saved settings.
func TestImportEnv_PreservesUntouchedFields(t *testing.T) {
	store := &settingsStore{
		settings: storage.TeamSettings{
			TeamID:           "test-team",
			OllamaURL:        "http://my-ollama",
			OllamaModel:      "my-model",
			ClusterThreshold: 0.90,
			PipelineMinEntries: 20,
			Domains:          []string{"finance", "tech"},
		},
	}
	store.mockStore = mockStore{}

	env := aiconfig.EnvDefaults{
		AnthropicAPIKey: "env-key",
	}
	src := newAISources(store, env)
	srv := newAITestServer(t, store, src)

	// Only import the API key; everything else should be unchanged.
	body := `{"fields":["anthropic_api_key"]}`
	req := authRequest("POST", "/api/settings/import-env", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	saved := store.putCalls[0]
	if saved.OllamaURL != "http://my-ollama" {
		t.Errorf("OllamaURL = %q, want http://my-ollama", saved.OllamaURL)
	}
	if saved.OllamaModel != "my-model" {
		t.Errorf("OllamaModel = %q, want my-model", saved.OllamaModel)
	}
	if saved.ClusterThreshold != 0.90 {
		t.Errorf("ClusterThreshold = %v, want 0.90", saved.ClusterThreshold)
	}
	if saved.PipelineMinEntries != 20 {
		t.Errorf("PipelineMinEntries = %v, want 20", saved.PipelineMinEntries)
	}
	if len(saved.Domains) != 2 {
		t.Errorf("Domains len = %d, want 2", len(saved.Domains))
	}
	if saved.AnthropicAPIKey != "env-key" {
		t.Errorf("AnthropicAPIKey = %q, want env-key", saved.AnthropicAPIKey)
	}
}
