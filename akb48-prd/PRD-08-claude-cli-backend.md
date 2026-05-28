# PRD-08: Claude CLI Backend — Subprocess LLM Provider

**Status:** Draft v1.0  
**Parent:** PRD-00 (AKB48 Master PRD), PRD-01 (Core Runtime)  
**Author:** Dandi  
**Created:** 2026-05-27  
**Dependencies:** PRD-01 (Core Runtime — LLM caller, config, runtime wiring)  
**Estimated Effort:** 5–7 days  
**SDLC Class:** `class:medium` — spec in issue body, self `/approve-spec`, `go test ./...`  
**Ticket:** KLW-031  
**Research:** `akb48-rsh/06-akb48-claude-cli-backend.md`

---

## 1. Problem

AKB48 currently requires `ANTHROPIC_API_KEY` — a Console pay-per-use credential. Many operators already have a **Claude Pro/Max subscription** with `claude` CLI installed and authenticated via `claude auth login`. There is no way to use that existing subscription to power AKB48.

The two operator types need different on-ramps:

| Operator type | Current path | Desired path |
|---|---|---|
| Console API key user | Set `ANTHROPIC_API_KEY` → works | No change |
| Pro/Max subscriber, no Console key | Blocked | `claude-cli` backend via subprocess |

OAuth token extraction (reading `~/.claude/.credentials.json` and calling the API directly) is **not a valid path** — Anthropic blocked it February 20, 2026. The only ToS-safe route is the subprocess model.

---

## 2. Goals

- **G1:** Operator with `claude` CLI authenticated can run AKB48 with `llm.backend = "claude-cli"` — no `ANTHROPIC_API_KEY` required
- **G2:** `akb48 --onboard` detects auth state and guides the operator to the correct backend config
- **G3:** CLI backend produces streaming token output indistinguishable from the API backend at the adapter layer
- **G4:** API backend (`llm.backend = "api"`) is unchanged — no regressions
- **G5:** `claude` binary absent → non-fatal startup warning; api-mode continues; cli-mode returns a user-visible error
- **G6:** Session history and identity context are injected via `--append-system-prompt` per turn — the subprocess is stateless
- **G7:** `CLILLMClient` adapter allows internal single-shot calls (e.g. future extraction) to go through the CLI executor when no API key is available

---

## 3. Non-Goals

- Direct OAuth token extraction or calling Anthropic API with OAuth credentials — blocked as of Feb 20, 2026; do not implement
- Long-lived subprocess kept alive across turns (persistent stdin/stdout process) — per-turn spawn only
- `--resume` flag usage — AKB48 owns session state; resuming via CLI session ID would re-execute previous tool calls
- Running both API and CLI backends simultaneously (e.g. routing by model)
- Refreshing `CLAUDE_CODE_OAUTH_TOKEN` programmatically — operator obtains it via `claude setup-token` once
- Rate limit budgeting across Pro/Max monthly pool — out of scope
- Path C (OAuth extraction) from RSH-06 — dead end, will not be implemented

---

## 4. User Stories

| ID | Story | Acceptance Criteria |
|----|-------|---------------------|
| US-C01 | As an operator with Claude Pro, I run `akb48 --onboard` and get instructed to set `backend = "claude-cli"` | `--onboard` runs `claude auth status`; if authenticated prints config instructions and `claude setup-token` guidance |
| US-C02 | As an operator, I set `backend = "claude-cli"` in config and send a message — I get a streaming response | Response streams token-by-token through Discord/Telegram adapter; session persists to JSONL normally |
| US-C03 | As an operator, my session history is available to the LLM in cli-mode | Last N turns injected via `--append-system-prompt`; LLM can reference prior turns in same session |
| US-C04 | As an operator, I have `claude` binary but it is not authenticated | CLI backend returns error: "claude auth required — run `claude auth login` or set `CLAUDE_CODE_OAUTH_TOKEN`" |
| US-C05 | As an operator, `claude` binary is absent and `backend = "claude-cli"` | Startup logs warning; first message returns "cli backend unavailable (claude binary missing?)" |
| US-C06 | As an operator using API key, I change nothing and AKB48 behaves exactly as before | All existing tests pass; no config changes needed; `backend` defaults to `"api"` |
| US-C07 | As an operator, I run `akb48 --onboard` and have no `claude` CLI installed | `--onboard` detects missing binary, prints instructions to install Claude Code CLI |

