# PRD-01: Core Runtime & Message Loop

**Status:** Draft v1.0
**Parent:** PRD-00 (Klawmbing Master PRD)
**Author:** Dandi
**Created:** April 26, 2026
**Dependencies:** None (this is the foundation)
**Estimated Effort:** 2-3 days

---

## 1. Problem

Klawmbing needs a core process that starts up, loads configuration, accepts messages from any channel adapter, sends them to the Claude API, executes tool calls in the response, and returns results. Without this core loop, nothing else works.

This PRD covers the minimum viable runtime: the entry point, configuration loader, LLM caller with streaming, tool execution dispatcher, and the interface contracts that all other components (adapters, skills, sessions) plug into.

---

## 2. Goals

- **G1:** A single `./klawmbing` binary (built with `go build -o klawmbing ./cmd/klawmbing/`) starts the entire runtime
- **G2:** The runtime loads configuration from a TOML file (API keys, model config, adapter toggles)
- **G3:** The LLM caller sends messages to the Claude API and streams tokens back via a channel
- **G4:** Tool calls in the LLM response are dispatched to registered tool handlers
- **G5:** The runtime defines clean interface contracts for adapters, tools, and post-turn hooks
- **G6:** Errors in any component do not crash the main process

## 3. Non-Goals

- Specific adapter implementations (PRD-02)
- GBrain/MCP connection (PRD-03)
- Skill resolution and context assembly (PRD-04)
- Session persistence (PRD-05)
- Cron scheduling (Phase 1, Week 4)
- Multi-process / multi-worker architecture

---

## 4. Actors

| Actor | Role in this PRD |
|-------|-----------------|
| Operator | Starts the runtime, provides config, sends test messages via CLI adapter |
| Klawmbing Runtime | Loads config, initializes components, runs the message loop |
| Claude API | Receives prompts, returns streaming responses with optional tool calls |
| Tool Handlers | Execute side effects (gbrain, shell, web search) when the LLM requests them |

---

## 5. User Stories

| ID | Story | Acceptance Criteria |
|----|-------|-------------------|
| US-R01 | As an operator, I run `./klawmbing` and the process starts without errors | Process starts, logs "Klawmbing started" with loaded config summary (model, adapters enabled) |
| US-R02 | As an operator, I provide a config.toml with my Anthropic API key and model preferences | Runtime reads config, validates required fields, fails fast with clear error if API key is missing |
| US-R03 | As an operator, I send a message through any adapter and receive a streamed response | First token arrives in < 2s, full response streams to the adapter's `Send()` method |
| US-R04 | As an operator, if the LLM returns a tool_use block, the registered handler is called | Tool handler receives the tool name + input, returns result, and the LLM continues with the tool result |
| US-R05 | As an operator, if a tool handler returns an error, the error is caught and reported gracefully | Error message returned to chat, process continues running, error logged to tool-calls.jsonl |
| US-R06 | As an operator, I can see all LLM calls and tool invocations in a log file | tool-calls.jsonl contains: timestamp, session_id, message_id, tool name, input, output, tokens used, latency |

---

## 6. Technical Design

### 6.1 Directory Structure (this PRD's scope)

```
klawmbing/
├── cmd/klawmbing/main.go       # Entry point: parse flags, load config, start runtime
├── internal/
│   ├── config/config.go        # Config struct + TOML loader + validation
│   ├── runtime/runtime.go      # KlawmbingRuntime — wires all components, message loop
│   ├── llm/llm.go              # LLMCaller — SDK wrapper, streaming, tool loop
│   ├── tools/tools.go          # ToolRegistry — register/dispatch/log
│   └── types/types.go          # Shared types: Message, Response, ToolCall, ToolResult, TokenUsage
├── adapters/
│   ├── adapter.go              # ChannelAdapter interface
│   ├── cli/cli.go
│   └── telegram/telegram.go
├── skills/                     # SKILL.md files (populated by PRD-04)
├── identity/                   # AGENTS.md, SOUL.md, USER.md (populated by PRD-04)
├── sessions/                   # JSONL session files (populated by PRD-05)
├── logs/
│   └── tool-calls.jsonl        # Audit trail for every LLM call and tool invocation
├── go.mod
├── go.sum
└── config.toml
```

### 6.2 Configuration Schema (config.toml)

