// Package config loads and validates Klawmbing configuration from a TOML file.
package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// Config is the top-level Klawmbing configuration.
type Config struct {
	LLM      LLMConfig      `toml:"llm"`
	Adapters AdaptersConfig `toml:"adapters"`
	Brain    BrainConfig    `toml:"brain"`
	Session  SessionConfig  `toml:"session"`
	Skills   SkillsConfig   `toml:"skills"`
	Identity IdentityConfig `toml:"identity"`
}

// LLMConfig holds model and API configuration.
type LLMConfig struct {
	Model               string `toml:"model"`
	ExtractionModel     string `toml:"extraction_model"`
	APIKeyEnv           string `toml:"api_key_env"`
	MaxTokens           int    `toml:"max_tokens"`
	MaxToolRounds       int    `toml:"max_tool_rounds"`
	MaxToolResultTokens int    `toml:"max_tool_result_tokens"`
}

// AdaptersConfig holds per-adapter settings.
type AdaptersConfig struct {
	CLI      CLIAdapterConfig      `toml:"cli"`
	Telegram TelegramAdapterConfig `toml:"telegram"`
}

// CLIAdapterConfig holds CLI adapter settings.
type CLIAdapterConfig struct {
	Enabled bool `toml:"enabled"`
}

// TelegramAdapterConfig holds Telegram adapter settings.
type TelegramAdapterConfig struct {
	Enabled             bool    `toml:"enabled"`
	TokenEnv            string  `toml:"token_env"`
	AllowedUserIDs      []int64 `toml:"allowed_user_ids"`
	StreamingIntervalMS int     `toml:"streaming_interval_ms"`
}

// BrainConfig holds GBrain/MCP settings.
type BrainConfig struct {
	Enabled              bool     `toml:"enabled"`
	MCPTransport         string   `toml:"mcp_transport"`
	GBrainCommand        string   `toml:"gbrain_command"`
	GBrainArgs           []string `toml:"gbrain_args"`
	HealthCheckIntervalS int      `toml:"health_check_interval_s"`
	MaxRestartAttempts   int      `toml:"max_restart_attempts"`
}

// SessionConfig holds session persistence settings.
type SessionConfig struct {
	StorageDir                 string `toml:"storage_dir"`
	MaxTurnsInContext          int    `toml:"max_turns_in_context"`
	MaxTurnsBeforeCompaction   int    `toml:"max_turns_before_compaction"`
	MaxFileSizeMB              int    `toml:"max_file_size_mb"`
	MaxContextTokens           int    `toml:"max_context_tokens"`
	ColdResumeThresholdMinutes int    `toml:"cold_resume_threshold_minutes"`
	LoadOnStartup              bool   `toml:"load_on_startup"`
}

// SkillsConfig holds skill system settings.
type SkillsConfig struct {
	Dir string `toml:"dir"`
}

// IdentityConfig holds identity file settings.
type IdentityConfig struct {
	Dir string `toml:"dir"`
}

// Load reads the TOML config at path, decodes it, and validates required fields.
// It returns an error if the file cannot be read, decoded, or fails validation.
func Load(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("config: decode %q: %w", path, err)
	}
	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("config: validation failed: %w", err)
	}
	return &cfg, nil
}

// validate checks required fields and cross-field constraints.
func validate(cfg *Config) error {
	if cfg.LLM.Model == "" {
		return fmt.Errorf("llm.model is required")
	}
	if cfg.LLM.ExtractionModel == "" {
		return fmt.Errorf("llm.extraction_model is required")
	}
	if cfg.LLM.APIKeyEnv == "" {
		return fmt.Errorf("llm.api_key_env is required")
	}
	if cfg.Session.StorageDir == "" {
		return fmt.Errorf("session.storage_dir is required")
	}
	if cfg.Session.MaxTurnsInContext <= 0 {
		return fmt.Errorf("session.max_turns_in_context must be > 0")
	}
	if cfg.Session.MaxTurnsBeforeCompaction <= 0 {
		return fmt.Errorf("session.max_turns_before_compaction must be > 0")
	}
	if cfg.Session.MaxFileSizeMB <= 0 {
		return fmt.Errorf("session.max_file_size_mb must be > 0")
	}
	if cfg.Skills.Dir == "" {
		return fmt.Errorf("skills.dir is required")
	}
	if cfg.Identity.Dir == "" {
		return fmt.Errorf("identity.dir is required")
	}
	if cfg.Adapters.Telegram.Enabled && cfg.Adapters.Telegram.TokenEnv == "" {
		return fmt.Errorf("adapters.telegram.token_env is required when telegram is enabled")
	}
	return nil
}