---

## 5. Technical Design

### 5.1 Config Changes

**File:** `internal/config/config.go`

Add `Backend` field to `LLMConfig`:

```go
type LLMConfig struct {
    Backend           string `toml:"backend" validate:"oneof=api claude-cli"`
    Model             string `toml:"model"`
    ExtractionModel   string `toml:"extraction_model"`
    APIKeyEnv         string `toml:"api_key_env"`
    MaxToolRounds     int    `toml:"max_tool_rounds"`
    MaxToolResultTokens int  `toml:"max_tool_result_tokens"`
}
```

Default `Backend = "api"` if unset (validated in startup config check).

**`config.toml` addition:**

```toml
[llm]
backend = "api"           # "api" (default) | "claude-cli"
model   = "claude-sonnet-4-6"
extraction_model = "claude-haiku-4-5-20251001"
api_key_env = "ANTHROPIC_API_KEY"   # required for "api" mode; optional for "claude-cli"
```

Validation rule: if `backend = "api"`, `api_key_env` must be non-empty. If `backend = "claude-cli"`, `api_key_env` is optional (injected into subprocess env if set, otherwise CLI OAuth auth is used).

---

### 5.2 New Package: `internal/llm/claudecli`

**File:** `internal/llm/claudecli/executor.go`

All new code lives in this package. Nothing in existing `internal/llm/llm.go` is modified in this phase.

#### 5.2.1 Types

```go
// CLIRequest is the input for a claude CLI agent turn.
// The runtime owns conversation context: history injected via AppendSystemPrompt.
// No --resume; each invocation is stateless.
type CLIRequest struct {
    Prompt             string
    SystemPrompt       string   // full agent system prompt (sent every turn)
    AppendSystemPrompt string   // dynamic context: session history + memory facts
    Model              string   // bare model name, e.g. "claude-sonnet-4-6"
    MaxTurns           int      // 0 = claude default
    AllowedTools       []string // claude built-in tools: ["Bash","Read"]; nil = no built-in tools
    MCPConfig          string   // JSON string for --mcp-config; empty = no MCP
}

// CLIEvent is one streamed output from the claude subprocess.
type CLIEvent struct {
    Type    string  // "text" | "done" | "error"
    Content string  // non-empty for "text"
    CostUSD float64 // non-zero for "done"
    Err     error   // non-nil for "error"
}

// CLIExecutor spawns a claude subprocess per turn and streams CLIEvents.
// New returns (nil, error) when the binary is absent — callers treat nil as non-fatal.
type CLIExecutor interface {
    Execute(ctx context.Context, req CLIRequest) (<-chan CLIEvent, error)
}
```

#### 5.2.2 Executor