```toml
[klawmbing]
name = "Klawmbing"
log_level = "INFO"                    # DEBUG | INFO | WARN | ERROR

[llm]
model = "claude-sonnet-4-6-20260326"          # Primary model
extraction_model = "claude-haiku-4-5-20251001" # Cheap model for extraction
max_tokens = 8192
max_tool_rounds = 10                          # Limit tool-use loop iterations
api_key_env = "ANTHROPIC_API_KEY"             # Read from env, never stored in file

[adapters]
cli_enabled = true
telegram_enabled = false              # Enabled in PRD-02
telegram_token_env = "TELEGRAM_BOT_TOKEN"
discord_enabled = false               # Future
discord_token_env = "DISCORD_BOT_TOKEN"

[brain]
enabled = false                       # Enabled in PRD-03
gbrain_command = "gbrain"
gbrain_args = ["serve"]
gbrain_working_dir = "~/brain"
tool_prefix = "gbrain"

[session]
storage_dir = "sessions"
max_turns_in_context = 50
max_turns_before_compaction = 30      # Phase 2

[skills]
skills_dir = "skills"
identity_dir = "identity"
```

**Validation rules:**
- `api_key_env` must resolve to a non-empty environment variable — fail fast if empty
- If `telegram_enabled = true`, `telegram_token_env` must resolve to a non-empty value
- `model` must be a non-empty string
- `max_tool_rounds` must be > 0; default to 10 if unset
- Validation is performed by `config.Validate() error` after loading; all errors are collected and printed together before exit

### 6.3 Go Module

```
module github.com/dandi/klawmbing

go 1.23
```

**Key dependencies:**

| Package | Purpose |
|---------|---------|
| `github.com/BurntSushi/toml` | TOML config parsing |
| `github.com/anthropics/anthropic-sdk-go` | Claude API client |
| `github.com/google/uuid` | Idempotency key generation |
| `golang.org/x/sync/errgroup` | Coordinating post-turn hook goroutines |

Standard library covers the rest: `log/slog`, `os/signal`, `context`, `sync`, `encoding/json`, `net/http`.

### 6.4 Core Data Types (internal/types/types.go)

```go
// Message is a normalized inbound message from any channel adapter.
type Message struct {
    ID        string            // UUID, generated by the adapter on receipt
    SessionID string            // Format: {scope}:{channel}:{identifier}
    Channel   string            // "telegram" | "discord" | "cli"
    Sender    string            // Platform user identifier
    Content   string            // Message text
    Timestamp time.Time
    Metadata  map[string]string // Channel-specific extras (e.g. chat_id, reply_to)
}

// Response is the assembled result of one LLM turn (after all tool rounds complete).
type Response struct {
    Content   string      // Final text content (empty if last turn was tool-only)
    ToolCalls []ToolCall  // All tool calls made during this turn
    Usage     TokenUsage
    Model     string
}

// ToolCall represents a single tool invocation requested by the LLM.
type ToolCall struct {
    ID             string          // tool_use ID from Claude API
    Name           string          // Registered tool name
    Input          json.RawMessage // Raw JSON input — handler unmarshals as needed
    IdempotencyKey string          // UUID generated by the runtime before dispatch
}

// ToolResult is the outcome of executing a ToolCall.
type ToolResult struct {
    ToolCallID string
    Output     string // Stringified result passed back to the LLM
    IsError    bool
    DurationMS int64
}

// TokenUsage tracks token counts across all rounds of a single turn.
type TokenUsage struct {
    InputTokens       int
    OutputTokens      int
    CacheReadTokens   int // Prompt caching hits
    CacheWriteTokens  int
}
```

### 6.5 Channel Adapter Interface (adapters/adapter.go)

```go
// ChannelAdapter is the normalized interface for all chat platforms.
// Each platform implements this contract; the runtime holds a []ChannelAdapter.
type ChannelAdapter interface {
    // Start begins listening for inbound messages.
    // Blocks until ctx is cancelled or a fatal error occurs.
    Start(ctx context.Context) error

    // Stop performs a graceful shutdown, draining in-flight sends.
    Stop() error

    // Send delivers a complete message to the user identified by sessionID.
    Send(ctx context.Context, sessionID string, msg string) error

    // SendStreaming consumes tokens from the channel and delivers them to the user.
    // Implementations must drain the channel fully even if the context is cancelled.
    SendStreaming(ctx context.Context, sessionID string, tokens <-chan string) error

    // SetMessageHandler registers the callback invoked for each inbound message.
    // The handler must be set before Start is called.
    // Signature: func(ctx context.Context, msg types.Message) error
    SetMessageHandler(handler MessageHandler)
}

// MessageHandler is the function signature adapters call when a message arrives.
type MessageHandler func(ctx context.Context, msg types.Message) error
```

