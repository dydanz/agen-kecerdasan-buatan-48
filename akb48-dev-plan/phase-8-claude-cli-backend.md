# Phase 8: Claude CLI Backend — Subprocess LLM Provider

**PRD:** `akb48-prd/PRD-08-claude-cli-backend.md`  
**Research:** `akb48-rsh/06-akb48-claude-cli-backend.md`  
**Goal:** Allow operators with a Claude Pro/Max subscription to run AKB48 without `ANTHROPIC_API_KEY` by spawning `claude` CLI as a subprocess per turn. API backend unchanged.

**Definition of Done:**
- `backend = "api"` (default): zero behavioural change, all existing tests pass
- `backend = "claude-cli"`, binary absent: startup `WARN`, first message returns error to user, process does not crash
- `backend = "claude-cli"`, binary present + authenticated: message receives streaming response, session persists to JSONL
- `backend = "claude-cli"`, `api_key_env` not set in config: startup succeeds (not a validation error)
- Streaming tokens flow through same `chan string` interface — no adapter changes required
- `akb48 --onboard` with binary absent: prints install URL, exits 0
- `akb48 --onboard` with authenticated CLI: prints `backend = "claude-cli"` config snippet + `setup-token` guidance, exits 0
- `--resume` never appears in subprocess args (verified by test)
- `go test ./...` passes

**Tickets:** KLW-031 → KLW-032 → KLW-033 → KLW-034

---

## KLW-031 — Config changes + `internal/llm/claudecli` package

**Type:** Feature  
**Effort:** 3 SP  
**Labels:** `phase/8`, `type/feature`, `size/M`, `component:config`, `component:runtime`  
**Dependencies:** none  
**Branch:** `feature/klw-031-claude-cli-backend`

### User Story

> As an operator with Claude Pro, I want to set `backend = "claude-cli"` in config and have AKB48 start successfully without `ANTHROPIC_API_KEY` set.

### Implementation Plan

#### 1. `internal/config/config.go` — add `Backend` field, fix validation

Add `Backend` to `LLMConfig`:

```go
type LLMConfig struct {
    Backend             string `toml:"backend"`
    Model               string `toml:"model"`
    ExtractionModel     string `toml:"extraction_model"`
    APIKeyEnv           string `toml:"api_key_env"`
    MaxTokens           int    `toml:"max_tokens"`
    MaxToolRounds       int    `toml:"max_tool_rounds"`
    MaxToolResultTokens int    `toml:"max_tool_result_tokens"`
}
```

In `applyDefaults()` — add after the existing LLM defaults:

```go
if c.LLM.Backend == "" {
    c.LLM.Backend = "api"
}
```

In `validate()` — wrap the API key checks so they only fire in `api` mode:

```go
func (c *Config) validate() error {
    if c.LLM.Model == "" {
        return fmt.Errorf("llm.model is required")
    }
    if c.LLM.Backend != "claude-cli" {
        if c.LLM.APIKeyEnv == "" {
            return fmt.Errorf("llm.api_key_env is required")
        }
        if os.Getenv(c.LLM.APIKeyEnv) == "" {
            return fmt.Errorf("env var %q (llm.api_key_env) is not set", c.LLM.APIKeyEnv)
        }
    }
    return nil
}
```

#### 2. `config.toml` + `deploy/config.docker.toml`

Add under `[llm]`:
```toml
backend = "api"   # "api" | "claude-cli"
```

#### 3. New package: `internal/llm/claudecli/executor.go`

Full file:

