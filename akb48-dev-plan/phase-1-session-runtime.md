# Phase 1: Session & Runtime Core

**Goal:** A working CLI agent with persistent session history. The operator can type a message, receive a response, restart the process, and see prior conversation context loaded automatically.

**Definition of Done:**
- `./akb48` starts and accepts messages via CLI
- Multi-turn conversations work (agent recalls earlier turns)
- Restarting the process loads the previous session from disk
- Graceful shutdown on `Ctrl+C`

**Tickets:** KLW-004 → KLW-005 → KLW-006 → KLW-007 → KLW-008 → KLW-009 (in order)

---

## KLW-004 — Session Data Model & JSONL Persistence

**Type:** Chore
**Owner:** Backend
**Effort:** 5 SP
**Labels:** `phase/1`, `type/chore`, `size/M`, `component/session`
**Dependencies:** KLW-001 (types, config)
**Branch:** `feat/session-model`

### Description

Implement the session data model (`Session`, `SessionTurn`) and append-only JSONL persistence. This is the storage layer for all conversation history — every message, response, tool call, and token count is written to disk atomically.

### User Story

N/A — internal infrastructure.

### Implementation Plan

**Files to create:**
- `internal/session/session.go` — data model + persistence
- `internal/session/session_test.go`

**Data structures:**

```go
// internal/session/session.go
package session

import (
    "encoding/json"
    "sync"
    "time"
    "github.com/dydanz/akb48/internal/types"
)

type ToolCallRecord struct {
    Name       string          `json:"name"`
    Input      json.RawMessage `json:"input"`
    Output     string          `json:"output"`      // truncated to 500 chars
    DurationMs int64           `json:"duration_ms"`
}

type SessionTurn struct {
    TurnID            string           `json:"turn_id"`
    Timestamp         time.Time        `json:"timestamp"`
    UserMessage       string           `json:"user_message"`
    AssistantResponse string           `json:"assistant_response"`
    ToolCalls         []ToolCallRecord `json:"tool_calls,omitempty"`
    TokenUsage        types.TokenUsage `json:"token_usage"`
    SkillUsed         string           `json:"skill_used,omitempty"`
    LatencyMs         int64            `json:"latency_ms"`
}

type Session struct {
    SessionID string        `json:"session_id"`
    CreatedAt time.Time     `json:"created_at"`
    UpdatedAt time.Time     `json:"updated_at"`
    Turns     []SessionTurn `json:"turns"`
    mu        sync.RWMutex  // not serialised
}

func NewSession(sessionID string) *Session
func (s *Session) AddTurn(turn SessionTurn)          // acquires write lock
func (s *Session) TurnCount() int                    // acquires read lock
func (s *Session) IsCold(threshold time.Duration) bool
```

**IsCold logic:**
```go
func (s *Session) IsCold(threshold time.Duration) bool {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return len(s.Turns) == 0 || time.Since(s.UpdatedAt) > threshold
}
```

**JSONL format** — file at `{storage_dir}/{session_id_sanitized}.jsonl` (`:` → `_`):
- Line 1: `{"event":"session_created","session_id":"...","created_at":"..."}`
- Subsequent lines: one `SessionTurn` JSON object per line

**Persistence functions:**

```go
// SanitizeID replaces ":" with "_" for filesystem safety
func SanitizeID(sessionID string) string

// Persist appends one turn to the session's JSONL file
// File opened with O_APPEND|O_CREATE|O_WRONLY — never rewrites
func Persist(filePath string, turn SessionTurn) error

// LoadFromDisk reads a JSONL file, skips corrupt lines with slog.Warn
// Returns a Session with all valid turns loaded
func LoadFromDisk(filePath string) (*Session, error)
```

### Acceptance Criteria

- [ ] Write 3 turns to a session file; inspect with `jq` — all 3 present with correct fields
- [ ] Corrupt JSON line mid-file → skipped with `slog.Warn`; remaining turns loaded
- [ ] `IsCold(30 * time.Minute)` returns `true` for brand-new session
- [ ] `IsCold(30 * time.Minute)` returns `false` immediately after `AddTurn`
- [ ] `IsCold(30 * time.Minute)` returns `true` after simulated 31-minute gap
- [ ] File path uses sanitized session ID (`main:cli:local` → `main_cli_local.jsonl`)
- [ ] `go test ./internal/session/... -run TestSession` passes

