# PRD-05: Session Management & Persistence

**Status:** Draft v2.0 (Go)
**Parent:** PRD-00 (AKB48 Master PRD)
**Author:** Dandi
**Created:** April 26, 2026
**Revised:** May 1, 2026
**Dependencies:** PRD-01 (Core Runtime), PRD-02 (Channel Adapters — session IDs)
**Estimated Effort:** 2-3 days

---

## 1. Problem

Without session management, every message is independent — the agent has no memory of what was said 5 minutes ago. Without persistence, restarting AKB48 erases all conversation context. Without post-turn hooks, there's no place to extract facts for the brain or trigger compaction.

This PRD covers:
- Session resolution (which session does this message belong to?)
- Session state (conversation history maintained in memory)
- Session persistence (JSONL files that survive restarts)
- Post-turn hooks (extensible processing after each agent response)

---

## 2. Goals

- **G1:** Each message is associated with a session that maintains conversation history
- **G2:** Session history is sent to the LLM as message context (the LLM "remembers" the conversation)
- **G3:** Sessions persist to disk as JSONL files and survive process restarts
- **G4:** On startup, AKB48 loads the most recent session per session ID and resumes
- **G5:** Post-turn hooks run after every agent response (persist session, log metrics)
- **G6:** Session state is bounded — old turns are available but the architecture supports future compaction

## 3. Non-Goals

- Session compaction / summarization (Phase 2 — requires memory flush to brain first)
- Memory flush / fact extraction from sessions (Phase 2)
- Multi-session management (switching between sessions in one chat)
- Session sharing between adapters (CLI session != Telegram session)
- Session encryption

---

## 4. User Stories

| ID | Story | Acceptance Criteria |
|----|-------|-------------------|
| US-P01 | As an operator, I send multiple messages and the agent remembers what I said earlier in the conversation | Message 1: "Our database is Postgres 16." Message 5: "What database are we using?" -> "Postgres 16." |
| US-P02 | As an operator, I restart AKB48 and my conversation context is preserved | After restart, ask "What did I just tell you about the database?" -> Agent recalls from loaded session. |
| US-P03 | As an operator, my Telegram session and CLI session are independent | Facts shared in CLI don't appear in Telegram session history (they may appear via brain if stored). |
| US-P04 | As a developer, I can inspect a session file to see the full conversation history | JSONL file contains human-readable entries with role, content, timestamp, and tool calls. |
| US-P05 | As a developer, post-turn hooks run reliably after every response | Hook logs confirm execution. If a hook fails, the error is logged but doesn't affect the user response. |

---

## 5. Technical Design

### 5.1 Session Data Model

```go
// core/types.go

// TokenUsage mirrors the Anthropic API usage block.
type TokenUsage struct {
    InputTokens      int `json:"input_tokens"`
    OutputTokens     int `json:"output_tokens"`
    CacheReadTokens  int `json:"cache_read_tokens"`
    CacheWriteTokens int `json:"cache_write_tokens"`
}

// ToolCallRecord is a single tool invocation within a turn.
// Input is stored as raw JSON so the original structure is preserved without
// a round-trip through Go types. Output is truncated to 500 chars before
// being written to the session file (full output lives in tool-calls.jsonl).
type ToolCallRecord struct {
    Name       string          `json:"name"`
    Input      json.RawMessage `json:"input"`
    Output     string          `json:"output"`
    DurationMs int64           `json:"duration_ms"`
}

// SessionTurn is one complete request/response cycle.
type SessionTurn struct {
    TurnID            string           `json:"turn_id"`             // UUID v4
    Timestamp         time.Time        `json:"timestamp"`           // UTC, RFC3339
    UserMessage       string           `json:"user_message"`
    AssistantResponse string           `json:"assistant_response"`
    ToolCalls         []ToolCallRecord `json:"tool_calls,omitempty"`
    TokenUsage        TokenUsage       `json:"token_usage"`
    SkillUsed         string           `json:"skill_used,omitempty"` // empty = general mode
    LatencyMs         int64            `json:"latency_ms"`
}

// Session is the in-memory representation of one conversation thread.
// mu is not serialised — it guards Turns and UpdatedAt for concurrent access
// from the main goroutine (write) and background hook goroutines (read).
type Session struct {
    SessionID string        `json:"session_id"`  // e.g. "main:telegram:123456789"
    CreatedAt time.Time     `json:"created_at"`
    UpdatedAt time.Time     `json:"updated_at"`
    Turns     []SessionTurn `json:"turns"`
    mu        sync.RWMutex  // not serialised
}

// TurnCount returns the number of turns under a read lock.
func (s *Session) TurnCount() int {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return len(s.Turns)
}

// IsCold returns true if this is a brand-new session or the last activity
// was more than threshold ago. Used by hooks to decide whether to skip
// expensive operations on idle sessions.
func (s *Session) IsCold(threshold time.Duration) bool {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return len(s.Turns) == 0 || time.Since(s.UpdatedAt) > threshold
}

// AddTurn appends a turn and advances UpdatedAt. Called from the main loop
// after the LLM response is complete; the write lock prevents hook goroutines
// from reading a partial slice.
func (s *Session) AddTurn(turn SessionTurn) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.Turns = append(s.Turns, turn)
    s.UpdatedAt = time.Now().UTC()
}
```