```go
package claudecli

import (
    "bufio"
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "os/exec"
    "strconv"
    "strings"
)

// CLIRequest is the input for a claude CLI agent turn.
// The runtime owns conversation context: history injected via AppendSystemPrompt.
// Never use --resume; each invocation is stateless.
type CLIRequest struct {
    Prompt             string
    SystemPrompt       string
    AppendSystemPrompt string   // session history + memory; injected every turn
    Model              string   // bare model name, e.g. "claude-sonnet-4-6"
    MaxTurns           int      // 0 = claude default
    AllowedTools       []string // claude built-in tools; nil = no built-in tools
    MCPConfig          string   // JSON for --mcp-config; empty = no MCP
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

type executor struct {
    binPath string
    // Auth via CLAUDE_CODE_OAUTH_TOKEN env var or ~/.claude/ credentials.
    // No API key injection — avoids conflict with ANTHROPIC_API_KEY in parent env.
}

// New returns a CLIExecutor. Returns error if claude is not in PATH.
// Caller must treat (nil, err) as non-fatal: api-mode continues without CLI.
func New() (CLIExecutor, error) {
    path, err := exec.LookPath("claude")
    if err != nil {
        return nil, fmt.Errorf("claude binary not found in PATH: %w", err)
    }
    return &executor{binPath: path}, nil
}

func (e *executor) Execute(ctx context.Context, req CLIRequest) (<-chan CLIEvent, error) {
    args := buildArgs(req)
    cmd := exec.CommandContext(ctx, e.binPath, args...) // #nosec G204 — binPath from LookPath
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
    cmd.Dir = os.TempDir() // CRITICAL: prevents subprocess reading local CLAUDE.md / hooks

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
        scanner.Buffer(make([]byte, 1<<20), 1<<20) // 1MB — default 64KB silently truncates

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
    args = append(args, "--", req.Prompt) // "--" separates flags from prompt
    return args
}

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
        // text shrinks when a tool_use block replaces a partial text block
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
            return CLIEvent{Type: "error", Err: fmt.Errorf("claude result: %s", raw.Subtype)}, true
        }
    }

    return CLIEvent{}, false // system:init, tool_use, other types silently ignored
}
```

#### 4. New file: `internal/llm/claudecli/client.go`

```go
package claudecli

import (
    "context"
    "strings"
)

// CLILLMClient wraps CLIExecutor for internal single-shot calls (e.g. future extraction).
// MaxTurns = 1 — not suitable for agentic loops.
type CLILLMClient struct {
    executor CLIExecutor
}

func NewCLILLMClient(exec CLIExecutor) *CLILLMClient {
    return &CLILLMClient{executor: exec}
}

// Call runs a single prompt turn and returns the full response text.
func (c *CLILLMClient) Call(ctx context.Context, systemPrompt, userPrompt, model string) (string, error) {
    if idx := strings.Index(model, ":"); idx >= 0 {
        model = model[idx+1:] // strip "anthropic:" prefix if present
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

### Acceptance Criteria

- [ ] `LLMConfig.Backend` field present with `toml:"backend"` tag
- [ ] `applyDefaults()` sets `Backend = "api"` when unset
- [ ] `validate()` skips `api_key_env` checks when `backend = "claude-cli"`; existing API validation unchanged
- [ ] `config.toml` and `deploy/config.docker.toml` have `backend = "api"`
- [ ] `internal/llm/claudecli/executor.go` compiles clean
- [ ] `internal/llm/claudecli/client.go` compiles clean
- [ ] `go build ./...` clean

---

## KLW-032 — Runtime wiring + context injection

**Type:** Feature  
**Effort:** 2 SP  
**Labels:** `phase/8`, `type/feature`, `size/S`, `component:runtime`  
**Dependencies:** KLW-031  
**Branch:** `feature/klw-031-claude-cli-backend`

### User Story

> As an operator with `backend = "claude-cli"` set, I send a message and receive a streaming response with my session history available to the LLM.

### Implementation Plan

#### 1. New file: `internal/runtime/context.go`

Builds the `--append-system-prompt` string from session history. Uses `session.SessionTurn` which has `UserMessage` and `AssistantResponse` string fields. History retrieved via `r.sessionManager.GetContextTurns(sess)`.

```go
package runtime

import (
    "fmt"
    "log/slog"
    "strings"

    "github.com/dydanz/akb48/internal/session"
)

