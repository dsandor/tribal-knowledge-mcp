package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "# comment\n\nLLM_PROVIDER=ollama\nexport OLLAMA_LLM_MODEL=\"qwen3\"\nALREADY_SET=from-file\nMALFORMED LINE\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	t.Setenv("ALREADY_SET", "from-env")
	os.Unsetenv("LLM_PROVIDER")
	os.Unsetenv("OLLAMA_LLM_MODEL")
	t.Cleanup(func() {
		os.Unsetenv("LLM_PROVIDER")
		os.Unsetenv("OLLAMA_LLM_MODEL")
	})

	loadDotEnv(path)

	if got := os.Getenv("LLM_PROVIDER"); got != "ollama" {
		t.Errorf("LLM_PROVIDER = %q, want ollama", got)
	}
	if got := os.Getenv("OLLAMA_LLM_MODEL"); got != "qwen3" {
		t.Errorf("OLLAMA_LLM_MODEL = %q, want qwen3 (export prefix + quotes stripped)", got)
	}
	if got := os.Getenv("ALREADY_SET"); got != "from-env" {
		t.Errorf("ALREADY_SET = %q, want from-env (real env must win)", got)
	}
}

func TestLoadDotEnvMissingFileIsNoop(t *testing.T) {
	loadDotEnv(filepath.Join(t.TempDir(), "nope.env")) // must not panic or error
}