### 6.6 Tool Registry (internal/tools/tools.go)

```go
// ToolHandler is the function signature every tool must implement.
// input is the raw JSON from the LLM's tool_use block.
// Return (result string, err error); on error the registry wraps it as an error ToolResult.
type ToolHandler func(ctx context.Context, input json.RawMessage) (string, error)

// ToolDefinition carries everything the registry needs to describe a tool to the LLM
// and dispatch calls to the handler.
type ToolDefinition struct {
    Name        string
    Description string
    InputSchema json.RawMessage // JSON Schema object sent to Claude API
    Handler     ToolHandler
}

// ToolRegistry is a concurrency-safe registry of available tools.
type ToolRegistry struct {
    mu    sync.RWMutex
    tools map[string]ToolDefinition
    logW  io.Writer // destination for tool-calls.jsonl lines
}

func NewToolRegistry(logWriter io.Writer) *ToolRegistry

// Register adds or replaces a tool. Safe to call concurrently (e.g. during MCP discovery).
func (r *ToolRegistry) Register(def ToolDefinition)

// Definitions returns tool definitions in the format expected by the Claude API.
// The returned slice is a snapshot; safe to read after the call returns.
func (r *ToolRegistry) Definitions() []anthropic.ToolParam

// Execute dispatches a ToolCall with idempotency check, timeout, error wrapping,
// and audit logging. It never panics; all errors become IsError ToolResults.
func (r *ToolRegistry) Execute(ctx context.Context, call types.ToolCall) types.ToolResult
```

**Execute internals (not exposed, for implementer reference):**

1. Check an in-memory `map[string]bool` of `IdempotencyKey` values under read lock; if already executed, return the cached result.
2. Release read lock; acquire write lock; check again (double-checked locking pattern).
3. Set a 30-second deadline on `ctx` for the handler call.
4. Call `handler(ctx, call.Input)`.
5. Record result in idempotency cache.
6. Append one JSON line to `logW`.

### 6.7 LLM Caller (internal/llm/llm.go)

```go
// LLMCaller wraps the Anthropic Go SDK.
// It owns the streaming tool-use loop and token accounting.
type LLMCaller struct {
    client          *anthropic.Client
    model           string
    extractionModel string
    maxTokens       int
    maxToolRounds   int
    registry        *tools.ToolRegistry
}

func NewLLMCaller(cfg config.LLMConfig, registry *tools.ToolRegistry) (*LLMCaller, error)

// Call sends a conversation to Claude and executes the tool-use loop until the model
// returns stop_reason "end_turn" or maxToolRounds is exhausted.
//
// Streaming tokens are sent to tokenCh as they arrive; the caller must drain this channel.
// tokenCh is closed when the final turn completes (successfully or not).
// The completed Response (full text + all tool calls + cumulative usage) is returned.
//
// messages follows the Claude API message format ([]anthropic.MessageParam).
func (c *LLMCaller) Call(
    ctx context.Context,
    messages []anthropic.MessageParam,
    systemPrompt string,
    tokenCh chan<- string,
) (types.Response, error)

// CallExtraction uses the cheap extraction model (Haiku) for non-streaming tasks
// such as fact extraction or summarization. Returns the full text response.
func (c *LLMCaller) CallExtraction(ctx context.Context, prompt string) (string, error)
```

**Tool-use loop (implemented inside `Call`, not exposed):**

```
for round := 0; round < c.maxToolRounds; round++ {
    stream := client.Messages.NewStreaming(ctx, params)
    // read stream events:
    //   - RawContentBlockDeltaEvent with TextDelta → send to tokenCh
    //   - RawContentBlockStopEvent with tool_use  → collect ToolCall
    // accumulate full message
    if message.StopReason == "end_turn" {
        break
    }
    // execute all tool calls, collect ToolResults
    // append assistant message + tool_result blocks to messages
    // loop
}
close(tokenCh)
return assembled Response
```

**Critical implementation notes:**
- Use `anthropic.NewClient()` which reads `ANTHROPIC_API_KEY` from the environment automatically — do not pass the key explicitly in code.
- Set `cache_control` on the system prompt block (type `ephemeral`) from day 1; the identity layer is identical across turns, making it a perfect cache candidate.
- Accumulate `TokenUsage` across all rounds by summing `Usage` from each API response.
- On HTTP 429: exponential backoff starting at 1s, doubling, capped at 30s, max 3 retries; log each retry at WARN level.
- Per-call deadline: 120 seconds (long tool loops). Pass via `context.WithTimeout` inside `Call`.

