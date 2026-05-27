// internal/config/config.go
package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type LLMConfig struct {
	Model               string `toml:"model"`
	ExtractionModel     string `toml:"extraction_model"`
	APIKeyEnv           string `toml:"api_key_env"`
	MaxTokens           int    `toml:"max_tokens"`
	MaxToolRounds       int    `toml:"max_tool_rounds"`
	MaxToolResultTokens int    `toml:"max_tool_result_tokens"`
}

type CLIConfig struct {
	Enabled bool `toml:"enabled"`
}

type TelegramConfig struct {
	Enabled             bool    `toml:"enabled"`
	TokenEnv            string  `toml:"token_env"`
	AllowedUserIDs      []int64 `toml:"allowed_user_ids"`
	StreamingIntervalMs int     `toml:"streaming_interval_ms"`
}

type DiscordConfig struct {
	Enabled             bool     `toml:"enabled"`
	TokenEnv            string   `toml:"token_env"`
	AllowedUserIDs      []string `toml:"allowed_user_ids"`
	StreamingIntervalMs int      `toml:"streaming_interval_ms"`
	SlashCommands       bool     `toml:"slash_commands"`
	SlashCommandGuildID string   `toml:"slash_command_guild_id"`
	MentionResponse     bool     `toml:"mention_response"`
}

type AdaptersConfig struct {
	CLI      CLIConfig      `toml:"cli"`
	Telegram TelegramConfig `toml:"telegram"`
	Discord  DiscordConfig  `toml:"discord"`
}

type BrainConfig struct {
	Enabled              bool     `toml:"enabled"`
	MCPTransport         string   `toml:"mcp_transport"`
	GBrainCommand        string   `toml:"gbrain_command"`
	GBrainArgs           []string `toml:"gbrain_args"`
	GBrainWorkingDir     string   `toml:"gbrain_working_dir"`
	ToolPrefix           string   `toml:"tool_prefix"`
	HealthCheckIntervalS int      `toml:"health_check_interval_s"`
	MaxRestartAttempts   int      `toml:"max_restart_attempts"`
}

type SessionConfig struct {
	StorageDir                 string `toml:"storage_dir"`
	MaxTurnsInContext          int    `toml:"max_turns_in_context"`
	MaxTurnsBeforeCompaction   int    `toml:"max_turns_before_compaction"`
	MaxFileSizeMB              int    `toml:"max_file_size_mb"`
	MaxContextTokens           int    `toml:"max_context_tokens"`
	ColdResumeThresholdMinutes int    `toml:"cold_resume_threshold_minutes"`
	LoadOnStartup              bool   `toml:"load_on_startup"`
}

type SkillsConfig struct {
	Dir string `toml:"dir"`
}

type IdentityConfig struct {
	Dir string `toml:"dir"`
}

type Config struct {
	LLM      LLMConfig      `toml:"llm"`
	Adapters AdaptersConfig `toml:"adapters"`
	Brain    BrainConfig    `toml:"brain"`
	Session  SessionConfig  `toml:"session"`
	Skills   SkillsConfig   `toml:"skills"`
	Identity IdentityConfig `toml:"identity"`
}

func Load(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.LLM.MaxToolRounds == 0 {
		c.LLM.MaxToolRounds = 5
	}
	if c.LLM.MaxToolResultTokens == 0 {
		c.LLM.MaxToolResultTokens = 500
	}
	if c.LLM.MaxTokens == 0 {
		c.LLM.MaxTokens = 4096
	}
	if c.Session.MaxTurnsInContext == 0 {
		c.Session.MaxTurnsInContext = 50
	}
	if c.Session.MaxContextTokens == 0 {
		c.Session.MaxContextTokens = 32000
	}
	if c.Session.ColdResumeThresholdMinutes == 0 {
		c.Session.ColdResumeThresholdMinutes = 30
	}
	if c.Adapters.Telegram.StreamingIntervalMs == 0 {
		c.Adapters.Telegram.StreamingIntervalMs = 1000
	}
	if c.Adapters.Telegram.TokenEnv == "" {
		c.Adapters.Telegram.TokenEnv = "TELEGRAM_BOT_TOKEN"
	}
	if c.Adapters.Discord.TokenEnv == "" {
		c.Adapters.Discord.TokenEnv = "DISCORD_BOT_TOKEN"
	}
	if c.Adapters.Discord.StreamingIntervalMs == 0 {
		c.Adapters.Discord.StreamingIntervalMs = 1000
	}
	if c.Brain.HealthCheckIntervalS == 0 {
		c.Brain.HealthCheckIntervalS = 30
	}
	if c.Brain.MaxRestartAttempts == 0 {
		c.Brain.MaxRestartAttempts = 3
	}
	if c.Brain.ToolPrefix == "" {
		c.Brain.ToolPrefix = "gbrain"
	}
	if c.Brain.GBrainCommand == "" {
		c.Brain.GBrainCommand = "gbrain"
	}
	if len(c.Brain.GBrainArgs) == 0 {
		c.Brain.GBrainArgs = []string{"serve"}
	}
	if c.Identity.Dir == "" {
		c.Identity.Dir = "identity"
	}
	if c.Skills.Dir == "" {
		c.Skills.Dir = "skills"
	}
	if c.Session.StorageDir == "" {
		c.Session.StorageDir = "sessions"
	}
}

func (c *Config) validate() error {
	if c.LLM.Model == "" {
		return fmt.Errorf("llm.model is required")
	}
	if c.LLM.APIKeyEnv == "" {
		return fmt.Errorf("llm.api_key_env is required")
	}
	if os.Getenv(c.LLM.APIKeyEnv) == "" {
		return fmt.Errorf("env var %q (llm.api_key_env) is not set", c.LLM.APIKeyEnv)
	}
	return nil
}

// AnthropicAPIKey reads the API key from the configured environment variable.
func (c *Config) AnthropicAPIKey() string {
	return os.Getenv(c.LLM.APIKeyEnv)
}
