# AKB48 Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the core Go packages — types, config, tool registry, and LLM caller — that every other AKB48 component depends on.

**Architecture:** Thin, dependency-injected packages under `internal/`. No global state. The LLM caller wraps the Anthropic Go SDK, handles streaming via `chan string`, enforces a tool-loop round limit, and truncates oversized tool results. The tool registry is `sync.RWMutex`-protected and idempotency-keyed.

**Tech Stack:** Go 1.23+, `github.com/anthropics/anthropic-sdk-go`, `github.com/BurntSushi/toml`, `github.com/google/uuid`, `golang.org/x/sync/errgroup`

---

## Scope Note

This is Plan 1 of 3 for the Hello World milestone (PRDs 01–05):
- **Plan 1 (this plan):** Foundation — types, config, tool registry, LLM caller (PRD-01 core)
- **Plan 2:** State & Context — session management, skill resolver, context assembler (PRD-04, PRD-05)
- **Plan 3:** Integration — CLI/Telegram adapters, GBrain bridge, runtime wiring, end-to-end test (PRD-02, PRD-03)

Start Plan 2 only after all Plan 1 tests pass.

---

## File Map

| File | Responsibility |
|------|---------------|
| `go.mod` / `go.sum` | Module definition and dependency lock |
| `config.toml` | Example config (no secrets) |
| `internal/types/types.go` | Shared data types: Message, Response, ToolCall, ToolResult, TokenUsage, LLMMessage |
| `internal/config/config.go` | TOML loading, struct validation, defaults |
| `internal/config/config_test.go` | Config load tests |
| `internal/config/testdata/valid.toml` | Fixture for tests |
| `internal/config/testdata/missing_model.toml` | Fixture for validation failure test |
| `internal/tools/tools.go` | ToolRegistry: register, dispatch, idempotency, concurrent-safe |
| `internal/tools/tools_test.go` | Registry unit tests |
| `internal/llm/llm.go` | LLMCaller: SDK wrapper, tool loop, streaming, result truncation |
| `internal/llm/llm_test.go` | Truncation unit test + skipped integration tests |

---

### Task 1: Initialize Go module and project scaffold

**Files:**
- Create: `go.mod`
- Create: `config.toml`

- [ ] **Step 1: Create directory structure**

```bash
mkdir -p cmd/akb48 \
  internal/config/testdata \
  internal/types \
  internal/tools \
  internal/llm \
  internal/skills \
  internal/identity \
  internal/session \
  internal/brain \
  internal/runtime \
  adapters/cli \
  adapters/telegram \
  identity \
  skills/note-capture \
  skills/research \
  sessions \
  logs
```

- [ ] **Step 2: Initialize Go module**

```bash
go mod init github.com/dydanz/akb48
```

Expected: `go.mod` created with `module github.com/dydanz/akb48` and `go 1.23`.

- [ ] **Step 3: Add dependencies**

```bash
go get github.com/BurntSushi/toml@v1.4.0
go get github.com/anthropics/anthropic-sdk-go@latest
go get github.com/google/uuid@v1.6.0
go get golang.org/x/sync@latest
```

- [ ] **Step 4: Create example config.toml**

Write `config.toml`:

```toml
[llm]
model = "claude-sonnet-4-6-20260326"
extraction_model = "claude-haiku-4-5-20251001"
api_key_env = "ANTHROPIC_API_KEY"
max_tokens = 4096
max_tool_rounds = 5
max_tool_result_tokens = 500

[adapters.cli]
enabled = true

[adapters.telegram]
enabled = false
token_env = "TELEGRAM_BOT_TOKEN"
allowed_user_ids = []
streaming_interval_ms = 1000

[brain]
enabled = false
mcp_transport = "stdio"
gbrain_command = "gbrain"
gbrain_args = ["serve"]
health_check_interval_s = 30
max_restart_attempts = 3

[session]
storage_dir = "sessions"
max_turns_in_context = 50
max_turns_before_compaction = 30
max_file_size_mb = 10
max_context_tokens = 32000
cold_resume_threshold_minutes = 30
load_on_startup = true

[skills]
dir = "skills"

[identity]
dir = "identity"
```

- [ ] **Step 5: Verify module builds**

```bash
go build ./...
```