```go
type executor struct {
    binPath string
    // Auth via CLAUDE_CODE_OAUTH_TOKEN env var or ~/.claude/ credentials.
    // No API key injection — avoids conflict with ANTHROPIC_API_KEY in parent env.
}

// New returns CLIExecutor. Returns error if claude binary not in PATH.
// Callers treat (nil, err) as non-fatal: api-mode continues without CLI.
func New() (CLIExecutor, error) {
    path, err := exec.LookPath("claude")
    if err != nil {
        return nil, fmt.Errorf("claude binary not found in PATH: %w", err)
    }
    return &executor{binPath: path}, nil
}

func (e *executor) Execute(ctx context.Context, req CLIRequest) (<-chan CLIEvent, error) {
    args := buildArgs(req)
    cmd := exec.CommandContext(ctx, e.binPath, args...) // #nosec G204
    // Strip ANTHROPIC_API_KEY — prevents conflict when CLAUDE_CODE_OAUTH_TOKEN is set.
    // CLAUDE_CODE_OAUTH_TOKEN is inherited naturally from os.Environ().
    base := os.Environ()
    filtered := make([]string, 0, len(base))
    for _, v := range base {
        if !strings.HasPrefix(v, "ANTHROPIC_API_KEY=") {
            filtered = append(filtered, v)
        }
    }
    cmd.Env = filtered
    cmd.Dir = os.TempDir() // prevents subprocess reading local CLAUDE.md / hooks

    stdout, err := cmd.StdoutPipe()
    if err != nil {
        return nil, fmt.Errorf("stdout pipe: %w", err)
    }
    var stderrBuf bytes.Buffer
    cmd.Stderr = &stderrBuf

    if err := cmd.Start(); err != nil {
        return nil, fmt.Errorf("start claude: %w", err)
    }

    ch := make(chan CLIEvent, 64)
    go func() {
        defer close(ch)
        var lastMsgID string
        var lastTextLen int
        var doneEmitted bool

        scanner := bufio.NewScanner(stdout)
        scanner.Buffer(make([]byte, 1<<20), 1<<20) // 1MB — default 64KB truncates silently

        for scanner.Scan() {
            evt, ok := parseStreamLine(scanner.Bytes(), &lastMsgID, &lastTextLen)
            if ok {
                if evt.Type == "done" || evt.Type == "error" {
                    doneEmitted = true
                }
                select {
                case ch <- evt:
                case <-ctx.Done():
                    _ = cmd.Wait()
                    return
                }
            }
        }

        if err := cmd.Wait(); err != nil && !doneEmitted {
            exitCode := -1
            var exitErr *exec.ExitError
            if errors.As(err, &exitErr) {
                exitCode = exitErr.ExitCode()
            }
            select {
            case ch <- CLIEvent{
                Type: "error",
                Err:  fmt.Errorf("claude exited %d: %s", exitCode, strings.TrimSpace(stderrBuf.String())),
            }:
            case <-ctx.Done():
            }
        }
    }()
    return ch, nil
}
```

#### 5.2.3 Args Builder

```go
func buildArgs(req CLIRequest) []string {
    args := []string{
        "--print",
        "--verbose",
        "--output-format", "stream-json",
        "--include-partial-messages",
        "--permission-mode", "bypassPermissions",
    }
    if req.Model != "" {
        args = append(args, "--model", req.Model)
    }
    if req.SystemPrompt != "" {
        args = append(args, "--system-prompt", req.SystemPrompt)
    }
    if req.MaxTurns > 0 {
        args = append(args, "--max-turns", strconv.Itoa(req.MaxTurns))
    }
    if len(req.AllowedTools) > 0 {
        args = append(args, "--tools", strings.Join(req.AllowedTools, ","))
    } else {
        args = append(args, "--tools", "") // explicit empty prevents CLI defaulting to built-in tools
    }
    if req.MCPConfig != "" {
        args = append(args, "--mcp-config", req.MCPConfig)
    }
    if req.AppendSystemPrompt != "" {
        args = append(args, "--append-system-prompt", req.AppendSystemPrompt)
    }
    args = append(args, "--", req.Prompt) // "--" separates flags from user prompt
    return args
}
```

**Do not add `--resume`.** AKB48 owns session state via `--append-system-prompt`. `--resume` would replay previous tool calls and corrupt turn state.

#### 5.2.4 Stream Line Parser

With `--include-partial-messages`, each `assistant` event carries **cumulative text**, not a delta. State is tracked across calls:

```go
type cliMessage struct {
    ID      string `json:"id"`
    Content []struct {
        Type string `json:"type"`
        Text string `json:"text"`
    } `json:"content"`
}

func parseStreamLine(line []byte, lastMsgID *string, lastTextLen *int) (CLIEvent, bool) {
    var raw struct {
        Type    string      `json:"type"`
        Subtype string      `json:"subtype"`
        Message *cliMessage `json:"message"`
        CostUSD float64     `json:"cost_usd"`
    }
    if err := json.Unmarshal(line, &raw); err != nil {
        return CLIEvent{}, false
    }

    switch raw.Type {
    case "assistant":
        if raw.Message == nil {
            return CLIEvent{}, false
        }
        var full strings.Builder
        for _, block := range raw.Message.Content {
            if block.Type == "text" {
                full.WriteString(block.Text)
            }
        }
        fullText := full.String()

        if raw.Message.ID != *lastMsgID {
            *lastMsgID = raw.Message.ID
            *lastTextLen = 0
        }
        // text shrinks when a tool_use block replaces a partial text block; reset position
        if *lastTextLen > len(fullText) {
            *lastTextLen = 0
        }
        delta := fullText[*lastTextLen:]
        *lastTextLen = len(fullText)

        if delta != "" {
            return CLIEvent{Type: "text", Content: delta}, true
        }

    case "result":
        switch raw.Subtype {
        case "success":
            return CLIEvent{Type: "done", CostUSD: raw.CostUSD}, true
        default:
            return CLIEvent{
                Type: "error",
                Err:  fmt.Errorf("claude result: %s", raw.Subtype),
            }, true
        }
    }

    return CLIEvent{}, false // system:init, tool_use, other types silently ignored
}
```