### Testing Plan

```go
// Test 1: roundtrip persistence
func TestSessionPersistAndLoad(t *testing.T) {
    // Create session, add 3 turns, persist each
    // Create new session from LoadFromDisk(path)
    // Assert TurnCount == 3, fields match
}

// Test 2: corrupt line recovery
func TestLoadFromDisk_CorruptLine(t *testing.T) {
    // Write valid line, then "not json", then valid line
    // Assert LoadFromDisk returns session with 2 turns
    // Assert slog.Warn was called (capture with slog handler)
}

// Test 3: IsCold threshold
func TestIsCold(t *testing.T) {
    s := NewSession("test")
    assert IsCold(30min) == true
    s.AddTurn(...)
    assert IsCold(30min) == false
}
```

---

## KLW-005 — SessionManager

**Type:** Chore
**Owner:** Backend
**Effort:** 5 SP
**Labels:** `phase/1`, `type/chore`, `size/M`, `component/session`
**Dependencies:** KLW-004
**Branch:** `feat/session-manager`

### Description

The `SessionManager` coordinates session lifecycle: in-memory caching, disk loading at startup, file handle management, and context window assembly for LLM calls. It is the single owner of all session state.

### Implementation Plan

**Files to create:**
- `internal/session/manager.go`
- `internal/session/manager_test.go`

**Struct and methods:**

```go
type SessionManager struct {
    storageDir  string
    sessions    sync.Map       // map[string]*Session
    fileHandles sync.Map       // map[string]*os.File — one handle per session
    cfg         SessionConfig
}

func NewSessionManager(cfg config.SessionConfig) *SessionManager

// ResolveOrCreate returns an existing session or creates a new one.
// Uses sync.Map.LoadOrStore to be race-free.
func (m *SessionManager) ResolveOrCreate(ctx context.Context, sessionID string) (*Session, error)

// GetContextTurns returns turns for the LLM context window.
// Respects MaxTurnsInContext and MaxContextTokens.
// Returns turns in chronological order (oldest first).
func (m *SessionManager) GetContextTurns(session *Session) []SessionTurn

// Persist appends one turn to the session file.
// Re-uses cached file handle; creates file on first call.
func (m *SessionManager) Persist(session *Session, turn SessionTurn) error

// LoadAll scans storageDir for .jsonl files and loads each session at startup.
func (m *SessionManager) LoadAll(ctx context.Context) error

// Shutdown closes all open file handles.
func (m *SessionManager) Shutdown() error
```

**GetContextTurns token budget logic:**
```go
func (m *SessionManager) GetContextTurns(session *Session) []SessionTurn {
    session.mu.RLock()
    defer session.mu.RUnlock()

    turns := session.Turns
    // Apply turn count limit
    if len(turns) > m.cfg.MaxTurnsInContext {
        turns = turns[len(turns)-m.cfg.MaxTurnsInContext:]
    }
    // Apply token budget — walk newest to oldest, drop oldest
    if m.cfg.MaxContextTokens > 0 {
        tokenCount := 0
        cutoff := 0
        for i := len(turns) - 1; i >= 0; i-- {
            raw, _ := json.Marshal(turns[i])
            tokenCount += len(raw) / 4
            if tokenCount > m.cfg.MaxContextTokens {
                cutoff = i + 1
                break
            }
        }
        turns = turns[cutoff:]
    }
    return turns
}
```

**LoadAll startup log:**
```go
slog.Info("Sessions loaded", "count", sessionCount, "total_turns", totalTurns)
```

**File size warning:**
```go
// After Persist, check file size and warn if > MaxFileSizeMB
if fi.Size() > int64(m.cfg.MaxFileSizeMB)*1024*1024 {
    slog.Warn("Session file exceeds size limit", "session_id", session.SessionID, "size_mb", fi.Size()/1024/1024)
}
```

### Acceptance Criteria