Expected: no output, exit code 0.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum config.toml
git commit -m "feat: initialize Go module and project directory structure"
```

---

### Task 2: Shared types

**Files:**
- Create: `internal/types/types.go`

- [ ] **Step 1: Write types**

```go
// internal/types/types.go
package types

import (
	"encoding/json"
	"time"
)

type TokenUsage struct {
	InputTokens  int64
	OutputTokens int64
	CacheRead    int64
	CacheWrite   int64
}

type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

type ToolResult struct {
	ToolCallID string
	Output     string
	IsError    bool
	DurationMs int64
}

type Message struct {
	SessionID string
	Text      string
	UserID    string
	Timestamp time.Time
}

type Response struct {
	Text       string
	TokenUsage TokenUsage
	LatencyMs  int64
}

// LLMMessage is a single turn in a conversation history, passed to the LLM caller.
type LLMMessage struct {
	Role    string // "user" or "assistant"
	Content string
}
```

- [ ] **Step 2: Verify build**

```bash
go build ./internal/types/...
```

Expected: no output, exit code 0.

- [ ] **Step 3: Commit**

```bash
git add internal/types/types.go
git commit -m "feat: shared types — Message, Response, ToolCall, ToolResult, TokenUsage, LLMMessage"
```

---

### Task 3: Config loader

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `internal/config/testdata/valid.toml`
- Create: `internal/config/testdata/missing_model.toml`

- [ ] **Step 1: Write test fixtures**

Write `internal/config/testdata/valid.toml`:

```toml
[llm]
model = "claude-sonnet-4-6-20260326"
extraction_model = "claude-haiku-4-5-20251001"
api_key_env = "ANTHROPIC_API_KEY"
max_tokens = 4096

[adapters.cli]
enabled = true

[adapters.telegram]
enabled = false

[brain]
enabled = false

[session]
storage_dir = "sessions"
max_turns_in_context = 50

[skills]
dir = "skills"

[identity]
dir = "identity"
```

Write `internal/config/testdata/missing_model.toml`:

```toml
[llm]
api_key_env = "ANTHROPIC_API_KEY"

[adapters.cli]
enabled = true

[brain]
enabled = false

[session]
storage_dir = "sessions"

[skills]
dir = "skills"

[identity]
dir = "identity"
```

- [ ] **Step 2: Write failing tests**

```go
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
	if cfg.LLM.MaxToolRounds != 5 {
		t.Errorf("got MaxToolRounds %d, want 5 (default)", cfg.LLM.MaxToolRounds)
	}
}

func TestLoad_MissingAPIKey(t *testing.T) {
	os.Unsetenv("ANTHROPIC_API_KEY")

	_, err := config.Load("testdata/valid.toml")
	if err == nil {
		t.Fatal("expected error for missing API key, got nil")
	}
}