const (
    maxHistoryTurns  = 20
    maxAssistantChars = 500
    maxAppendBytes   = 4 * 1024 // 4KB safety cap
)

// buildAppendContext formats recent session history for --append-system-prompt.
// Returns empty string when no history exists.
func buildAppendContext(turns []session.SessionTurn) string {
    if len(turns) == 0 {
        return ""
    }
    start := len(turns) - maxHistoryTurns
    if start < 0 {
        start = 0
    }

    var sb strings.Builder
    sb.WriteString("## Conversation history\n\n")
    for _, t := range turns[start:] {
        assistant := t.AssistantResponse
        if len(assistant) > maxAssistantChars {
            assistant = assistant[:maxAssistantChars] + "…"
        }
        fmt.Fprintf(&sb, "[user] %s\n[assistant] %s\n\n", t.UserMessage, assistant)
    }
    result := strings.TrimRight(sb.String(), "\n")

    if len(result) > maxAppendBytes {
        slog.Warn("buildAppendContext: history exceeds 4KB — truncating to 10 turns")
        return buildAppendContext(turns[len(turns)-10:])
    }
    return result
}
```

#### 2. `internal/runtime/runtime.go` — add CLI field and wire executor

Add `cliExec` field to `AKB48Runtime`:

```go
type AKB48Runtime struct {
    cfg            *config.Config
    llmCaller      llm.CallerInterface
    cliExec        claudecli.CLIExecutor  // nil when backend != "claude-cli" or binary absent
    registry       *tools.Registry
    sessionManager *session.SessionManager
    assembler      ContextAssembler
    coldOpener     *session.ColdOpener
    bridge         *brain.GBrainBridge
    hooks          []session.HookFunc
}
```

In `New()`, after the brain wiring block, add:

```go
// Wire CLI executor if backend = "claude-cli" (non-fatal if binary absent).
// Auth via CLAUDE_CODE_OAUTH_TOKEN in env — no API key injection.
var cliExec claudecli.CLIExecutor
if cfg.LLM.Backend == "claude-cli" {
    var cliErr error
    cliExec, cliErr = claudecli.New()
    if cliErr != nil {
        slog.Warn("CLIExecutor unavailable — cli-mode messages will return errors", "error", cliErr)
    }
}
```

Assign in the struct literal:
```go
rt := &AKB48Runtime{
    ...
    cliExec: cliExec,
}
```

#### 3. `internal/runtime/runtime.go` — branch `HandleMessage` on backend

Replace the current `r.llmCaller.Call(...)` block in `HandleMessage` with a backend switch:

```go
func (r *AKB48Runtime) HandleMessage(ctx context.Context, msg types.Message, tokens chan<- string) error {
    start := time.Now()

    sess, err := r.sessionManager.ResolveOrCreate(ctx, msg.SessionID)
    if err != nil {
        return fmt.Errorf("resolve session: %w", err)
    }

    var turnID = uuid.NewString()
    var finalText string
    var tokenUsage types.TokenUsage

    switch r.cfg.LLM.Backend {
    case "claude-cli":
        finalText, err = r.handleCLI(ctx, sess, msg.Text, tokens, turnID)
    default:
        finalText, tokenUsage, err = r.handleAPI(ctx, sess, msg.Text, tokens, turnID)
    }
    if err != nil {
        return err
    }

    turn := session.SessionTurn{
        TurnID:            turnID,
        Timestamp:         time.Now().UTC(),
        UserMessage:       msg.Text,
        AssistantResponse: finalText,
        TokenUsage:        tokenUsage,
        LatencyMs:         time.Since(start).Milliseconds(),
    }
    sess.AddTurn(turn)
    session.RunHooks(r.hooks, sess, turn)
    return nil
}
```

Extract current logic into `handleAPI`:

```go
func (r *AKB48Runtime) handleAPI(ctx context.Context, sess *session.Session, text string, tokens chan<- string, turnID string) (string, types.TokenUsage, error) {
    coldContext := ""
    if r.coldOpener != nil {
        coldContext = r.coldOpener.BuildColdContext(ctx, sess, text)
    }
    systemPrompt, messages, err := r.assembler.Build(sess, r.sessionManager, text, coldContext)
    if err != nil {
        return "", types.TokenUsage{}, fmt.Errorf("build context: %w", err)
    }
    messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(text)))
    result, err := r.llmCaller.Call(ctx, llm.CallParams{
        System:   systemPrompt,
        Messages: messages,
        Tokens:   tokens,
        TurnID:   turnID,
    })
    if err != nil {
        return "", types.TokenUsage{}, fmt.Errorf("llm call: %w", err)
    }
    return result.Text, result.TokenUsage, nil
}
```

New `handleCLI`:

```go
func (r *AKB48Runtime) handleCLI(ctx context.Context, sess *session.Session, text string, tokens chan<- string, turnID string) (string, error) {
    if r.cliExec == nil {
        return "", fmt.Errorf("cli backend unavailable (claude binary missing or not authenticated)")
    }

    // Get system prompt from assembler; discard messages[] — history goes via --append-system-prompt.
    systemPrompt, _, err := r.assembler.Build(sess, r.sessionManager, text, "")
    if err != nil {
        return "", fmt.Errorf("build system prompt: %w", err)
    }

    history := r.sessionManager.GetContextTurns(sess)
    appendCtx := buildAppendContext(history)

    ch, err := r.cliExec.Execute(ctx, claudecli.CLIRequest{
        Prompt:             text,
        SystemPrompt:       systemPrompt,
        AppendSystemPrompt: appendCtx,
        Model:              r.cfg.LLM.Model,
    })
    if err != nil {
        return "", fmt.Errorf("cli execute: %w", err)
    }

    var sb strings.Builder
    for evt := range ch {
        switch evt.Type {
        case "text":
            sb.WriteString(evt.Content)
            select {
            case tokens <- evt.Content:
            case <-ctx.Done():
                return sb.String(), ctx.Err()
            }
        case "error":
            return sb.String(), evt.Err
        }
    }
    return sb.String(), nil
}
```

**Note:** `handleAPI` and `handleCLI` are private methods. `SetLLMCaller` test helper unchanged.

### Acceptance Criteria

- [ ] `AKB48Runtime.cliExec` field present
- [ ] `New()` wires `CLIExecutor` non-fatally when `backend = "claude-cli"`
- [ ] `HandleMessage` switches on `r.cfg.LLM.Backend`; API path identical to current behaviour
- [ ] `handleCLI` returns user-facing error when `cliExec == nil` (binary absent)
- [ ] `buildAppendContext` formats last 20 turns; truncates assistant text at 500 chars; 4KB safety cap with 10-turn fallback
- [ ] `go build ./...` clean

---

## KLW-033 — Onboard command

**Type:** Feature  
**Effort:** 1 SP  
**Labels:** `phase/8`, `type/feature`, `size/XS`, `component:config`  
**Dependencies:** KLW-031  
**Branch:** `feature/klw-031-claude-cli-backend`

### User Story

> As a new operator with Claude Pro, I run `akb48 --onboard` and get step-by-step guidance to configure the CLI backend without needing a Console API key.

### Implementation Plan

#### 1. New file: `internal/onboard/onboard.go`

```go
package onboard

