package aiconfig_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dsandor/memory/internal/aiconfig"
	"github.com/dsandor/memory/internal/storage"
)

// fakeStore is a test double for SettingsStore.
type fakeStore struct {
	settings *storage.TeamSettings
	err      error
}

func (f *fakeStore) GetTeamSettings(_ context.Context, _ string) (*storage.TeamSettings, error) {
	return f.settings, f.err
}

// helpers

func env() aiconfig.EnvDefaults {
	return aiconfig.EnvDefaults{
		AnthropicAPIKey: "env-key",
		AnthropicModel:  "env-model",
		AgentModel:      "env-agent",
		OllamaURL:       "http://env-ollama",
		OllamaModel:     "env-ollama-model",
	}
}

func savedSettings() *storage.TeamSettings {
	return &storage.TeamSettings{
		TeamID:          "t1",
		AnthropicAPIKey: "saved-key",
		AnthropicModel:  "saved-model",
		AgentModel:      "saved-agent",
		OllamaURL:       "http://saved-ollama",
		OllamaModel:     "saved-ollama-model",
	}
}

// Test: saved values win over env defaults for every field.
func TestSavedWins(t *testing.T) {
	r := aiconfig.NewResolver(&fakeStore{settings: savedSettings()}, env())
	cfg, err := r.Effective(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cases := []struct {
		name  string
		field aiconfig.FieldValue
		want  string
	}{
		{"AnthropicAPIKey", cfg.AnthropicAPIKey, "saved-key"},
		{"AnthropicModel", cfg.AnthropicModel, "saved-model"},
		{"AgentModel", cfg.AgentModel, "saved-agent"},
		{"OllamaURL", cfg.OllamaURL, "http://saved-ollama"},
		{"OllamaModel", cfg.OllamaModel, "saved-ollama-model"},
	}
	for _, c := range cases {
		if c.field.Effective != c.want {
			t.Errorf("%s: Effective = %q, want %q", c.name, c.field.Effective, c.want)
		}
		if c.field.Source != "saved" {
			t.Errorf("%s: Source = %q, want %q", c.name, c.field.Source, "saved")
		}
		if c.field.Saved != c.want {
			t.Errorf("%s: Saved = %q, want %q", c.name, c.field.Saved, c.want)
		}
	}
}

// Test: env defaults used when saved fields are empty.
func TestEnvFallback(t *testing.T) {
	r := aiconfig.NewResolver(&fakeStore{settings: &storage.TeamSettings{TeamID: "t1"}}, env())
	cfg, err := r.Effective(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cases := []struct {
		name  string
		field aiconfig.FieldValue
		want  string
	}{
		{"AnthropicAPIKey", cfg.AnthropicAPIKey, "env-key"},
		{"AnthropicModel", cfg.AnthropicModel, "env-model"},
		{"AgentModel", cfg.AgentModel, "env-agent"},
		{"OllamaURL", cfg.OllamaURL, "http://env-ollama"},
		{"OllamaModel", cfg.OllamaModel, "env-ollama-model"},
	}
	for _, c := range cases {
		if c.field.Effective != c.want {
			t.Errorf("%s: Effective = %q, want %q", c.name, c.field.Effective, c.want)
		}
		if c.field.Source != "env" {
			t.Errorf("%s: Source = %q, want %q", c.name, c.field.Source, "env")
		}
		if c.field.Saved != "" {
			t.Errorf("%s: Saved should be empty, got %q", c.name, c.field.Saved)
		}
	}
}

// Test: both saved and env empty → Source "none", Effective "".
func TestBothEmpty(t *testing.T) {
	emptyEnv := aiconfig.EnvDefaults{}
	r := aiconfig.NewResolver(&fakeStore{settings: &storage.TeamSettings{TeamID: "t1"}}, emptyEnv)
	cfg, err := r.Effective(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fields := []struct {
		name  string
		field aiconfig.FieldValue
	}{
		{"AnthropicAPIKey", cfg.AnthropicAPIKey},
		{"AnthropicModel", cfg.AnthropicModel},
		{"AgentModel", cfg.AgentModel},
		{"OllamaURL", cfg.OllamaURL},
		{"OllamaModel", cfg.OllamaModel},
	}
	for _, f := range fields {
		if f.field.Source != "none" {
			t.Errorf("%s: Source = %q, want %q", f.name, f.field.Source, "none")
		}
		if f.field.Effective != "" {
			t.Errorf("%s: Effective = %q, want empty", f.name, f.field.Effective)
		}
	}
}

// Test: ErrNotFound from store → all env defaults used, no error.
func TestNotFoundAllEnv(t *testing.T) {
	r := aiconfig.NewResolver(&fakeStore{err: storage.ErrNotFound}, env())
	cfg, err := r.Effective(context.Background(), "missing-team")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AnthropicAPIKey.Effective != "env-key" {
		t.Errorf("AnthropicAPIKey: got %q, want %q", cfg.AnthropicAPIKey.Effective, "env-key")
	}
	if cfg.AnthropicAPIKey.Source != "env" {
		t.Errorf("AnthropicAPIKey: Source = %q, want env", cfg.AnthropicAPIKey.Source)
	}
}

// Test: non-ErrNotFound store error propagates.
func TestStoreErrorPropagates(t *testing.T) {
	storeErr := errors.New("database unavailable")
	r := aiconfig.NewResolver(&fakeStore{err: storeErr}, env())
	_, err := r.Effective(context.Background(), "t1")
	if !errors.Is(err, storeErr) {
		t.Fatalf("expected storeErr to propagate, got: %v", err)
	}
}

// Test: nil pointer returned without error → treated as not found, all env.
func TestNilSettingsPointer(t *testing.T) {
	r := aiconfig.NewResolver(&fakeStore{settings: nil, err: nil}, env())
	cfg, err := r.Effective(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AgentModel.Source != "env" {
		t.Errorf("AgentModel: Source = %q, want env", cfg.AgentModel.Source)
	}
}

// Test: per-field independence — saved model + env key (AnthropicAPIKey only saved).
func TestPerFieldIndependence(t *testing.T) {
	partial := &storage.TeamSettings{
		TeamID:         "t1",
		AnthropicModel: "saved-model-only",
		// AnthropicAPIKey intentionally left empty → should fall back to env
	}
	r := aiconfig.NewResolver(&fakeStore{settings: partial}, env())
	cfg, err := r.Effective(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// AnthropicModel has a saved value.
	if cfg.AnthropicModel.Effective != "saved-model-only" {
		t.Errorf("AnthropicModel: Effective = %q, want saved-model-only", cfg.AnthropicModel.Effective)
	}
	if cfg.AnthropicModel.Source != "saved" {
		t.Errorf("AnthropicModel: Source = %q, want saved", cfg.AnthropicModel.Source)
	}

	// AnthropicAPIKey has no saved value → falls back to env.
	if cfg.AnthropicAPIKey.Effective != "env-key" {
		t.Errorf("AnthropicAPIKey: Effective = %q, want env-key", cfg.AnthropicAPIKey.Effective)
	}
	if cfg.AnthropicAPIKey.Source != "env" {
		t.Errorf("AnthropicAPIKey: Source = %q, want env", cfg.AnthropicAPIKey.Source)
	}
}

// Test: Env field is always populated on FieldValue even when saved wins.
func TestEnvFieldAlwaysPopulated(t *testing.T) {
	r := aiconfig.NewResolver(&fakeStore{settings: savedSettings()}, env())
	cfg, err := r.Effective(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Even though saved wins, the Env field should still reflect the env default.
	if cfg.AnthropicAPIKey.Env != "env-key" {
		t.Errorf("AnthropicAPIKey.Env = %q, want env-key", cfg.AnthropicAPIKey.Env)
	}
}

// Test: LLMProvider and OllamaLLMModel resolve correctly in all three scenarios.
func TestEffectiveLLMProviderAndOllamaModel(t *testing.T) {
	t.Run("saved wins over env", func(t *testing.T) {
		saved := &storage.TeamSettings{
			TeamID:         "t1",
			LLMProvider:    "ollama",
			OllamaLLMModel: "llama3.1",
		}
		envD := aiconfig.EnvDefaults{LLMProvider: "anthropic"}
		r := aiconfig.NewResolver(&fakeStore{settings: saved}, envD)
		cfg, err := r.Effective(context.Background(), "t1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.LLMProvider.Effective != "ollama" {
			t.Errorf("LLMProvider.Effective = %q, want ollama", cfg.LLMProvider.Effective)
		}
		if cfg.LLMProvider.Source != "saved" {
			t.Errorf("LLMProvider.Source = %q, want saved", cfg.LLMProvider.Source)
		}
		if cfg.OllamaLLMModel.Effective != "llama3.1" {
			t.Errorf("OllamaLLMModel.Effective = %q, want llama3.1", cfg.OllamaLLMModel.Effective)
		}
		if cfg.OllamaLLMModel.Source != "saved" {
			t.Errorf("OllamaLLMModel.Source = %q, want saved", cfg.OllamaLLMModel.Source)
		}
	})

	t.Run("env used when saved empty", func(t *testing.T) {
		saved := &storage.TeamSettings{TeamID: "t1"} // LLMProvider and OllamaLLMModel empty
		envD := aiconfig.EnvDefaults{LLMProvider: "ollama"}
		r := aiconfig.NewResolver(&fakeStore{settings: saved}, envD)
		cfg, err := r.Effective(context.Background(), "t1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.LLMProvider.Effective != "ollama" {
			t.Errorf("LLMProvider.Effective = %q, want ollama", cfg.LLMProvider.Effective)
		}
		if cfg.LLMProvider.Source != "env" {
			t.Errorf("LLMProvider.Source = %q, want env", cfg.LLMProvider.Source)
		}
	})

	t.Run("both empty yields none", func(t *testing.T) {
		saved := &storage.TeamSettings{TeamID: "t1"}
		envD := aiconfig.EnvDefaults{}
		r := aiconfig.NewResolver(&fakeStore{settings: saved}, envD)
		cfg, err := r.Effective(context.Background(), "t1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.LLMProvider.Effective != "" {
			t.Errorf("LLMProvider.Effective = %q, want empty", cfg.LLMProvider.Effective)
		}
		if cfg.LLMProvider.Source != "none" {
			t.Errorf("LLMProvider.Source = %q, want none", cfg.LLMProvider.Source)
		}
		if cfg.OllamaLLMModel.Effective != "" {
			t.Errorf("OllamaLLMModel.Effective = %q, want empty", cfg.OllamaLLMModel.Effective)
		}
		if cfg.OllamaLLMModel.Source != "none" {
			t.Errorf("OllamaLLMModel.Source = %q, want none", cfg.OllamaLLMModel.Source)
		}
	})
}
