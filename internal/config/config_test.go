package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dydanz/klawmbing/internal/config"
)

func TestLoad_ValidConfig(t *testing.T) {
	// Write a minimal valid config to testdata
	src := filepath.Join("..", "..", "config.toml")
	cfg, err := config.Load(src)
	if err != nil {
		t.Fatalf("Load(%q) returned error: %v", src, err)
	}

	// LLM section
	if cfg.LLM.Model != "claude-sonnet-4-6-20260326" {
		t.Errorf("LLM.Model = %q, want %q", cfg.LLM.Model, "claude-sonnet-4-6-20260326")
	}
	if cfg.LLM.ExtractionModel != "claude-haiku-4-5-20251001" {
		t.Errorf("LLM.ExtractionModel = %q, want %q", cfg.LLM.ExtractionModel, "claude-haiku-4-5-20251001")
	}
	if cfg.LLM.APIKeyEnv != "ANTHROPIC_API_KEY" {
		t.Errorf("LLM.APIKeyEnv = %q, want %q", cfg.LLM.APIKeyEnv, "ANTHROPIC_API_KEY")
	}
	if cfg.LLM.MaxTokens != 4096 {
		t.Errorf("LLM.MaxTokens = %d, want 4096", cfg.LLM.MaxTokens)
	}
	if cfg.LLM.MaxToolRounds != 5 {
		t.Errorf("LLM.MaxToolRounds = %d, want 5", cfg.LLM.MaxToolRounds)
	}
	if cfg.LLM.MaxToolResultTokens != 500 {
		t.Errorf("LLM.MaxToolResultTokens = %d, want 500", cfg.LLM.MaxToolResultTokens)
	}

	// Adapters
	if !cfg.Adapters.CLI.Enabled {
		t.Error("Adapters.CLI.Enabled should be true")
	}
	if cfg.Adapters.Telegram.Enabled {
		t.Error("Adapters.Telegram.Enabled should be false")
	}
	if cfg.Adapters.Telegram.TokenEnv != "TELEGRAM_BOT_TOKEN" {
		t.Errorf("Adapters.Telegram.TokenEnv = %q, want %q", cfg.Adapters.Telegram.TokenEnv, "TELEGRAM_BOT_TOKEN")
	}
	if cfg.Adapters.Telegram.StreamingIntervalMS != 1000 {
		t.Errorf("Adapters.Telegram.StreamingIntervalMS = %d, want 1000", cfg.Adapters.Telegram.StreamingIntervalMS)
	}

	// Brain
	if cfg.Brain.Enabled {
		t.Error("Brain.Enabled should be false")
	}
	if cfg.Brain.MCPTransport != "stdio" {
		t.Errorf("Brain.MCPTransport = %q, want %q", cfg.Brain.MCPTransport, "stdio")
	}
	if cfg.Brain.GBrainCommand != "gbrain" {
		t.Errorf("Brain.GBrainCommand = %q, want %q", cfg.Brain.GBrainCommand, "gbrain")
	}
	if cfg.Brain.HealthCheckIntervalS != 30 {
		t.Errorf("Brain.HealthCheckIntervalS = %d, want 30", cfg.Brain.HealthCheckIntervalS)
	}
	if cfg.Brain.MaxRestartAttempts != 3 {
		t.Errorf("Brain.MaxRestartAttempts = %d, want 3", cfg.Brain.MaxRestartAttempts)
	}

	// Session
	if cfg.Session.StorageDir != "sessions" {
		t.Errorf("Session.StorageDir = %q, want %q", cfg.Session.StorageDir, "sessions")
	}
	if cfg.Session.MaxTurnsInContext != 50 {
		t.Errorf("Session.MaxTurnsInContext = %d, want 50", cfg.Session.MaxTurnsInContext)
	}
	if cfg.Session.MaxTurnsBeforeCompaction != 30 {
		t.Errorf("Session.MaxTurnsBeforeCompaction = %d, want 30", cfg.Session.MaxTurnsBeforeCompaction)
	}
	if cfg.Session.MaxFileSizeMB != 10 {
		t.Errorf("Session.MaxFileSizeMB = %d, want 10", cfg.Session.MaxFileSizeMB)
	}
	if cfg.Session.MaxContextTokens != 32000 {
		t.Errorf("Session.MaxContextTokens = %d, want 32000", cfg.Session.MaxContextTokens)
	}
	if cfg.Session.ColdResumeThresholdMinutes != 30 {
		t.Errorf("Session.ColdResumeThresholdMinutes = %d, want 30", cfg.Session.ColdResumeThresholdMinutes)
	}
	if !cfg.Session.LoadOnStartup {
		t.Error("Session.LoadOnStartup should be true")
	}

	// Skills and Identity
	if cfg.Skills.Dir != "skills" {
		t.Errorf("Skills.Dir = %q, want %q", cfg.Skills.Dir, "skills")
	}
	if cfg.Identity.Dir != "identity" {
		t.Errorf("Identity.Dir = %q, want %q", cfg.Identity.Dir, "identity")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := config.Load("/nonexistent/path/config.toml")
	if err == nil {
		t.Fatal("Load with missing file should return an error")
	}
}

func TestLoad_MissingRequiredFields(t *testing.T) {
	// Write a config missing LLM.Model
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")
	content := `
[llm]
extraction_model = "claude-haiku-4-5-20251001"
api_key_env = "ANTHROPIC_API_KEY"

[session]
storage_dir = "sessions"
max_turns_in_context = 50
max_turns_before_compaction = 30
max_file_size_mb = 10

[skills]
dir = "skills"

[identity]
dir = "identity"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("Load with missing LLM.Model should return a validation error")
	}
}

func TestLoad_TelegramMissingTokenEnvWhenEnabled(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")
	content := `
[llm]
model = "claude-sonnet-4-6-20260326"
extraction_model = "claude-haiku-4-5-20251001"
api_key_env = "ANTHROPIC_API_KEY"

[adapters.telegram]
enabled = true
# token_env intentionally missing

[session]
storage_dir = "sessions"
max_turns_in_context = 50
max_turns_before_compaction = 30
max_file_size_mb = 10

[skills]
dir = "skills"

[identity]
dir = "identity"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("Load with telegram enabled but missing token_env should return a validation error")
	}
}
