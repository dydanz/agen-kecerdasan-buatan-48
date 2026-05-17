// internal/config/config_test.go
package config_test

import (
	"os"
	"testing"

	"github.com/dydanz/akb48/internal/config"
)

func TestLoad_ValidConfig(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg, err := config.Load("testdata/valid.toml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LLM.Model != "claude-sonnet-4-6-20260326" {
		t.Errorf("got model %q, want claude-sonnet-4-6-20260326", cfg.LLM.Model)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	_, err := config.Load("testdata/nonexistent.toml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoad_MissingModel(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	_, err := config.Load("testdata/missing_model.toml")
	if err == nil {
		t.Fatal("expected error for missing model, got nil")
	}
}

func TestLoad_MissingAPIKey(t *testing.T) {
	os.Unsetenv("ANTHROPIC_API_KEY")

	_, err := config.Load("testdata/valid.toml")
	if err == nil {
		t.Fatal("expected error for missing API key env var, got nil")
	}
}

func TestLoad_DefaultsApplied(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg, err := config.Load("testdata/valid.toml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LLM.MaxToolRounds != 5 {
		t.Errorf("got MaxToolRounds %d, want 5", cfg.LLM.MaxToolRounds)
	}
	if cfg.LLM.MaxToolResultTokens != 500 {
		t.Errorf("got MaxToolResultTokens %d, want 500", cfg.LLM.MaxToolResultTokens)
	}
	if cfg.Session.MaxContextTokens != 32000 {
		t.Errorf("got MaxContextTokens %d, want 32000", cfg.Session.MaxContextTokens)
	}
	if cfg.Session.ColdResumeThresholdMinutes != 30 {
		t.Errorf("got ColdResumeThresholdMinutes %d, want 30", cfg.Session.ColdResumeThresholdMinutes)
	}
	if cfg.Brain.ToolPrefix != "gbrain" {
		t.Errorf("got ToolPrefix %q, want gbrain", cfg.Brain.ToolPrefix)
	}
}

func TestAnthropicAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")

	cfg, err := config.Load("testdata/valid.toml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.AnthropicAPIKey(); got != "sk-ant-test" {
		t.Errorf("got %q, want sk-ant-test", got)
	}
}