**Why `json.RawMessage` for `ToolCallRecord.Input`:** Tool inputs arrive from the Anthropic SDK as `json.RawMessage` already. Re-encoding them through a `map[string]any` would lose field order and number precision. Keeping them raw avoids that and is cheaper.

**Why `sync.RWMutex` on the struct, not a global lock:** Each `Session` has its own mutex. Concurrent hook goroutines can safely `RLock` different sessions simultaneously. Only a single write per turn (from `AddTurn`) holds a write lock.

### 5.2 Session Manager

```go
// core/session.go

// SessionConfig is the parsed [session] block from config.toml.
type SessionConfig struct {
    StorageDir              string `toml:"storage_dir"`               // relative to ~/.akb48/
    MaxTurnsInContext       int    `toml:"max_turns_in_context"`       // default: 50
    MaxTurnsBeforeCompaction int   `toml:"max_turns_before_compaction"` // Phase 2; default: 30
    MaxFileSizeMB           int    `toml:"max_file_size_mb"`           // warn threshold; default: 10
    MaxContextTokens        int    `toml:"max_context_tokens"`         // Tier 2 token budget; default: 40000
    LoadOnStartup           bool   `toml:"load_on_startup"`            // default: true
}

// SessionManager owns session lifecycle: creation, loading, caching, and persistence.
// sessions is a sync.Map (not a plain map + mutex) because reads vastly outnumber
// writes and sync.Map is optimised for that pattern.
// fileHandles caches one open *os.File per session to avoid open/close overhead
// on every turn append.
type SessionManager struct {
    storageDir  string
    config      SessionConfig
    sessions    sync.Map // map[string]*Session
    fileHandles sync.Map // map[string]*os.File — closed on Shutdown()
    logger      *slog.Logger
}

func NewSessionManager(cfg SessionConfig, logger *slog.Logger) (*SessionManager, error)

// ResolveOrCreate returns the live *Session for sessionID.
// Resolution order:
//   1. In-memory cache (sync.Map lookup — no lock contention)
//   2. Disk (JSONL file) — loaded and cached if found
//   3. New session — created, cached, but NOT written to disk until first Persist()
func (m *SessionManager) ResolveOrCreate(ctx context.Context, sessionID string) (*Session, error)

// Persist appends turn to the session's JSONL file.
// The file handle is kept open (cached in fileHandles) so the OS doesn't pay
// open/stat/close on every turn. The write is: json.Marshal(turn) + "\n".
// The file is opened with os.O_APPEND|os.O_CREATE|os.O_WRONLY and 0644 perms.
// If the file exceeds MaxFileSizeMB after the write, a warning is logged.
func (m *SessionManager) Persist(session *Session, turn SessionTurn) error

// LoadAll reads every *.jsonl file in storageDir and populates the in-memory
// cache. Called once at startup before any adapter begins accepting messages.
// Corrupt lines are skipped with a slog.Warn; the rest of the file is loaded.
// Logs: "Loaded N sessions (M total turns)" on success.
func (m *SessionManager) LoadAll(ctx context.Context) error

// GetContextTurns returns the slice of turns to send to the LLM.
// Phase 1: newest MaxTurnsInContext turns (simple slice from the tail).
// Phase 2 (Tier 2 token budget): walks turns newest-to-oldest, accumulating
//   estimated tokens via len(turnJSON)/4 heuristic, stops when MaxContextTokens hit.
// Always acquires session.mu.RLock().
func (m *SessionManager) GetContextTurns(session *Session) []SessionTurn

// Shutdown closes all cached file handles. Called from the runtime's cleanup path.
func (m *SessionManager) Shutdown()
```