func TestLoad_MissingModel(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	_, err := config.Load("testdata/missing_model.toml")
	if err == nil {
		t.Fatal("expected error for missing model, got nil")
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
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./internal/config/... -v
```

Expected: FAIL — `cannot find package "github.com/dydanz/akb48/internal/config"`

- [ ] **Step 4: Write config implementation**

```go
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

type AdaptersConfig struct {
	CLI      CLIConfig      `toml:"cli"`
	Telegram TelegramConfig `toml:"telegram"`
}

type BrainConfig struct {
	Enabled              bool     `toml:"enabled"`
	MCPTransport         string   `toml:"mcp_transport"`
	GBrainCommand        string   `toml:"gbrain_command"`
	GBrainArgs           []string `toml:"gbrain_args"`
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
	if c.Brain.HealthCheckIntervalS == 0 {
		c.Brain.HealthCheckIntervalS = 30
	}
	if c.Brain.MaxRestartAttempts == 0 {
		c.Brain.MaxRestartAttempts = 3
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

func (c *Config) AnthropicAPIKey() string {
	return os.Getenv(c.LLM.APIKeyEnv)
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/config/... -v
```

Expected:
```
=== RUN   TestLoad_ValidConfig
--- PASS: TestLoad_ValidConfig (0.00s)
=== RUN   TestLoad_MissingAPIKey
--- PASS: TestLoad_MissingAPIKey (0.00s)
=== RUN   TestLoad_MissingModel
--- PASS: TestLoad_MissingModel (0.00s)
=== RUN   TestLoad_DefaultsApplied
--- PASS: TestLoad_DefaultsApplied (0.00s)
PASS
```

- [ ] **Step 6: Commit**

```bash
git add internal/config/
git commit -m "feat: config loader with TOML parsing, defaults, and validation"
```

---

### Task 4: Tool Registry

**Files:**
- Create: `internal/tools/tools.go`
- Create: `internal/tools/tools_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/tools/tools_test.go
package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dydanz/akb48/internal/tools"
	"github.com/dydanz/akb48/internal/types"
)

func TestRegistry_ExecuteKnownTool(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(tools.ToolDefinition{
		Name:        "echo",
		Description: "echoes input text",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
	}, func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct{ Text string }
		json.Unmarshal(input, &p)
		return p.Text, nil
	})

	call := types.ToolCall{
		ID:    "call-1",
		Name:  "echo",
		Input: json.RawMessage(`{"text":"hello"}`),
	}
	result := reg.Execute(context.Background(), call, "idem-1")

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Output)
	}
	if result.Output != "hello" {
		t.Errorf("got %q, want %q", result.Output, "hello")
	}
}

func TestRegistry_ExecuteUnknownTool(t *testing.T) {
	reg := tools.NewRegistry()
	call := types.ToolCall{ID: "call-1", Name: "nonexistent"}
	result := reg.Execute(context.Background(), call, "idem-2")

	if !result.IsError {
		t.Fatal("expected IsError=true for unknown tool")
	}
}

func TestRegistry_IdempotencyPreventsDoubleExecution(t *testing.T) {
	reg := tools.NewRegistry()
	callCount := 0
	reg.Register(tools.ToolDefinition{Name: "counter"}, func(ctx context.Context, input json.RawMessage) (string, error) {
		callCount++
		return "done", nil
	})

	call := types.ToolCall{ID: "call-1", Name: "counter"}
	reg.Execute(context.Background(), call, "same-key")
	reg.Execute(context.Background(), call, "same-key")

	if callCount != 1 {
		t.Errorf("got callCount=%d, want 1 (idempotency should prevent second execution)", callCount)
	}
}

func TestRegistry_Definitions(t *testing.T) {
	reg := tools.NewRegistry()
	noop := func(_ context.Context, _ json.RawMessage) (string, error) { return "", nil }
	reg.Register(tools.ToolDefinition{Name: "a"}, noop)
	reg.Register(tools.ToolDefinition{Name: "b"}, noop)

	defs := reg.Definitions()
	if len(defs) != 2 {
		t.Errorf("got %d definitions, want 2", len(defs))
	}
}

func TestRegistry_DurationRecorded(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(tools.ToolDefinition{Name: "fast"}, func(_ context.Context, _ json.RawMessage) (string, error) {
		return "ok", nil
	})

	call := types.ToolCall{ID: "c1", Name: "fast"}
	result := reg.Execute(context.Background(), call, "idem-dur")

	if result.DurationMs < 0 {
		t.Errorf("expected non-negative DurationMs, got %d", result.DurationMs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tools/... -v
```

Expected: FAIL — `cannot find package "github.com/dydanz/akb48/internal/tools"`

- [ ] **Step 3: Write tool registry implementation**

```go
// internal/tools/tools.go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/dydanz/akb48/internal/types"
)

type ToolHandler func(ctx context.Context, input json.RawMessage) (string, error)

type ToolDefinition struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

type Registry struct {
	mu          sync.RWMutex
	handlers    map[string]ToolHandler
	definitions map[string]ToolDefinition
	idempotency sync.Map // map[string]string: idempotency_key -> output
}

func NewRegistry() *Registry {
	return &Registry{
		handlers:    make(map[string]ToolHandler),
		definitions: make(map[string]ToolDefinition),
	}
}

func (r *Registry) Register(def ToolDefinition, handler ToolHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[def.Name] = handler
	r.definitions[def.Name] = def
}

func (r *Registry) Definitions() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]ToolDefinition, 0, len(r.definitions))
	for _, d := range r.definitions {
		defs = append(defs, d)
	}
	return defs
}