### 6.8 Runtime Orchestration (internal/runtime/runtime.go)

```go
// KlawmbingRuntime wires all components together and owns the message loop.
type KlawmbingRuntime struct {
    cfg        config.Config
    llm        *llm.LLMCaller
    tools      *tools.ToolRegistry
    adapters   []adapters.ChannelAdapter
    // session manager added by PRD-05; context assembler added by PRD-04
}

func NewKlawmbingRuntime(cfg config.Config) (*KlawmbingRuntime, error)

// Start initializes all components and launches each adapter in its own goroutine.
// Returns when ctx is cancelled (graceful shutdown) or a fatal error occurs.
//
// Startup sequence:
//  1. Initialize LLM caller
//  2. Initialize tool registry
//  3. Spawn MCP connections (PRD-03)
//  4. Load identity files (PRD-04)
//  5. Start enabled channel adapters (each in its own goroutine)
//  6. Log startup summary (model, adapters enabled, tool count)
func (r *KlawmbingRuntime) Start(ctx context.Context) error

// HandleMessage is the core loop invoked by channel adapters for every inbound message.
//
//  1. Resolve session and load history (PRD-05)
//  2. Assemble context: identity + skill + session history (PRD-04)
//  3. Create tokenCh and call LLM.Call in a goroutine
//  4. Pass tokenCh to adapter.SendStreaming while the goroutine runs
//  5. Wait for LLM.Call to return; collect Response
//  6. Run post-turn hooks via errgroup (session persist + metrics log)
//     — hook errors are logged, never propagated
//  7. Log completion metrics (session_id, tokens, latency, tool call count)
func (r *KlawmbingRuntime) HandleMessage(ctx context.Context, msg types.Message) error

// Shutdown stops all adapters, flushes in-flight logs, and closes connections.
func (r *KlawmbingRuntime) Shutdown(ctx context.Context) error
```

**Post-turn hooks** use `golang.org/x/sync/errgroup`. Each hook is a goroutine; the group is started with a fresh context so a hook timeout does not propagate to the next message. All `error` returns from hooks are logged at ERROR level and discarded.

### 6.9 Entry Point (cmd/klawmbing/main.go)

```go
// Usage:
//   ./klawmbing                      — start with default config.toml
//   ./klawmbing --config path.toml   — start with custom config
//   ./klawmbing --validate           — validate config and exit 0/1

func main() {
    flags := flag.NewFlagSet("klawmbing", flag.ExitOnError)
    configPath := flags.String("config", "config.toml", "path to config file")
    validateOnly := flags.Bool("validate", false, "validate config and exit")
    flags.Parse(os.Args[1:])

    cfg, err := config.Load(*configPath)
    if err != nil {
        slog.Error("config error", "err", err)
        os.Exit(1)
    }
    if *validateOnly {
        fmt.Println("config OK")
        os.Exit(0)
    }

    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    rt, err := runtime.NewKlawmbingRuntime(cfg)
    if err != nil {
        slog.Error("runtime init failed", "err", err)
        os.Exit(1)
    }

    if err := rt.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
        slog.Error("runtime exited with error", "err", err)
        os.Exit(1)
    }

    shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer shutdownCancel()
    rt.Shutdown(shutdownCtx)
}
```

**Signal handling:** `signal.NotifyContext` is the idiomatic Go approach — it cancels the root context on SIGINT/SIGTERM, which propagates through all components via `ctx`. No goroutine-per-signal needed.

---

## 7. Error Handling Strategy

| Error Type | Handling | User-Facing |
|------------|----------|-------------|
| Config validation failure | Collect all field errors, print together, `os.Exit(1)` | "config error: ANTHROPIC_API_KEY env var is empty" |
| Claude API rate limit (429) | Exponential backoff (1s, 2s, 4s, 8s, max 30s), 3 retries; log each at WARN | "Processing... (retry N/3)" sent to adapter |
| Claude API error (5xx) | Retry once after 2s; if still failing, return `error` from `LLMCaller.Call` | "Something went wrong with the AI service. Try again." |
| Tool handler returns error | Wrap as `ToolResult{IsError: true}`; LLM receives the error string and can retry or explain | LLM decides how to surface it to the user |
| Adapter `Send` failure | Log at ERROR; do not retry (message may already be partially delivered) | None (user sees nothing new; they can resend) |
| `HandleMessage` returns error | Catch at adapter callback site; log; attempt to send error message to user | "An unexpected error occurred. Check logs." |
| Hook goroutine panics | `recover()` in each hook wrapper; log stack trace; discard | None |