#### 5.2.5 CLILLMClient

For future internal single-shot calls (fact extraction, summarization) when no API key is available:

```go
// CLILLMClient wraps CLIExecutor to satisfy a single-call LLM interface.
// MaxTurns is hardcoded to 1 — not suitable for agentic loops.
type CLILLMClient struct {
    executor CLIExecutor
}

func NewCLILLMClient(exec CLIExecutor) *CLILLMClient {
    return &CLILLMClient{executor: exec}
}

// Call runs a single prompt turn and returns the full response text.
func (c *CLILLMClient) Call(ctx context.Context, systemPrompt, userPrompt, model string) (string, error) {
    // strip provider prefix if present: "anthropic:claude-haiku-4-5" → "claude-haiku-4-5"
    if idx := strings.Index(model, ":"); idx >= 0 {
        model = model[idx+1:]
    }
    ch, err := c.executor.Execute(ctx, CLIRequest{
        Prompt:       userPrompt,
        SystemPrompt: systemPrompt,
        Model:        model,
        MaxTurns:     1,
    })
    if err != nil {
        return "", err
    }
    var sb strings.Builder
    for evt := range ch {
        switch evt.Type {
        case "text":
            sb.WriteString(evt.Content)
        case "error":
            return "", evt.Err
        }
    }
    return sb.String(), nil
}
```

---

### 5.3 Runtime Wiring

**File:** `internal/runtime/runtime.go`

```go
// At startup, after config load:
var cliExec claudecli.CLIExecutor
if cfg.LLM.Backend == "claude-cli" {
    var err error
    cliExec, err = claudecli.New()
    if err != nil {
        slog.Warn("CLIExecutor unavailable — cli-mode messages will return errors", "error", err)
        // cliExec = nil; handled at call site
    }
}
```

Auth is provided by `CLAUDE_CODE_OAUTH_TOKEN` in the environment (set in `.env` for Docker, or via `claude auth login` on Linux). The executor strips `ANTHROPIC_API_KEY` from the subprocess env to prevent conflicts.

In the message handler, switch on backend:

```go
func (r *Runtime) handleMessage(ctx context.Context, msg types.Message) (types.Response, error) {
    switch r.cfg.LLM.Backend {
    case "claude-cli":
        return r.handleCLI(ctx, msg)
    default:
        return r.handleAPI(ctx, msg)
    }
}

func (r *Runtime) handleCLI(ctx context.Context, msg types.Message) (types.Response, error) {
    if r.cliExec == nil {
        return types.Response{}, fmt.Errorf("cli backend unavailable (claude binary missing?)")
    }
    appendCtx := r.buildAppendContext(msg.SessionID)
    ch, err := r.cliExec.Execute(ctx, claudecli.CLIRequest{
        Prompt:             msg.Text,
        SystemPrompt:       r.systemPrompt(),
        AppendSystemPrompt: appendCtx,
        Model:              r.cfg.LLM.Model,
    })
    if err != nil {
        return types.Response{}, err
    }
    return r.drainCLIEvents(ch)
}
```

`buildAppendContext` formats the last N session turns as markdown — same data the API path passes in `messages[]`, just formatted differently for `--append-system-prompt`.

---

### 5.4 Context Injection via `--append-system-prompt`

**Why not `--resume`:** `--resume` re-runs previous tool calls inside the subprocess. AKB48 owns session state in JSONL and must remain the source of truth. Each subprocess turn is stateless.

**Format for injected history:**

```
## Conversation history

[user] can you check the build status?
[assistant] Sure — running `go build ./...` now. No errors found.

[user] what about tests?
[assistant] All 42 tests pass in 1.2s.
```