func (r *Registry) Execute(ctx context.Context, call types.ToolCall, idempotencyKey string) types.ToolResult {
	if idempotencyKey == "" {
		idempotencyKey = uuid.New().String()
	}

	if cached, ok := r.idempotency.Load(idempotencyKey); ok {
		return types.ToolResult{ToolCallID: call.ID, Output: cached.(string)}
	}

	r.mu.RLock()
	handler, ok := r.handlers[call.Name]
	r.mu.RUnlock()

	if !ok {
		return types.ToolResult{
			ToolCallID: call.ID,
			Output:     fmt.Sprintf("unknown tool: %s", call.Name),
			IsError:    true,
		}
	}

	start := time.Now()
	output, err := handler(ctx, call.Input)
	dur := time.Since(start).Milliseconds()

	if err != nil {
		return types.ToolResult{
			ToolCallID: call.ID,
			Output:     err.Error(),
			IsError:    true,
			DurationMs: dur,
		}
	}

	r.idempotency.Store(idempotencyKey, output)
	return types.ToolResult{
		ToolCallID: call.ID,
		Output:     output,
		DurationMs: dur,
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/tools/... -v
```

Expected:
```
=== RUN   TestRegistry_ExecuteKnownTool
--- PASS: TestRegistry_ExecuteKnownTool (0.00s)
=== RUN   TestRegistry_ExecuteUnknownTool
--- PASS: TestRegistry_ExecuteUnknownTool (0.00s)
=== RUN   TestRegistry_IdempotencyPreventsDoubleExecution
--- PASS: TestRegistry_IdempotencyPreventsDoubleExecution (0.00s)
=== RUN   TestRegistry_Definitions
--- PASS: TestRegistry_Definitions (0.00s)
=== RUN   TestRegistry_DurationRecorded
--- PASS: TestRegistry_DurationRecorded (0.00s)
PASS
```

- [ ] **Step 5: Commit**

```bash
git add internal/tools/
git commit -m "feat: tool registry with idempotency and concurrent-safe dispatch"
```

---

### Task 5: LLM Caller

**Files:**
- Create: `internal/llm/llm.go`
- Create: `internal/llm/llm_test.go`

The LLM caller is the most complex package. It wraps the Anthropic Go SDK, manages the tool-use loop, streams tokens over a channel, and truncates oversized tool results. Integration tests that hit the real API are skipped by default — run them manually with a real `ANTHROPIC_API_KEY`.

- [ ] **Step 1: Write failing tests**

```go
// internal/llm/llm_test.go
package llm_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dydanz/akb48/internal/config"
	"github.com/dydanz/akb48/internal/llm"
	"github.com/dydanz/akb48/internal/tools"
	"github.com/dydanz/akb48/internal/types"
)

func TestTruncateToolResult_ShortInput(t *testing.T) {
	out := llm.TruncateToolResult("hello", 500)
	if out != "hello" {
		t.Errorf("short input should be unchanged, got %q", out)
	}
}

func TestTruncateToolResult_LongInput(t *testing.T) {
	long := strings.Repeat("a", 3000)
	out := llm.TruncateToolResult(long, 500)
	// 500 tokens * 4 chars/token = 2000 chars max, plus " [truncated]"
	if len(out) > 2000+len(" [truncated]") {
		t.Errorf("result not truncated: len=%d", len(out))
	}
	if !strings.HasSuffix(out, " [truncated]") {
		t.Errorf("expected [truncated] suffix, got: %q", out[len(out)-20:])
	}
}

func TestCall_ReturnsText(t *testing.T) {
	t.Skip("integration: requires ANTHROPIC_API_KEY")

	t.Setenv("ANTHROPIC_API_KEY", "")
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Model:               "claude-haiku-4-5-20251001",
			MaxTokens:           256,
			MaxToolRounds:       3,
			MaxToolResultTokens: 500,
		},
	}
	reg := tools.NewRegistry()
	caller := llm.NewCaller(cfg, reg)

	result, err := caller.Call(context.Background(), llm.CallParams{
		System:   "Reply with exactly the word: pong",
		Messages: []types.LLMMessage{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text == "" {
		t.Error("expected non-empty response text")
	}
}

func TestCall_ToolLoop(t *testing.T) {
	t.Skip("integration: requires ANTHROPIC_API_KEY")

	cfg := &config.Config{
		LLM: config.LLMConfig{
			Model:               "claude-haiku-4-5-20251001",
			MaxTokens:           256,
			MaxToolRounds:       3,
			MaxToolResultTokens: 500,
		},
	}
	reg := tools.NewRegistry()
	reg.Register(tools.ToolDefinition{
		Name:        "get_time",
		Description: "Returns the current time as an ISO 8601 string",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	}, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "2026-05-01T10:00:00Z", nil
	})

	caller := llm.NewCaller(cfg, reg)
	result, err := caller.Call(context.Background(), llm.CallParams{
		System:   "Use the get_time tool to answer questions about the current time.",
		Messages: []types.LLMMessage{{Role: "user", Content: "What time is it right now?"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text == "" {
		t.Error("expected non-empty response after tool loop")
	}
}
```

- [ ] **Step 2: Run truncation tests to verify they fail**

```bash
go test ./internal/llm/... -v -run TestTruncate
```

Expected: FAIL — `cannot find package "github.com/dydanz/akb48/internal/llm"`

- [ ] **Step 3: Write LLM caller implementation**

```go
// internal/llm/llm.go
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/dydanz/akb48/internal/config"
	"github.com/dydanz/akb48/internal/tools"
	"github.com/dydanz/akb48/internal/types"
)

// CallParams holds everything needed to make a single LLM call.
type CallParams struct {
	System   string
	Messages []types.LLMMessage
	// Tokens, if non-nil, receives streamed text tokens as they arrive.
	// Caller must drain or close the channel; Call blocks until streaming completes.
	Tokens chan<- string
}

// CallResult is the final output of a completed LLM call (possibly after tool loops).
type CallResult struct {
	Text       string
	TokenUsage types.TokenUsage
	LatencyMs  int64
}

// Caller wraps the Anthropic SDK and handles streaming, tool loops, and cost guards.
type Caller struct {
	client   *anthropic.Client
	cfg      config.LLMConfig
	registry *tools.Registry
}

func NewCaller(cfg *config.Config, registry *tools.Registry) *Caller {
	return &Caller{
		client:   anthropic.NewClient(),
		cfg:      cfg.LLM,
		registry: registry,
	}
}

// Call runs the LLM call loop: send → execute tools → repeat until end_turn or max rounds.
func (c *Caller) Call(ctx context.Context, params CallParams) (*CallResult, error) {
	messages := toAPIMessages(params.Messages)
	var totalUsage types.TokenUsage
	start := time.Now()

	for round := 0; round < c.cfg.MaxToolRounds; round++ {
		apiParams := c.buildParams(params.System, messages)

		var msg *anthropic.Message
		var err error

		if params.Tokens != nil {
			msg, err = c.streamCall(ctx, apiParams, params.Tokens)
		} else {
			msg, err = c.client.Messages.New(ctx, apiParams)
		}
		if err != nil {
			return nil, fmt.Errorf("anthropic API (round %d): %w", round, err)
		}

		totalUsage.InputTokens += msg.Usage.InputTokens
		totalUsage.OutputTokens += msg.Usage.OutputTokens
		totalUsage.CacheRead += msg.Usage.CacheReadInputTokens
		totalUsage.CacheWrite += msg.Usage.CacheCreationInputTokens

		switch msg.StopReason {
		case anthropic.StopReasonEndTurn:
			return &CallResult{
				Text:       extractText(msg),
				TokenUsage: totalUsage,
				LatencyMs:  time.Since(start).Milliseconds(),
			}, nil

		case anthropic.StopReasonToolUse:
			toolCalls := extractToolCalls(msg)
			// Append assistant turn to history.
			messages = append(messages, assistantMsgParam(msg.Content))
			// Execute each tool and collect results.
			var resultBlocks []anthropic.ContentBlockParamUnion
			for _, tc := range toolCalls {
				result := c.registry.Execute(ctx, tc, "")
				output := TruncateToolResult(result.Output, c.cfg.MaxToolResultTokens)
				resultBlocks = append(resultBlocks, anthropic.NewToolResultBlock(tc.ID, output, result.IsError))
			}
			messages = append(messages, anthropic.NewUserMessage(resultBlocks...))

		default:
			// Unexpected stop reason (e.g. max_tokens): return whatever text we have.
			return &CallResult{
				Text:       extractText(msg),
				TokenUsage: totalUsage,
				LatencyMs:  time.Since(start).Milliseconds(),
			}, nil
		}
	}

	return nil, fmt.Errorf("max tool rounds (%d) exceeded without end_turn", c.cfg.MaxToolRounds)
}

func (c *Caller) streamCall(ctx context.Context, params anthropic.MessageNewParams, tokens chan<- string) (*anthropic.Message, error) {
	stream := c.client.Messages.NewStreaming(ctx, params)
	var accumulated anthropic.Message
	for stream.Next() {
		event := stream.Current()
		if err := accumulated.Accumulate(event); err != nil {
			return nil, fmt.Errorf("accumulate stream event: %w", err)
		}
		if ev, ok := event.AsUnion().(anthropic.ContentBlockDeltaEvent); ok {
			if delta, ok := ev.Delta.AsUnion().(anthropic.TextDelta); ok && tokens != nil {
				select {
				case tokens <- delta.Text:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("stream error: %w", err)
	}
	return &accumulated, nil
}

func (c *Caller) buildParams(system string, messages []anthropic.MessageParam) anthropic.MessageNewParams {
	params := anthropic.MessageNewParams{
		Model:     anthropic.F(anthropic.Model(c.cfg.Model)),
		MaxTokens: anthropic.F(int64(c.cfg.MaxTokens)),
		System: anthropic.F([]anthropic.TextBlockParam{
			anthropic.NewTextBlock(system),
		}),
		Messages: anthropic.F(messages),
	}

	if defs := c.registry.Definitions(); len(defs) > 0 {
		apiTools := make([]anthropic.ToolParam, 0, len(defs))
		for _, d := range defs {
			var props interface{}
			if d.InputSchema != nil {
				var schema map[string]interface{}
				json.Unmarshal(d.InputSchema, &schema)
				props = schema["properties"]
			}
			apiTools = append(apiTools, anthropic.ToolParam{
				Name:        anthropic.F(d.Name),
				Description: anthropic.F(d.Description),
				InputSchema: anthropic.F(anthropic.ToolInputSchemaParam{
					Type:       anthropic.F(anthropic.ToolInputSchemaTypeObject),
					Properties: anthropic.F(props),
				}),
			})
		}
		params.Tools = anthropic.F(apiTools)
	}

	return params
}

// toAPIMessages converts the simple LLMMessage slice to Anthropic SDK message params.
func toAPIMessages(msgs []types.LLMMessage) []anthropic.MessageParam {
	out := make([]anthropic.MessageParam, len(msgs))
	for i, m := range msgs {
		if m.Role == "user" {
			out[i] = anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content))
		} else {
			out[i] = anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Content))
		}
	}
	return out
}

// assistantMsgParam converts the API response content blocks back into a message param
// so it can be appended to the conversation history for the next round.
func assistantMsgParam(content []anthropic.ContentBlock) anthropic.MessageParam {
	blocks := make([]anthropic.ContentBlockParamUnion, 0, len(content))
	for _, block := range content {
		switch b := block.AsUnion().(type) {
		case anthropic.TextBlock:
			blocks = append(blocks, anthropic.NewTextBlock(b.Text))
		case anthropic.ToolUseBlock:
			inputJSON, _ := json.Marshal(b.Input)
			blocks = append(blocks, anthropic.ToolUseBlockParam{
				Type:  anthropic.F(anthropic.ToolUseBlockParamType("tool_use")),
				ID:    anthropic.F(b.ID),
				Name:  anthropic.F(b.Name),
				Input: anthropic.Raw[interface{}](inputJSON),
			})
		}
	}
	return anthropic.NewAssistantMessage(blocks...)
}

func extractText(msg *anthropic.Message) string {
	for _, block := range msg.Content {
		if tb, ok := block.AsUnion().(anthropic.TextBlock); ok {
			return tb.Text
		}
	}
	return ""
}

func extractToolCalls(msg *anthropic.Message) []types.ToolCall {
	var calls []types.ToolCall
	for _, block := range msg.Content {
		if tub, ok := block.AsUnion().(anthropic.ToolUseBlock); ok {
			inputJSON, _ := json.Marshal(tub.Input)
			calls = append(calls, types.ToolCall{
				ID:    tub.ID,
				Name:  tub.Name,
				Input: inputJSON,
			})
		}
	}
	return calls
}

// TruncateToolResult caps tool output at maxTokens*4 characters and appends [truncated] if cut.
// Exported so it can be tested without a real API client.
func TruncateToolResult(output string, maxTokens int) string {
	limit := maxTokens * 4
	if len(output) <= limit {
		return output
	}
	return output[:limit] + " [truncated]"
}
```

**SDK type note:** If `anthropic.ToolUseBlockParamType` or `anthropic.Raw[interface{}]` do not compile with the installed SDK version, run `go doc github.com/anthropics/anthropic-sdk-go ToolUseBlockParam` and adjust the field types to match. The logic (preserve ID, name, input for the next round) is correct regardless of exact types.

- [ ] **Step 4: Run truncation tests to verify they pass**

```bash
go test ./internal/llm/... -v -run TestTruncate
```

Expected:
```
=== RUN   TestTruncateToolResult_ShortInput
--- PASS: TestTruncateToolResult_ShortInput (0.00s)
=== RUN   TestTruncateToolResult_LongInput
--- PASS: TestTruncateToolResult_LongInput (0.00s)
PASS
```

- [ ] **Step 5: Verify full build compiles**

```bash
go build ./...
```

Expected: no output, exit code 0. If there are SDK type errors in `assistantMsgParam`, run `go doc github.com/anthropics/anthropic-sdk-go ToolUseBlockParam` and fix field names.

- [ ] **Step 6: (Optional) Run integration tests against real API**

Remove the `t.Skip(...)` lines from `TestCall_ReturnsText` and `TestCall_ToolLoop`, then:

```bash
ANTHROPIC_API_KEY=sk-ant-... go test ./internal/llm/... -v -run TestCall -count=1
```

Expected: both tests pass. `TestCall_ToolLoop` should return a response that mentions the time "2026-05-01".

- [ ] **Step 7: Commit**

```bash
git add internal/llm/ internal/types/
git commit -m "feat: LLM caller with streaming, tool loop (max 5 rounds), and result truncation"
```

---

### Task 6: Validate foundation and run all tests

**Files:** None (validation only)

- [ ] **Step 1: Run all tests**

```bash
go test ./... -v
```

Expected: all non-skipped tests PASS. Skipped integration tests show `--- SKIP`. No FAIL lines.

- [ ] **Step 2: Run vet**

```bash
go vet ./...
```

Expected: no output, exit code 0.

- [ ] **Step 3: Commit clean state**

```bash
git add .
git commit -m "chore: foundation complete — config, types, tools, LLM caller all green"
```

---

## Self-Review

**Spec coverage:**
- PRD-01: config struct ✓, LLM caller ✓, tool registry ✓, types ✓, idempotency ✓, max_tool_rounds ✓, result truncation ✓
- Entry point (`cmd/akb48/main.go`) and runtime (`internal/runtime/`) — intentionally deferred to Plan 3 (needs adapters and session to be meaningful)
- Streaming (`chan string`) — implemented in `streamCall` ✓

**Placeholder scan:** No TBDs or TODOs in task steps. The SDK type note in Task 5 Step 3 is advisory (not a placeholder — the logic is complete and the note tells the engineer exactly what to check if compilation fails).

**Type consistency:**
- `types.LLMMessage` — defined in Task 2, used in Task 5 `CallParams.Messages` ✓
- `types.ToolCall` — defined in Task 2, used in Task 4 `Execute()` and Task 5 `extractToolCalls` ✓
- `types.ToolResult` — defined in Task 2, used in Task 4 `Execute()` return and Task 5 tool loop ✓
- `tools.ToolDefinition` — defined in Task 4, used in Task 5 `buildParams` ✓
- `llm.TruncateToolResult` — defined in Task 5, tested in Task 5 ✓
- `config.LLMConfig.MaxToolRounds` — used in Task 5 `Call()` loop bound ✓
- `config.LLMConfig.MaxToolResultTokens` — used in Task 5 `TruncateToolResult` call ✓