**File handle cache rationale:** A solo-operator deployment typically has 2-3 active sessions (CLI + Telegram DM). Keeping file handles open is safe and eliminates repeated `open` syscalls per turn. On shutdown, `Shutdown()` iterates `fileHandles` and calls `f.Close()` on each.

### 5.3 JSONL File Format

Each session is stored as a JSONL file: one JSON object per line.

**File location:** `~/.akb48/sessions/{session_id_sanitized}.jsonl`

Session ID sanitization: `strings.ReplaceAll(sessionID, ":", "_")` — e.g. `main_telegram_123456789.jsonl`

**First line — session header (written once on session creation):**

```json
{"type":"session_created","session_id":"main:telegram:123456789","created_at":"2026-04-26T14:00:00Z"}
```

**Subsequent lines — one per turn:**

```json
{"turn_id":"a1b2c3d4-...","timestamp":"2026-04-26T14:00:05Z","user_message":"Remember that our database port is 5433","assistant_response":"Stored. Your database runs on port 5433.","tool_calls":[{"name":"gbrain_put","input":{"title":"Database Configuration","content":"..."},"output":"Page created: Database Configuration","duration_ms":230}],"token_usage":{"input_tokens":1240,"output_tokens":45,"cache_read_tokens":800,"cache_write_tokens":0},"skill_used":"note-capture","latency_ms":2100}
```

**Design decisions:**

- Append-only: each turn is one line appended via the cached file handle. The file is never rewritten.
- No pretty-printing in the file: `json.Marshal` (compact) keeps lines short and `jq` handles formatting at read time.
- Crash-safe: if the process dies mid-turn, completed turns are intact. The in-flight turn is lost; no partial lines are written because `Write` is atomic for small payloads under 4KB (OS guarantee on POSIX).
- Size guard: after each `Persist`, check `f.Stat().Size()` and log `slog.Warn` if it exceeds `MaxFileSizeMB * 1024 * 1024`. No action taken in Phase 1.
- LoadAll skips lines where `json.Unmarshal` fails with `slog.Warn("corrupt jsonl line", "file", path, "line", n, "err", err)`.

### 5.4 Session Resolution

Session IDs are set by the channel adapter (PRD-02). The manager is ID-agnostic.

| Source | Session ID | Behavior |
|--------|-----------|----------|
| CLI | `main:cli:local` | Always the same session. Resumes on restart. |
| Telegram DM (operator) | `main:telegram:{user_id}` | One persistent session per operator account. |
| Telegram group (future) | `group:telegram:{chat_id}` | One session per group. Sandboxed tools. |
| Discord DM (future) | `main:discord:{user_id}` | One session per user. |

**Prefix semantics:**
- `main:` prefix = full tool access (all registered tools available)
- `group:` prefix = sandboxed (Phase 2: gbrain_* tools only, no shell/code execution)

The prefix is interpreted by the tool registry and context assembler (PRD-01, PRD-04), not by the session manager itself. The session manager stores and returns sessions without restricting tool access.

**Resolution logic (Go):**

```go
func (m *SessionManager) ResolveOrCreate(ctx context.Context, sessionID string) (*Session, error) {
    // 1. In-memory cache hit — no lock needed (sync.Map)
    if v, ok := m.sessions.Load(sessionID); ok {
        return v.(*Session), nil
    }

    // 2. Load from disk
    session, err := m.loadFromDisk(sessionID)
    if err != nil {
        return nil, fmt.Errorf("loading session %s: %w", sessionID, err)
    }
    if session != nil {
        m.sessions.Store(sessionID, session)
        m.logger.InfoContext(ctx, "session loaded from disk",
            "session_id", sessionID,
            "turns", session.TurnCount())
        return session, nil
    }

    // 3. Create new session
    now := time.Now().UTC()
    session = &Session{
        SessionID: sessionID,
        CreatedAt: now,
        UpdatedAt: now,
        Turns:     make([]SessionTurn, 0, 8),
    }
    // Store with LoadOrStore to handle a race where two goroutines create
    // the same session ID simultaneously (e.g. rapid Telegram retries).
    actual, loaded := m.sessions.LoadOrStore(sessionID, session)
    if loaded {
        return actual.(*Session), nil
    }
    m.logger.InfoContext(ctx, "new session created", "session_id", sessionID)
    return session, nil
}
```