import (
    "encoding/json"
    "fmt"
    "os/exec"
)

// Run detects claude CLI auth state and prints config guidance.
func Run() {
    claudePath, err := exec.LookPath("claude")
    if err != nil {
        fmt.Println("claude binary not found in PATH.")
        fmt.Println("Install Claude Code CLI: https://claude.ai/download")
        fmt.Println("Then run: akb48 --onboard")
        return
    }
    fmt.Printf("Found claude at %s\n\n", claudePath)

    out, err := exec.Command(claudePath, "auth", "status", "--json").Output() //nolint:gosec
    if err != nil {
        fmt.Println("Not authenticated.")
        fmt.Println("Run: claude auth login")
        return
    }

    var status struct {
        LoggedIn bool   `json:"loggedIn"`
        Email    string `json:"email"`
    }
    _ = json.Unmarshal(out, &status)

    if !status.LoggedIn {
        fmt.Println("Not authenticated.")
        fmt.Println("Run: claude auth login")
        return
    }

    fmt.Printf("Authenticated as: %s\n\n", status.Email)
    fmt.Println("Add to config.toml under [llm]:")
    fmt.Println()
    fmt.Println(`  backend = "claude-cli"`)
    fmt.Println()
    fmt.Println("For a long-lived scripted token (recommended):")
    fmt.Println("  claude setup-token")
    fmt.Println("  export CLAUDE_CODE_OAUTH_TOKEN=<token printed above>")
    fmt.Println()
    fmt.Println("Then start normally: akb48")
}
```

#### 2. `cmd/akb48/main.go` — add `--onboard` flag

Add flag declaration after existing flags:
```go
onboard := flag.Bool("onboard", false, "detect CLI auth and print backend config guidance")
```

Add handling before `runtime.New(cfg)`:
```go
if *onboard {
    onboardpkg.Run()
    os.Exit(0)
}
```

Add import: `onboardpkg "github.com/dydanz/akb48/internal/onboard"`

**Note:** `--onboard` exits before config validation runs — intentional, since `ANTHROPIC_API_KEY` may not be set on a fresh machine. Config is loaded first (to get `--config` path support) but `validate()` is skipped by the early `os.Exit(0)`.

Wait — `config.Load()` calls `validate()` which currently fails if `ANTHROPIC_API_KEY` unset. With KLW-031's change, `validate()` only checks env var when `backend = "api"`. But on a fresh machine, `config.toml` may not exist yet. Two options:

**Chosen approach:** Run `--onboard` *before* `config.Load()`, so no config file is required:

```go
// In main(), before config.Load():
onboard := flag.Bool("onboard", false, "detect CLI auth and print backend config guidance")
configPath := flag.String("config", "config.toml", "path to config.toml")
validate := flag.Bool("validate", false, "validate config and exit")
flag.Parse()