Last 20 turns. Assistant turns truncated to 500 chars if longer to keep `--append-system-prompt` under 4KB.

**File:** `internal/runtime/context.go` (new helper, ~50 lines)

```go
func buildAppendContext(history []types.Turn, maxTurns int) string {
    if len(history) == 0 {
        return ""
    }
    start := len(history) - maxTurns
    if start < 0 {
        start = 0
    }
    var sb strings.Builder
    sb.WriteString("## Conversation history\n\n")
    for _, t := range history[start:] {
        assistant := t.AssistantText
        if len(assistant) > 500 {
            assistant = assistant[:500] + "…"
        }
        fmt.Fprintf(&sb, "[user] %s\n[assistant] %s\n\n", t.UserText, assistant)
    }
    return strings.TrimRight(sb.String(), "\n")
}
```

---

### 5.5 Onboard Command

**Flag:** `akb48 --onboard`

**File:** `cmd/akb48/main.go` — add `--onboard` flag handling before `runtime.Start()`.

```go
if *onboard {
    runOnboard()
    os.Exit(0)
}
```

**File:** `internal/onboard/onboard.go`

```go
func Run() {
    // 1. Check claude binary
    claudePath, err := exec.LookPath("claude")
    if err != nil {
        fmt.Println("claude binary not found in PATH.")
        fmt.Println("Install Claude Code CLI: https://claude.ai/download")
        return
    }
    fmt.Printf("Found claude at %s\n", claudePath)

    // 2. Check auth status
    out, err := exec.Command(claudePath, "auth", "status", "--json").Output()
    if err != nil {
        fmt.Println("Not authenticated. Run: claude auth login")
        return
    }

    var status struct {
        LoggedIn bool   `json:"loggedIn"`
        Email    string `json:"email"`
    }
    _ = json.Unmarshal(out, &status)

    if !status.LoggedIn {
        fmt.Println("Not authenticated. Run: claude auth login")
        return
    }

    // 3. Guide operator
    fmt.Printf("Authenticated as %s\n\n", status.Email)
    fmt.Println("To use Claude CLI as AKB48 backend, set in config.toml:")
    fmt.Println()
    fmt.Println("  [llm]")
    fmt.Println(`  backend = "claude-cli"`)
    fmt.Println()
    fmt.Println("For a long-lived token (recommended for scripted use):")
    fmt.Println("  claude setup-token")
    fmt.Println("  export CLAUDE_CODE_OAUTH_TOKEN=<token from above>")
    fmt.Println()
    fmt.Println("Then start AKB48 normally: akb48")
}
```

---

## 6. Error Handling

| Scenario | Behavior |
|---|---|
| `claude` binary missing, `backend = "claude-cli"` | Startup: `WARN CLIExecutor unavailable`. First message: returns error string to user. AKB48 does not crash. |
| `claude` authenticated but rate-limited | Subprocess exits non-zero; stderr captured; error returned to user as "claude exited 1: rate limit exceeded". |
| `claude` not authenticated, `backend = "claude-cli"` | Subprocess exits with auth error message in stderr; surfaced to user. |
| Subprocess crash without `result` event | `cmd.Wait()` error emitted as `CLIEvent{Type:"error"}` after scanner drains. |
| Context cancelled mid-stream | Scanner loop exits; `cmd.Wait()` called; channel closed. |
| `backend = "api"` with missing `ANTHROPIC_API_KEY` | Existing behavior — fail at startup config validation. |
| `--append-system-prompt` exceeds subprocess arg limit | Truncate history to 10 turns with warning log. Platform arg limit ~2MB; 4KB history is well within bounds. |

---

## 7. Testing

### Unit Tests — `internal/llm/claudecli/`