Go does not use exceptions. Every function that can fail returns `(T, error)`. Panics are reserved for truly unrecoverable states (e.g. programmer error in `init()`).

---

## 8. Logging & Observability

### Structured logging with `log/slog`

Configure a `slog.JSONHandler` at startup using the `log_level` from config:

```go
level := slog.LevelInfo // default
switch strings.ToUpper(cfg.Klawmbing.LogLevel) {
case "DEBUG": level = slog.LevelDebug
case "WARN":  level = slog.LevelWarn
case "ERROR": level = slog.LevelError
}
slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
```

Key log fields (always present in every structured log line):
- `session_id` — attached via `slog.With` inside `HandleMessage`
- `model` — on LLM call log lines
- `latency_ms` — on completion log lines

### tool-calls.jsonl format

One JSON object per line, appended by `ToolRegistry.Execute` and `LLMCaller.Call`:

```json
{
  "timestamp": "2026-04-26T14:30:00Z",
  "session_id": "main:telegram:123456",
  "message_id": "550e8400-e29b-41d4-a716-446655440000",
  "type": "llm_call",
  "model": "claude-sonnet-4-6-20260326",
  "input_tokens": 2340,
  "output_tokens": 512,
  "cache_read_tokens": 1800,
  "cache_write_tokens": 540,
  "latency_ms": 1450,
  "tool_calls": [
    {
      "name": "gbrain_search",
      "idempotency_key": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
      "output_length": 340,
      "is_error": false,
      "duration_ms": 230
    }
  ]
}
```

Tool input is intentionally omitted from the log line (may contain secrets). If debug-level tracing is needed, a separate debug log file can be added in Phase 2.

### Console output (stderr)

- **Startup:** `Klawmbing started model=... adapters=[cli] tools=0`
- **Per-message completion:** `turn complete session_id=... tokens=... latency_ms=... tool_calls=N`
- **Errors:** Full error chain via `fmt.Errorf("context: %w", err)` unwrapping; message content truncated to 200 chars

---

## 9. Acceptance Criteria (Definition of Done)

- [ ] `./klawmbing` starts without errors with a valid `config.toml`
- [ ] `./klawmbing --validate` checks config and exits 0 on success, 1 on failure
- [ ] Missing `ANTHROPIC_API_KEY` causes an immediate, clear error before any network call
- [ ] CLI adapter (implemented as the simplest possible adapter in PRD-02) can send a message and receive a streamed response via `tokenCh`
- [ ] If a tool handler is registered and the LLM invokes it, the handler runs and the LLM receives the result in the next round
- [ ] If a tool handler returns an error, it is caught, logged, and the process continues
- [ ] `logs/tool-calls.jsonl` is written after every LLM call
- [ ] SIGINT/SIGTERM triggers graceful shutdown: adapters drain, logs flush, connections close within 10 seconds
- [ ] The binary runs for 1 hour without memory growth under idle + periodic message load (verify with `runtime.ReadMemStats`)
- [ ] `go vet ./...` and `go build ./...` pass with zero warnings

---

## 10. Open Questions

| ID | Question | Default |
|----|----------|---------|
| TODO-R01 | Should we use `slog.TextHandler` (human-readable) for local dev and `slog.JSONHandler` for prod, switchable via `log_format = "text" \| "json"` in config? | Default to JSON always; `jq` is available everywhere. Re-evaluate if the log noise is painful during development. |
| TODO-R02 | Prompt caching: enable `cache_control` from day 1 or add later? The identity layer (AGENTS.md + SOUL.md + USER.md) is identical across turns — a perfect cache candidate. | Enable from day 1. The Go SDK supports `cache_control` on `SystemPromptParam` blocks. |
| TODO-R03 | Should `tool-calls.jsonl` be rotated? At scale it could grow large. | Rotate daily, keep 30 days. Implement in Phase 2 using a simple `lumberjack`-style rotate-on-open approach. |
| TODO-R04 | Should `HandleMessage` run one goroutine per adapter message concurrently, or serialize within a session? | Serialize per session (one in-flight turn per `session_id`); concurrency across sessions is fine. Use a `sync.Map[string]*sync.Mutex` keyed by session_id. |