if *onboard {
    onboardpkg.Run()
    os.Exit(0)
}

cfg, err := config.Load(*configPath)
// ... rest unchanged
```

### Acceptance Criteria

- [ ] `akb48 --onboard` exits 0 without requiring `config.toml` or any env vars
- [ ] Binary absent: prints install URL
- [ ] Binary present, not authenticated: prints `claude auth login` instruction
- [ ] Binary present, authenticated: prints `backend = "claude-cli"` snippet + `setup-token` guidance with operator's email
- [ ] `go build ./...` clean

---

## KLW-034 — Unit + integration tests

**Type:** Testing  
**Effort:** 2 SP  
**Labels:** `phase/8`, `type/test`, `size/S`, `component:runtime`  
**Dependencies:** KLW-031, KLW-032, KLW-033  
**Branch:** `feature/klw-031-claude-cli-backend`

### User Story

> As a developer, I want unit tests for `buildArgs`, `parseStreamLine`, and `buildAppendContext` so I can refactor with confidence.

### Implementation Plan

#### 1. New file: `internal/llm/claudecli/executor_test.go`

```go
package claudecli

import (
    "strings"
    "testing"
)

func TestBuildArgs_NoTools(t *testing.T) {
    args := buildArgs(CLIRequest{Prompt: "hello"})
    joined := strings.Join(args, " ")
    if !strings.Contains(joined, "--tools ") {
        t.Error("expected --tools flag when AllowedTools nil")
    }
    // Explicit empty value disables built-in tools
    for i, a := range args {
        if a == "--tools" && i+1 < len(args) && args[i+1] != "" {
            t.Errorf("expected empty --tools value, got %q", args[i+1])
        }
    }
}