### 5.5 Context Window Management

`GetContextTurns` converts session history into the slice passed to the LLM caller. The LLM caller (PRD-01) converts turns to Claude API `messages` format.

**Phase 1 — turn count limit (simple):**

```go
func (m *SessionManager) GetContextTurns(session *Session) []SessionTurn {
    session.mu.RLock()
    defer session.mu.RUnlock()

    if len(session.Turns) <= m.config.MaxTurnsInContext {
        // Return a copy to avoid data races if the caller iterates while
        // AddTurn runs concurrently.
        result := make([]SessionTurn, len(session.Turns))
        copy(result, session.Turns)
        return result
    }
    start := len(session.Turns) - m.config.MaxTurnsInContext
    result := make([]SessionTurn, m.config.MaxTurnsInContext)
    copy(result, session.Turns[start:])
    return result
}
```

**Phase 2 (Tier 2) — token budget:**

Walk turns newest-to-oldest. Estimate tokens via `len(turnJSON)/4` (rough chars-to-tokens heuristic). Stop when the running total would exceed `MaxContextTokens`. Return the accumulated slice in chronological order.

```go
// Phase 2 variant — replaces the Phase 1 implementation above.
func (m *SessionManager) GetContextTurns(session *Session) []SessionTurn {
    session.mu.RLock()
    defer session.mu.RUnlock()

    budget := m.config.MaxContextTokens
    selected := make([]SessionTurn, 0, m.config.MaxTurnsInContext)

    for i := len(session.Turns) - 1; i >= 0; i-- {
        b, _ := json.Marshal(session.Turns[i])
        cost := len(b) / 4
        if budget-cost < 0 {
            break
        }
        budget -= cost
        selected = append(selected, session.Turns[i])
        if len(selected) >= m.config.MaxTurnsInContext {
            break
        }
    }

    // Reverse to restore chronological order.
    slices.Reverse(selected)
    return selected
}
```

**Phase 2 compaction (triggered by hook, not by GetContextTurns):**

When `session.TurnCount() > MaxTurnsBeforeCompaction`:
1. Run memory flush: extract facts from oldest 50% of turns to gbrain (PRD-03).
2. Summarise the flushed turns into a paragraph (Haiku call, ~12x cheaper).
3. Prepend the summary as a synthetic exchange at the start of context — this is injected by the LLM caller, not stored as a real `SessionTurn`.
4. Drop the raw turns that were summarised from `session.Turns`.
5. Write a `{"type":"compaction","summary":"...","turns_compacted":N}` line to the JSONL file.

The summary string lives in memory only (not on the `Session` struct in Phase 1). In Phase 2, add `CompactedSummary string` to `Session` and persist it in the compaction JSONL line.

### 5.6 Post-Turn Hooks

Post-turn hooks are functions that run after every agent response. They are fire-and-forget — failures never propagate to the main loop.

```go
// core/hooks.go

// HookFunc is the signature every hook must implement.
type HookFunc func(ctx context.Context, session *Session, turn SessionTurn) error

// RunHooks executes all hooks concurrently using errgroup.
// The context has a 5-second deadline: hooks that exceed it are cancelled.
// Errors are logged via slog.Error and discarded — the caller is never aware.
func RunHooks(ctx context.Context, hooks []HookFunc, session *Session, turn SessionTurn) {
    hookCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()

    g, gCtx := errgroup.WithContext(hookCtx)
    for _, h := range hooks {
        h := h // capture loop variable
        g.Go(func() error {
            if err := h(gCtx, session, turn); err != nil {
                slog.ErrorContext(gCtx, "post-turn hook failed",
                    "hook", runtime.FuncForPC(reflect.ValueOf(h).Pointer()).Name(),
                    "session_id", session.SessionID,
                    "turn_id", turn.TurnID,
                    "err", err)
            }
            return nil // always nil — errors are logged, not returned
        })
    }
    _ = g.Wait() // error is always nil by construction above
}
```