- [ ] `LoadAll` on a directory with 2 session files → both loaded; startup log shows correct counts
- [ ] `ResolveOrCreate` called 10× concurrently for same session ID → only 1 session created (no duplicates)
- [ ] `GetContextTurns` on 60-turn session with `MaxTurnsInContext=50` → returns exactly 50 turns
- [ ] `GetContextTurns` respects `MaxContextTokens=32000` by dropping oldest turns
- [ ] `Shutdown` closes all file handles; subsequent read of file succeeds
- [ ] File size warning triggers when session file exceeds `MaxFileSizeMB`

### Testing Plan

```go
func TestGetContextTurns_TurnLimit(t *testing.T) {
    // Add 60 turns, MaxTurnsInContext=50
    // Assert len(GetContextTurns()) == 50 and they are the newest 50
}

func TestGetContextTurns_TokenBudget(t *testing.T) {
    // Add turns with known sizes, set MaxContextTokens to cut at turn 30
    // Assert only turns 31-N returned
}

func TestResolveOrCreate_Concurrent(t *testing.T) {
    // 10 goroutines call ResolveOrCreate with same ID via errgroup
    // Assert all return same *Session pointer
}

func TestLoadAll_MultipleFiles(t *testing.T) {
    // Write 2 session files to temp dir
    // LoadAll, assert 2 sessions loaded with correct turn counts
}
```

---

## KLW-006 — Post-Turn Hooks

**Type:** Chore
**Owner:** Backend
**Effort:** 3 SP
**Labels:** `phase/1`, `type/chore`, `size/M`, `component/session`
**Dependencies:** KLW-004, KLW-005
**Branch:** `feat/post-turn-hooks`

### Description

The hook system fires after every agent response. Hooks are fire-and-forget — a failed hook is logged and never affects the user response or subsequent turns.

### Implementation Plan

**Files to create:**
- `internal/session/hooks.go`
- `internal/session/hooks_test.go`

**Types and functions:**

```go
// HookFunc is the signature for all post-turn hooks.
type HookFunc func(ctx context.Context, session *Session, turn SessionTurn) error

// RunHooks runs all hooks concurrently with a 5-second deadline.
// Errors are logged; RunHooks never returns an error.
func RunHooks(hooks []HookFunc, session *Session, turn SessionTurn) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    var eg errgroup.Group
    for i, h := range hooks {
        hook := h
        idx := i
        eg.Go(func() error {
            if err := hook(ctx, session, turn); err != nil {
                slog.Error("Post-turn hook failed",
                    "hook_index", idx,
                    "session_id", session.SessionID,
                    "turn_id", turn.TurnID,
                    "error", err)
            }
            return nil // never propagate
        })
    }
    eg.Wait()
}

// PersistSessionHook returns a HookFunc that appends the turn to JSONL.
func PersistSessionHook(manager *SessionManager) HookFunc

// LogMetricsHook returns a HookFunc that appends a metrics line to tool-calls.jsonl.
// Metrics line format:
// {"timestamp":"...","session_id":"...","turn_id":"...","token_usage":{...},
//  "tool_names":["gbrain_search"],"skill_used":"note-capture","latency_ms":1234}
func LogMetricsHook(logPath string) HookFunc
```

**LogMetricsHook** — opens `tool-calls.jsonl` with `O_APPEND|O_CREATE|O_WRONLY` on each call (acceptable for Phase 1 frequency; Phase 2 can cache handle).

### Acceptance Criteria

- [ ] `RunHooks` with one failing hook + one succeeding hook → succeeding hook still runs
- [ ] `RunHooks` with a hook that panics → recovered; other hooks run; no crash
- [ ] Hook that blocks for 10s → `RunHooks` returns within ~6s (5s timeout + margin)
- [ ] `PersistSessionHook` → turn appears in JSONL file
- [ ] `LogMetricsHook` → metrics line appended to `tool-calls.jsonl` with correct fields
- [ ] `go test ./internal/session/... -run TestHook` passes

### Testing Plan