func TestBuildArgs_WithTools(t *testing.T) {
    args := buildArgs(CLIRequest{Prompt: "hi", AllowedTools: []string{"Bash", "Read"}})
    joined := strings.Join(args, " ")
    if !strings.Contains(joined, "--tools Bash,Read") {
        t.Errorf("expected --tools Bash,Read in args, got: %s", joined)
    }
}

func TestBuildArgs_AppendSystemPrompt(t *testing.T) {
    args := buildArgs(CLIRequest{Prompt: "hi", AppendSystemPrompt: "## History\n\n[user] test"})
    joined := strings.Join(args, " ")
    if !strings.Contains(joined, "--append-system-prompt") {
        t.Error("expected --append-system-prompt flag")
    }
}

func TestBuildArgs_NoResume(t *testing.T) {
    // --resume must NEVER appear — it would replay previous tool calls
    cases := []CLIRequest{
        {Prompt: "a"},
        {Prompt: "b", AllowedTools: []string{"Bash"}},
        {Prompt: "c", AppendSystemPrompt: "history"},
        {Prompt: "d", MCPConfig: `{"servers":{}}`},
    }
    for _, req := range cases {
        for _, arg := range buildArgs(req) {
            if arg == "--resume" {
                t.Errorf("--resume found in args for request %+v", req)
            }
        }
    }
}

func TestBuildArgs_PromptAfterDoubleDash(t *testing.T) {
    args := buildArgs(CLIRequest{Prompt: "--weird-prompt"})
    // "--" must appear before the prompt so it isn't parsed as a flag
    foundSep := false
    for i, a := range args {
        if a == "--" {
            foundSep = true
            if i+1 >= len(args) || args[i+1] != "--weird-prompt" {
                t.Error("prompt not immediately after --")
            }
        }
    }
    if !foundSep {
        t.Error("-- separator not found in args")
    }
}

func TestParseStreamLine_TextDelta(t *testing.T) {
    var lastMsgID string
    var lastTextLen int

    // First event: "Hello"
    line1 := []byte(`{"type":"assistant","message":{"id":"msg_1","content":[{"type":"text","text":"Hello"}]}}`)
    evt, ok := parseStreamLine(line1, &lastMsgID, &lastTextLen)
    if !ok || evt.Type != "text" || evt.Content != "Hello" {
        t.Errorf("event 1: got %+v ok=%v", evt, ok)
    }

    // Second event: cumulative "Hello World"
    line2 := []byte(`{"type":"assistant","message":{"id":"msg_1","content":[{"type":"text","text":"Hello World"}]}}`)
    evt, ok = parseStreamLine(line2, &lastMsgID, &lastTextLen)
    if !ok || evt.Type != "text" || evt.Content != " World" {
        t.Errorf("event 2: expected delta \" World\", got %+v ok=%v", evt, ok)
    }
}

func TestParseStreamLine_Shrink(t *testing.T) {
    var lastMsgID string
    var lastTextLen int

    // Partial text block
    line1 := []byte(`{"type":"assistant","message":{"id":"msg_1","content":[{"type":"text","text":"Hello"}]}}`)
    parseStreamLine(line1, &lastMsgID, &lastTextLen)

    // Next event: text block replaced by tool_use — text shrinks to ""
    line2 := []byte(`{"type":"assistant","message":{"id":"msg_1","content":[{"type":"tool_use","text":""}]}}`)
    evt, ok := parseStreamLine(line2, &lastMsgID, &lastTextLen)
    // No text delta expected; lastTextLen reset to 0
    if ok && evt.Type == "text" && evt.Content != "" {
        t.Errorf("expected no text delta after shrink, got %q", evt.Content)
    }
    if lastTextLen != 0 {
        t.Errorf("expected lastTextLen=0 after shrink, got %d", lastTextLen)
    }
}