**Why `errgroup` instead of plain goroutines:** `errgroup` with a shared context ensures all hooks respect the 5-second deadline and the parent goroutine waits for all of them to finish before returning. Plain `go func()` would leak goroutines if the caller proceeds immediately.

**Built-in hooks (Phase 1):**

```go
// PersistSessionHook appends the latest turn to the session's JSONL file.
func PersistSessionHook(sm *SessionManager) HookFunc {
    return func(ctx context.Context, session *Session, turn SessionTurn) error {
        return sm.Persist(session, turn)
    }
}

// LogMetricsHook appends a metrics record to logs/tool-calls.jsonl.
// Uses the same append-only JSONL pattern as session files.
func LogMetricsHook(logPath string) HookFunc {
    // File handle opened once via sync.Once at construction time.
    var (
        once sync.Once
        f    *os.File
        fErr error
    )
    return func(ctx context.Context, session *Session, turn SessionTurn) error {
        once.Do(func() {
            f, fErr = os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
        })
        if fErr != nil {
            return fmt.Errorf("open metrics log: %w", fErr)
        }

        record := struct {
            Timestamp     string     `json:"timestamp"`
            SessionID     string     `json:"session_id"`
            TurnID        string     `json:"turn_id"`
            TokenUsage    TokenUsage `json:"token_usage"`
            LatencyMs     int64      `json:"latency_ms"`
            SkillUsed     string     `json:"skill_used,omitempty"`
            ToolCallCount int        `json:"tool_call_count"`
        }{
            Timestamp:     turn.Timestamp.Format(time.RFC3339),
            SessionID:     session.SessionID,
            TurnID:        turn.TurnID,
            TokenUsage:    turn.TokenUsage,
            LatencyMs:     turn.LatencyMs,
            SkillUsed:     turn.SkillUsed,
            ToolCallCount: len(turn.ToolCalls),
        }
        line, err := json.Marshal(record)
        if err != nil {
            return err
        }
        line = append(line, '\n')
        _, err = f.Write(line)
        return err
    }
}
```

**Future hooks (Phase 2):**

```go
// MemoryFlushHook extracts structured facts from the turn and stores in gbrain.
// Only fires when the session is not cold (recent activity with real content).
func MemoryFlushHook(brainClient BrainClient) HookFunc

// CompactionCheckHook triggers compaction when TurnCount() > MaxTurnsBeforeCompaction.
// Compaction is a multi-step operation (flush -> summarise -> truncate Turns).
func CompactionCheckHook(sm *SessionManager, brainClient BrainClient) HookFunc
```

---

## 6. Integration with Core Runtime (PRD-01)

The `HandleMessage` method in `runtime.go` wires all components together:

```go
// core/runtime.go

func (r *Runtime) HandleMessage(ctx context.Context, msg Message) error {
    // 1. Session resolution (PRD-05)
    session, err := r.sessionManager.ResolveOrCreate(ctx, msg.SessionID)
    if err != nil {
        return fmt.Errorf("resolve session: %w", err)
    }

    // 2. Context assembly (PRD-04)
    contextTurns := r.sessionManager.GetContextTurns(session)
    systemPrompt, err := r.contextAssembler.Assemble(ctx, msg, contextTurns)
    if err != nil {
        return fmt.Errorf("assemble context: %w", err)
    }

    // 3. Build Claude API messages from turn history + current user message
    apiMessages := turnsToAPIMessages(contextTurns)
    apiMessages = append(apiMessages, anthropic.UserMessage(msg.Content))

    // 4. Call LLM with streaming (PRD-01)
    start := time.Now()
    var (
        fullResponse string
        toolCalls    []ToolCallRecord
        tokenUsage   TokenUsage
    )

    adapter := r.adapters[msg.Channel]
    stream := r.llmCaller.Stream(ctx, LLMRequest{
        Messages:     apiMessages,
        SystemPrompt: systemPrompt,
        Tools:        r.toolRegistry.Definitions(),
    })

    for event := range stream.Events() {
        switch e := event.(type) {
        case TextDelta:
            fullResponse += e.Text
            adapter.SendStreamingToken(ctx, msg.SessionID, e.Text)
        case ToolUseEvent:
            rec, err := r.toolRegistry.Execute(ctx, e.ToolCall)
            if err != nil {
                slog.ErrorContext(ctx, "tool execution failed", "tool", e.ToolCall.Name, "err", err)
            }
            toolCalls = append(toolCalls, rec)
        case FinalUsage:
            tokenUsage = e.Usage
        }
    }
    if err := stream.Err(); err != nil {
        return fmt.Errorf("llm stream: %w", err)
    }

    // 5. Build turn record
    turn := SessionTurn{
        TurnID:            uuid.NewString(),
        Timestamp:         time.Now().UTC(),
        UserMessage:       msg.Content,
        AssistantResponse: fullResponse,
        ToolCalls:         toolCalls,
        TokenUsage:        tokenUsage,
        SkillUsed:         r.contextAssembler.LastSkillUsed(),
        LatencyMs:         time.Since(start).Milliseconds(),
    }

    // 6. Commit turn to in-memory session
    session.AddTurn(turn)

    // 7. Post-turn hooks — fire and forget (PRD-05)
    // RunHooks blocks for at most 5s; it never returns an error.
    RunHooks(ctx, r.hooks, session, turn)

    return nil
}
```