```go
func TestRunHooks_FailingHookDoesNotBlock(t *testing.T) {
    failHook := func(...) error { return errors.New("boom") }
    var called bool
    okHook := func(...) error { called = true; return nil }
    RunHooks([]HookFunc{failHook, okHook}, ...)
    assert called == true
}

func TestRunHooks_SlowHookTimesOut(t *testing.T) {
    slow := func(ctx context.Context, ...) error {
        <-ctx.Done(); return ctx.Err()
    }
    start := time.Now()
    RunHooks([]HookFunc{slow}, ...)
    assert time.Since(start) < 6*time.Second
}

func TestPersistSessionHook(t *testing.T) {
    // Create temp session, call hook, verify JSONL file exists and contains turn
}

func TestLogMetricsHook(t *testing.T) {
    // Create temp file path, call hook, read file, unmarshal, verify fields
}
```

---

## KLW-007 — AKB48Runtime & HandleMessage

**Type:** Chore
**Owner:** Backend
**Effort:** 8 SP
**Labels:** `phase/1`, `type/chore`, `size/L`, `component/runtime`
**Dependencies:** KLW-003 (LLM caller), KLW-005 (session manager), KLW-006 (hooks)
**Branch:** `feat/runtime`

### Description

The central runtime that wires all components together. `HandleMessage` is the per-turn entrypoint called by all adapters. In Phase 1, the context assembler is a stub (empty system prompt + session history). Full assembly comes in Phase 2 (KLW-011).

### Implementation Plan

**Files to create:**
- `internal/runtime/runtime.go`
- `internal/runtime/runtime_test.go`

**Struct and interfaces:**

```go
// MessageHandler is the function signature adapters call per incoming message.
type MessageHandler func(ctx context.Context, msg types.Message, tokens chan<- string) error

type AKB48Runtime struct {
    cfg            *config.Config
    llmCaller      *llm.Caller
    registry       *tools.Registry
    sessionManager *session.SessionManager
    assembler      ContextAssembler // interface — Phase 1: stub, Phase 2: full
    hooks          []session.HookFunc
}

// ContextAssembler is the interface the runtime uses to build LLM context.
// Phase 1 stub returns empty system prompt + session history only.
type ContextAssembler interface {
    Build(sess *session.Session, userMessage string, coldContext string) (systemPrompt string, messages []types.LLMMessage, err error)
}

func NewRuntime(cfg *config.Config) (*AKB48Runtime, error)

func (r *AKB48Runtime) HandleMessage(ctx context.Context, msg types.Message, tokens chan<- string) error

func (r *AKB48Runtime) Start(ctx context.Context) error

func (r *AKB48Runtime) Stop() error
```

**HandleMessage flow:**
```go
func (r *AKB48Runtime) HandleMessage(ctx context.Context, msg types.Message, tokens chan<- string) error {
    start := time.Now()

    // 1. Resolve session
    sess, err := r.sessionManager.ResolveOrCreate(ctx, msg.SessionID)
    if err != nil { return err }

    // 2. Build context (Phase 1: stub returns session history + empty system)
    systemPrompt, messages, err := r.assembler.Build(sess, msg.Text, "")
    if err != nil { return err }

    // 3. Append current user message
    messages = append(messages, types.LLMMessage{Role: types.RoleUser, Content: msg.Text})

    // 4. Call LLM
    result, err := r.llmCaller.Call(ctx, llm.CallParams{
        System:   systemPrompt,
        Messages: messages,
        Tokens:   tokens,
    })
    if err != nil { return err }

    // 5. Build turn record
    turn := session.SessionTurn{
        TurnID:            uuid.NewString(),
        Timestamp:         time.Now().UTC(),
        UserMessage:       msg.Text,
        AssistantResponse: result.Text,
        TokenUsage:        result.TokenUsage,
        LatencyMs:         time.Since(start).Milliseconds(),
    }

    // 6. Commit to session
    sess.AddTurn(turn)

    // 7. Fire-and-forget hooks
    session.RunHooks(r.hooks, sess, turn)

    return nil
}
```

**Phase 1 stub assembler:**
```go
type stubAssembler struct {
    manager *session.SessionManager
}

func (s *stubAssembler) Build(sess *session.Session, userMessage, coldContext string) (string, []types.LLMMessage, error) {
    turns := s.manager.GetContextTurns(sess)
    var msgs []types.LLMMessage
    for _, t := range turns {
        msgs = append(msgs, types.LLMMessage{Role: types.RoleUser, Content: t.UserMessage})
        msgs = append(msgs, types.LLMMessage{Role: types.RoleAssistant, Content: t.AssistantResponse})
    }
    return "", msgs, nil
}
```