| Test | Coverage |
|---|---|
| `TestBuildArgs_NoTools` | `--tools ""` present when `AllowedTools` nil |
| `TestBuildArgs_WithTools` | `--tools Bash,Read` when `AllowedTools = ["Bash","Read"]` |
| `TestBuildArgs_AppendSystemPrompt` | flag appears when field non-empty |
| `TestBuildArgs_NoResume` | `--resume` absent from args in all cases |
| `TestParseStreamLine_TextDelta` | incremental delta extraction across 3 events |
| `TestParseStreamLine_Shrink` | shrink guard resets `lastTextLen` |
| `TestParseStreamLine_NewMessageID` | `lastTextLen` resets on new ID |
| `TestParseStreamLine_ResultSuccess` | `CLIEvent{Type:"done"}` with `CostUSD` |
| `TestParseStreamLine_ResultError` | `CLIEvent{Type:"error"}` for non-success subtype |
| `TestParseStreamLine_UnknownType` | returns `(CLIEvent{}, false)` |
| `TestParseStreamLine_MalformedJSON` | returns `(CLIEvent{}, false)` |

### Integration Test — subprocess with stub

```go
// TestExecute_StubClaude: write a tiny shell script to tmp, set binPath, feed known NDJSON via stdout
// Verify: delta events emitted in order, done event closes channel
```

### Onboard Tests — `internal/onboard/`

| Test | Coverage |
|---|---|
| `TestOnboard_BinaryMissing` | correct error printed |
| `TestOnboard_NotAuthenticated` | `claude auth login` guidance printed |
| `TestOnboard_Authenticated` | config instructions + setup-token guidance printed |

---

## 8. Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-01 | `go build ./...` passes after all changes |
| AC-02 | `go test ./internal/llm/claudecli/... ./internal/onboard/...` passes |
| AC-03 | `backend = "api"` config: all existing tests pass, no behavioral change |
| AC-04 | `backend = "claude-cli"` with `claude` binary absent: startup log `WARN CLIExecutor unavailable`; sending message returns error string to user; process does not crash |
| AC-05 | `backend = "claude-cli"` with authenticated `claude`: message receives streaming response; session persists to JSONL normally |
| AC-06 | Streaming tokens reach the Discord/Telegram adapter via the same `chan string` interface — no adapter changes required |
| AC-07 | `TestBuildArgs_NoResume` passes — `--resume` never in args |
| AC-08 | `TestParseStreamLine_Shrink` passes — shrink guard works |
| AC-09 | `akb48 --onboard` with missing binary prints install URL and exits 0 |
| AC-10 | `akb48 --onboard` with authenticated CLI prints config snippet and `claude setup-token` guidance, exits 0 |
| AC-11 | `cmd.Dir = os.TempDir()` verified in executor — subprocess cannot read local `CLAUDE.md` |

---

## 9. Implementation Tickets

| Ticket | Title | Effort | Gate |
|---|---|---|---|
| **KLW-031** | PRD-08 Config + CLIExecutor + stream parser | 3 SP | Self-approval |
| **KLW-032** | PRD-08 Runtime wiring + context injection | 2 SP | Self-approval |
| **KLW-033** | PRD-08 Onboard command | 1 SP | Self-approval |
| **KLW-034** | PRD-08 Unit + integration tests | 2 SP | CI gate |

All on branch `feature/klw-031-claude-cli-backend`. PR must close KLW-031. `go test ./...` must pass before merge.

---

## 10. Security Considerations

- **ToS compliance:** Subprocess only. No OAuth token extraction. `CLAUDE_CODE_OAUTH_TOKEN` is fed as env var into the subprocess — it never touches `anthropic-sdk-go` or the Anthropic API directly.
- **`cmd.Dir = os.TempDir()`:** Critical. Prevents subprocess from auto-loading project `CLAUDE.md`, hooks, or skill files. Subprocess is fully stateless from Claude's perspective — context comes only from flags.
- **`#nosec G204`:** `binPath` is resolved via `exec.LookPath("claude")` at startup, not from user input. Operator-controlled, not attacker-controlled.
- **Subprocess env isolation:** `cmd.Env` is built from `os.Environ()` with `ANTHROPIC_API_KEY` stripped. `CLAUDE_CODE_OAUTH_TOKEN` is inherited naturally — never remapped to `ANTHROPIC_API_KEY`. No secrets written to disk.

---

## 11. Dependencies

| Component | Requirement |
|---|---|
| PRD-01 (Core Runtime) | Config struct, runtime.go wiring, `types.Turn` for history |
| `claude` CLI binary | `>=` any recent version; `auth status --json` must be supported |
| No new Go dependencies | All stdlib (`os/exec`, `bufio`, `bytes`, `strings`, `encoding/json`) |