func TestParseStreamLine_NewMessageID(t *testing.T) {
    var lastMsgID string
    var lastTextLen int

    line1 := []byte(`{"type":"assistant","message":{"id":"msg_1","content":[{"type":"text","text":"Hello"}]}}`)
    parseStreamLine(line1, &lastMsgID, &lastTextLen)

    // New message ID — lastTextLen must reset
    line2 := []byte(`{"type":"assistant","message":{"id":"msg_2","content":[{"type":"text","text":"Hi"}]}}`)
    evt, ok := parseStreamLine(line2, &lastMsgID, &lastTextLen)
    if !ok || evt.Content != "Hi" {
        t.Errorf("expected full text on new message ID, got %+v ok=%v", evt, ok)
    }
}

func TestParseStreamLine_ResultSuccess(t *testing.T) {
    var lastMsgID string
    var lastTextLen int
    line := []byte(`{"type":"result","subtype":"success","cost_usd":0.0042}`)
    evt, ok := parseStreamLine(line, &lastMsgID, &lastTextLen)
    if !ok || evt.Type != "done" || evt.CostUSD != 0.0042 {
        t.Errorf("expected done event with cost, got %+v ok=%v", evt, ok)
    }
}

func TestParseStreamLine_ResultError(t *testing.T) {
    var lastMsgID string
    var lastTextLen int
    line := []byte(`{"type":"result","subtype":"error_during_execution"}`)
    evt, ok := parseStreamLine(line, &lastMsgID, &lastTextLen)
    if !ok || evt.Type != "error" || evt.Err == nil {
        t.Errorf("expected error event, got %+v ok=%v", evt, ok)
    }
}

func TestParseStreamLine_UnknownType(t *testing.T) {
    var lastMsgID string
    var lastTextLen int
    line := []byte(`{"type":"system","subtype":"init"}`)
    _, ok := parseStreamLine(line, &lastMsgID, &lastTextLen)
    if ok {
        t.Error("expected false for unknown type system:init")
    }
}

func TestParseStreamLine_MalformedJSON(t *testing.T) {
    var lastMsgID string
    var lastTextLen int
    _, ok := parseStreamLine([]byte(`not json`), &lastMsgID, &lastTextLen)
    if ok {
        t.Error("expected false for malformed JSON")
    }
}
```

#### 2. New file: `internal/runtime/context_test.go`

```go
package runtime

import (
    "strings"
    "testing"

    "github.com/dydanz/akb48/internal/session"
)

func TestBuildAppendContext_Empty(t *testing.T) {
    got := buildAppendContext(nil)
    if got != "" {
        t.Errorf("expected empty string for nil turns, got %q", got)
    }
    got = buildAppendContext([]session.SessionTurn{})
    if got != "" {
        t.Errorf("expected empty string for empty turns, got %q", got)
    }
}

func TestBuildAppendContext_Format(t *testing.T) {
    turns := []session.SessionTurn{
        {UserMessage: "hello", AssistantResponse: "world"},
        {UserMessage: "foo", AssistantResponse: "bar"},
    }
    got := buildAppendContext(turns)
    if !strings.Contains(got, "## Conversation history") {
        t.Error("expected header")
    }
    if !strings.Contains(got, "[user] hello") {
        t.Error("expected user turn")
    }
    if !strings.Contains(got, "[assistant] world") {
        t.Error("expected assistant turn")
    }
}

func TestBuildAppendContext_TruncatesAssistant(t *testing.T) {
    long := strings.Repeat("x", 600)
    turns := []session.SessionTurn{
        {UserMessage: "q", AssistantResponse: long},
    }
    got := buildAppendContext(turns)
    if strings.Contains(got, long) {
        t.Error("expected assistant response to be truncated at 500 chars")
    }
    if !strings.Contains(got, "…") {
        t.Error("expected ellipsis after truncated assistant text")
    }
}