**Start and Stop:**
```go
func (r *AKB48Runtime) Start(ctx context.Context) error {
    if err := r.sessionManager.LoadAll(ctx); err != nil {
        return fmt.Errorf("load sessions: %w", err)
    }
    slog.Info("AKB48 started", "sessions_loaded", ...)
    return nil
}

func (r *AKB48Runtime) Stop() error {
    return r.sessionManager.Shutdown()
}
```

### Acceptance Criteria

- [ ] `HandleMessage` with mock LLM returning "pong" → turn added to session, hooks called
- [ ] Two consecutive `HandleMessage` calls → session has 2 turns
- [ ] LLM error → `HandleMessage` returns error; no turn added
- [ ] `Start` logs session count from `LoadAll`
- [ ] `Stop` flushes all session file handles
- [ ] `go test ./internal/runtime/...` passes

### Testing Plan

```go
func TestHandleMessage_AddsTurn(t *testing.T) {
    // Mock LLM returning "pong"
    // Call HandleMessage with "ping"
    // Assert session.TurnCount() == 1
    // Assert turn.AssistantResponse == "pong"
}

func TestHandleMessage_LLMError(t *testing.T) {
    // Mock LLM returning error
    // Assert HandleMessage returns error
    // Assert session.TurnCount() == 0
}

func TestHandleMessage_HooksCalled(t *testing.T) {
    // Register a hook that sets a flag
    // Call HandleMessage
    // Assert flag was set
}

func TestRuntime_MultiTurnContext(t *testing.T) {
    // 3 HandleMessage calls
    // Verify 3rd call's LLM context contains first 2 turns
}
```

---

## KLW-008 — CLI Adapter

**Type:** User Story
**Owner:** Backend
**Effort:** 3 SP
**Labels:** `phase/1`, `type/user-story`, `size/M`, `component/adapter`
**Dependencies:** KLW-007
**Branch:** `feat/cli-adapter`

### User Story

> As an operator, I want to run `./akb48` and interact with the agent via a terminal prompt, so I can develop and test without needing Telegram.

### Implementation Plan

**Files to create:**
- `adapters/cli/cli.go`
- `adapters/cli/cli_test.go`

**Struct:**

```go
type CLIAdapter struct {
    reader    *bufio.Scanner
    writer    io.Writer
    handler   runtime.MessageHandler
    sessionID string
}

func NewCLIAdapter(cfg config.CLIConfig, handler runtime.MessageHandler) *CLIAdapter {
    return &CLIAdapter{
        reader:    bufio.NewScanner(os.Stdin),
        writer:    os.Stdout,
        sessionID: "main:cli:local",
        handler:   handler,
    }
}
```

**Start loop:**
```go
func (a *CLIAdapter) Start(ctx context.Context) error {
    for {
        fmt.Fprint(a.writer, "> ")
        if !a.reader.Scan() {
            return nil // EOF or error
        }
        line := strings.TrimSpace(a.reader.Text())
        if line == "" {
            continue
        }
        select {
        case <-ctx.Done():
            return nil
        default:
        }

        tokens := make(chan string, 64)
        var wg sync.WaitGroup
        wg.Add(1)
        go func() {
            defer wg.Done()
            for t := range tokens {
                fmt.Fprint(a.writer, t)
            }
            fmt.Fprintln(a.writer)
        }()

        msg := types.Message{
            SessionID: a.sessionID,
            Text:      line,
            Timestamp: time.Now(),
        }
        if err := a.handler(ctx, msg, tokens); err != nil {
            slog.Error("Handler error", "error", err)
            fmt.Fprintf(a.writer, "Error: %v\n", err)
        }
        close(tokens)
        wg.Wait()
    }
}
```

**Session ID:** Always `"main:cli:local"` — no authentication, local process only.

### Acceptance Criteria

- [ ] `./akb48` prints `> ` prompt and waits for input
- [ ] Typing "hello" → handler called → response printed to stdout
- [ ] Empty input (just Enter) → skipped, prompt shown again
- [ ] Multi-turn: second message includes first turn in context
- [ ] Streaming: tokens appear progressively, not all at once
- [ ] `Ctrl+C` → context cancelled → clean exit
- [ ] EOF (piped input ends) → clean exit