**`turnsToAPIMessages` helper:**

```go
func turnsToAPIMessages(turns []SessionTurn) []anthropic.MessageParam {
    msgs := make([]anthropic.MessageParam, 0, len(turns)*2)
    for _, t := range turns {
        msgs = append(msgs, anthropic.UserMessage(t.UserMessage))
        if len(t.ToolCalls) > 0 {
            // Build assistant message with tool_use blocks, then a user message
            // with the corresponding tool_result blocks. This is the format the
            // Anthropic API requires for tool call history.
            msgs = append(msgs, buildToolUseMessage(t)...)
        } else {
            msgs = append(msgs, anthropic.AssistantMessage(t.AssistantResponse))
        }
    }
    return msgs
}
```

---

## 7. Configuration

```toml
[session]
storage_dir               = "sessions"   # Relative to ~/.akb48/
max_turns_in_context      = 50           # Max turns sent to LLM (Phase 1 limit)
max_turns_before_compaction = 30         # Phase 2: triggers compaction check hook
max_file_size_mb          = 10           # Log warning if session file exceeds this
max_context_tokens        = 40000        # Phase 2: token budget for GetContextTurns
load_on_startup           = true         # Load all session files at startup
```

All fields are required. Fail fast at startup if any are missing — use a validated struct with `toml:"..."` tags and `config.Validate() error` (see PRD-01). Optional: `go-playground/validator` for declarative field constraints.

---

## 8. Error Handling

| Error | Handling | User-facing |
|-------|----------|-------------|
| Session file corrupted (invalid JSON line) | Skip corrupt line with `slog.Warn`; continue loading remaining lines | None (transparent recovery) |
| Disk full on `Persist` | Log `slog.Error`; session remains in memory until disk space is freed | None (in-memory session intact for remainder of process lifetime) |
| Session file exceeds `max_file_size_mb` | Log `slog.Warn` after `Persist`; no other action in Phase 1 | None (warning only) |
| Post-turn hook returns error | `slog.Error` with hook name, session ID, turn ID; error discarded | None (hooks never affect user response) |
| Hook exceeds 5-second deadline | Context cancelled; hook returns; `slog.Error` logged | None |
| `LoadAll` takes > 5s | Log `slog.Warn` with duration and file sizes | None (startup is slow; acceptable) |
| `ResolveOrCreate` race on new session | `sync.Map.LoadOrStore` returns the winner; loser is GC'd | None (transparent) |

---

## 9. Acceptance Criteria

- [ ] Multi-turn conversation works: agent recalls what was said earlier in the same session
- [ ] After restarting AKB48, the agent recalls previous conversation from the loaded session
- [ ] CLI and Telegram sessions are independent (different session IDs, different histories)
- [ ] Session JSONL file contains readable, complete turn records parseable with `jq`
- [ ] Corrupt JSONL lines are skipped with a `slog.Warn` (no panic, no crash)
- [ ] Post-turn hooks execute after every response
- [ ] Post-turn hook failure is logged and does not affect the response or subsequent turns
- [ ] Startup log shows: "Loaded N sessions (M total turns)"
- [ ] Session file size warning triggers at 10MB
- [ ] `go test ./core/...` passes with a session that survives a simulated restart (write turns, reinitialise manager, load from disk, assert turns match)