func TestBuildAppendContext_LimitsTo20Turns(t *testing.T) {
    turns := make([]session.SessionTurn, 30)
    for i := range turns {
        turns[i] = session.SessionTurn{UserMessage: "q", AssistantResponse: "a"}
    }
    got := buildAppendContext(turns)
    // Count "[user]" occurrences
    count := strings.Count(got, "[user]")
    if count > 20 {
        t.Errorf("expected max 20 turns, got %d", count)
    }
}
```

#### 3. Run validation

```bash
go test ./internal/llm/claudecli/... -v
go test ./internal/runtime/... -v
go test ./internal/onboard/... -v   # if onboard tests added
go test ./...                        # full suite, no regressions
go build ./...
```

### Acceptance Criteria

- [ ] `TestBuildArgs_NoResume` passes
- [ ] `TestBuildArgs_NoTools` passes — `--tools ""` present
- [ ] `TestParseStreamLine_Shrink` passes
- [ ] `TestParseStreamLine_TextDelta` passes — delta correctly extracted
- [ ] `TestBuildAppendContext_TruncatesAssistant` passes
- [ ] `TestBuildAppendContext_LimitsTo20Turns` passes
- [ ] `go test ./...` green — no regressions in existing packages

---

## Manual Test Checklist

Run after all four tickets are merged:

```bash
# Test 1: API mode unchanged
export ANTHROPIC_API_KEY=sk-ant-...
./akb48 --validate
# Expected: config ok, no errors

# Test 2: CLI mode, binary absent (simulate by using wrong PATH)
PATH=/tmp ./akb48 --config config.toml
# Expected: WARN CLIExecutor unavailable; starts normally
# Send message → receives error "cli backend unavailable (claude binary missing...)"

# Test 3: --onboard, no config required
unset ANTHROPIC_API_KEY
./akb48 --onboard
# Expected: detects claude binary + auth, prints backend = "claude-cli" snippet, exits 0

# Test 4: --onboard, binary not in PATH
PATH=/tmp ./akb48 --onboard
# Expected: "claude binary not found", prints install URL, exits 0

# Test 5: CLI mode, authenticated
# In config.toml: backend = "claude-cli", comment out api_key_env check
./akb48
# Send "hello" via Discord/CLI
# Expected: streaming response, session persists to JSONL
```

---

## Risk Register

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| `assembler.Build()` second return (messages[]) discarded in CLI path — cold opener context lost | Low | Cold opener sets `coldContext` which flows into `systemPrompt` via assembler — not discarded. Only `messages[]` is discarded; history is re-injected via `--append-system-prompt` |
| `--append-system-prompt` arg exceeds OS arg limit (ARG_MAX ~2MB) | Very low | 4KB cap with 10-turn fallback; well within limits |
| `claude setup-token` 1-year token invalidated by password change | Low | Out of scope; operator re-runs `--onboard` + `setup-token` |
| Delta extraction breaks on future claude CLI output format change | Low | `parseStreamLine` silently skips unknown types; worst case is missing deltas, not a crash |
| `handleAPI` refactor introduces regression | Low | Behaviour is identical — only moved into helper; existing tests cover it |

---

## File Change Summary

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `Backend` field; update `applyDefaults` and `validate` |
| `config.toml` | Add `backend = "api"` |
| `deploy/config.docker.toml` | Add `backend = "api"` |
| `internal/llm/claudecli/executor.go` | New — types, executor, buildArgs, parseStreamLine |
| `internal/llm/claudecli/client.go` | New — CLILLMClient |
| `internal/runtime/context.go` | New — buildAppendContext helper |
| `internal/runtime/runtime.go` | Add cliExec field; wire in New(); branch HandleMessage; extract handleAPI/handleCLI |
| `internal/onboard/onboard.go` | New — Run() |
| `cmd/akb48/main.go` | Add --onboard flag; call onboard.Run() before config.Load() |
| `internal/llm/claudecli/executor_test.go` | New — buildArgs + parseStreamLine tests |
| `internal/runtime/context_test.go` | New — buildAppendContext tests |