### Testing Plan

```go
func TestCLIAdapter_SingleMessage(t *testing.T) {
    input := strings.NewReader("hello\n")
    var output bytes.Buffer
    var called bool
    handler := func(ctx context.Context, msg types.Message, tokens chan<- string) error {
        called = true
        tokens <- "world"
        return nil
    }
    adapter := &CLIAdapter{reader: bufio.NewScanner(input), writer: &output, handler: handler, sessionID: "test"}
    adapter.Start(context.Background())
    assert called == true
    assert strings.Contains(output.String(), "world")
}

func TestCLIAdapter_EmptyLineSkipped(t *testing.T) {
    // Input: "\nhello\n" — assert handler called exactly once
}
```

---

## KLW-009 — Entry Point & Graceful Shutdown

**Type:** Chore
**Owner:** Backend
**Effort:** 3 SP
**Labels:** `phase/1`, `type/chore`, `size/M`, `component/runtime`
**Dependencies:** KLW-007, KLW-008
**Branch:** `feat/entrypoint`

### Description

Replace the stub `cmd/akb48/main.go` with the full entry point: flag parsing, config loading, runtime wiring, signal handling, and graceful shutdown.

### Implementation Plan

**File to modify:** `cmd/akb48/main.go`

```go
package main

import (
    "context"
    "flag"
    "fmt"
    "log/slog"
    "os"
    "os/signal"
    "syscall"

    "github.com/dydanz/akb48/internal/config"
    "github.com/dydanz/akb48/internal/runtime"
    "github.com/dydanz/akb48/adapters/cli"
)

func main() {
    configPath := flag.String("config", "config.toml", "Path to config.toml")
    validate := flag.Bool("validate", false, "Validate config and exit")
    flag.Parse()

    cfg, err := config.Load(*configPath)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
        os.Exit(1)
    }

    if *validate {
        fmt.Println("Config OK")
        os.Exit(0)
    }

    rt, err := runtime.NewRuntime(cfg)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Runtime init error: %v\n", err)
        os.Exit(1)
    }

    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    if err := rt.Start(ctx); err != nil {
        slog.Error("Start failed", "error", err)
        os.Exit(1)
    }

    // Start CLI adapter (Phase 1 default; Phase 4 adds Telegram)
    adapter := cli.NewCLIAdapter(cfg.Adapters.CLI, rt.HandleMessage)
    go func() {
        if err := adapter.Start(ctx); err != nil {
            slog.Error("CLI adapter error", "error", err)
        }
    }()

    <-ctx.Done()
    slog.Info("Shutting down...")
    if err := rt.Stop(); err != nil {
        slog.Error("Shutdown error", "error", err)
    }
    slog.Info("Shutdown complete")
}
```

**Flags:**
- `--config` (default: `config.toml`) — path to TOML config
- `--validate` — validate config and exit (useful in deploy scripts)

### Acceptance Criteria

- [ ] `./akb48 --validate` exits 0 with "Config OK" when config + env vars valid
- [ ] `./akb48 --validate` exits 1 with clear error when `ANTHROPIC_API_KEY` not set
- [ ] `./akb48 --config nonexistent.toml` exits 1 with clear error
- [ ] `./akb48` starts and prints `> ` prompt
- [ ] `Ctrl+C` → "Shutting down..." → "Shutdown complete" logged → clean exit
- [ ] `go build ./cmd/akb48/` produces a binary under 30MB
- [ ] **End-to-end Phase 1 smoke test:** start binary, send "hello", receive response, `Ctrl+C`, verify session JSONL written

### Testing Plan

```bash
# Build
go build -o akb48 ./cmd/akb48/

# Validate test
ANTHROPIC_API_KEY=test ./akb48 --validate
echo $?  # expect 0

# Missing key test
unset ANTHROPIC_API_KEY
./akb48 --validate
echo $?  # expect 1

# Manual smoke test (requires real API key)
ANTHROPIC_API_KEY=sk-ant-... ./akb48
# Type: hello
# Expect: response text
# Ctrl+C
# cat sessions/main_cli_local.jsonl | jq .
```