---

## 10. Open Questions

| ID | Question | Decision |
|----|----------|---------|
| TODO-P01 | Should tool call details (full input/output) be stored in session history? They can be large (e.g. gbrain search results). | Store tool name + output truncated to 500 chars in `ToolCallRecord.Output`. Full output goes to `tool-calls.jsonl`. Keeps session files manageable. |
| TODO-P02 | Session file rotation: new file per day, or one file per session forever? | One file per session. Phase 2: compaction controls size. |
| TODO-P03 | Should the agent see tool call history in the context? | Yes — include `tool_use`/`tool_result` content blocks. The LLM needs tool history for coherent continuity. Truncate large outputs to 500 chars when building API messages. |
| TODO-P04 | Maximum session age: should sessions expire after N days of inactivity? | No expiry. Sessions are cheap to store. The brain holds durable facts; the session holds conversation flow. |
| TODO-P05 | Should there be a `/reset` command to start a fresh session? | Phase 2 slash command. For hello world: delete the JSONL file manually. |

---

## 11. Appendix: End-to-End "Hello World" Flow

This is the complete flow when all five PRDs are implemented:

```
1. Operator runs: go run ./cmd/akb48/
   ├── config.toml loaded and validated (PRD-01)
   ├── LLM caller initialised with Anthropic SDK (PRD-01)
   ├── Tool registry created (PRD-01)
   ├── GBrain MCP server started, tools discovered and registered (PRD-03)
   ├── Identity files loaded: AGENTS.md, SOUL.md, USER.md (PRD-04)
   ├── Skills loaded: note-capture, research (PRD-04)
   ├── SessionManager.LoadAll() — active sessions loaded from disk (PRD-05)
   ├── CLI adapter started (PRD-02)
   ├── Telegram adapter started, long-polling (PRD-02)
   └── slog.Info: "AKB48 started. Adapters: CLI, Telegram. Brain: connected (32 tools). Skills: 2."

2. Operator sends via Telegram: "Hello, who are you?"
   ├── Telegram adapter receives update (PRD-02)
   ├── Message normalised to Message{SessionID: "main:telegram:123456789", ...} (PRD-02)
   ├── SessionManager.ResolveOrCreate() — new Session created (PRD-05)
   ├── Skill resolver: no match -> general mode (PRD-04)
   ├── Context assembled: AGENTS.md + SOUL.md + USER.md (PRD-04)
   ├── LLM called with context + message (PRD-01)
   ├── Response streamed via editMessageText (PRD-02)
   ├── session.AddTurn(turn)
   ├── RunHooks() -> PersistSessionHook writes main_telegram_123456789.jsonl (PRD-05)
   ├── RunHooks() -> LogMetricsHook writes tool-calls.jsonl (PRD-05)
   └── Agent: "I'm AKB48, your personal AI agent. I have access to a knowledge
        brain and can research, remember things, and help you think through problems.
        What are you working on?"

3. Operator sends: "Remember that our staging cluster is ap-southeast-1"
   ├── Skill resolver: "remember" matches note-capture (PRD-04)
   ├── Context assembled: identity + note-capture skill (PRD-04)
   ├── GetContextTurns() returns [turn 1] (PRD-05)
   ├── LLM called -> decides to invoke gbrain_put tool (PRD-01, PRD-03)
   ├── Tool executed: gbrain_put(title="Staging Cluster", ...) (PRD-03)
   ├── LLM receives tool result, generates confirmation
   ├── Response: "Stored. Staging cluster is in ap-southeast-1."
   └── Turn persisted with ToolCallRecord to JSONL (PRD-05)

4. Operator restarts AKB48 (kill + restart)
   ├── LoadAll() reads main_telegram_123456789.jsonl: 2 turns loaded (PRD-05)
   └── GBrain reconnected (PRD-03)

5. Operator sends: "What do you know about our staging cluster?"
   ├── GetContextTurns() returns both prior turns (PRD-05)
   ├── LLM called -> decides to invoke gbrain_search("staging cluster") (PRD-03)
   ├── GBrain returns the stored page
   ├── Response: "Your staging cluster is in ap-southeast-1."
   └── Hello World complete: chat -> brain -> persistence -> recall works end-to-end.
```
