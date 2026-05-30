# AKB48 Research 06 — Claude CLI as Backend Provider

**Date:** 2026-05-24  
**Topic:** Using Claude Code CLI credentials/subprocess as backend — no separate API key required  
**Inspiration:** OpenClaw `onboard` command pattern

---

## Motivation

AKB48 currently requires `ANTHROPIC_API_KEY` (Console pay-per-use key) to call the Anthropic API via `anthropic-sdk-go`. Many operators already have an active **Claude Pro/Max subscription** with Claude Code CLI installed and authenticated. Research question: can AKB48 use that existing CLI auth to power itself, eliminating the need for a separate Console key?

---

## 1. Claude CLI Credential Storage

### macOS (Keychain)

Stored in system Keychain under service `"Claude Code-credentials"`, account `$USER`. Value is hex-encoded JSON:

```json
{
  "claudeAiOauth": {
    "accessToken":   "sk-ant-oat01-...",
    "refreshToken":  "sk-ant-ort01-...",
    "expiresAt":     1234567890000,
    "scopes": [
      "user:inference",
      "user:profile",
      "user:sessions:claude_code",
      "user:mcp_servers"
    ],
    "subscriptionType": null,
    "rateLimitTier":    null
  }
}
```

Read via:
```bash
security find-generic-password -a "$USER" -s "Claude Code-credentials" -w \
  | xxd -r -p | jq .
```

`~/.claude.json` holds account metadata (UUIDs, email) but **not** the token.

### Linux / Windows

Stored in `~/.claude/.credentials.json` (mode `0600`). Location moves to `$CLAUDE_CODE_DIR/.credentials.json` if that env var is set.

### `claude auth login` flow

PKCE OAuth: browser opens `https://claude.ai/oauth/authorize` → user approves → token exchanged via `POST https://platform.claude.com/v1/oauth/token` → stored in Keychain or `.credentials.json`.

### `claude setup-token`

Generates a long-lived (1-year) `sk-ant-oat01-...` token, prints to stdout only — **never stored to disk**. Operator must manually export as `CLAUDE_CODE_OAUTH_TOKEN`. This is the official path for scripted usage.

---

## 2. Claude CLI Subprocess Interface

The CLI has a full machine-readable mode for programmatic use. Canonical invocation:

```bash
claude \
  --print \
  --input-format  stream-json \
  --output-format stream-json \
  --model         claude-sonnet-4-6 \
  --permission-mode bypassPermissions
```

### Output formats

| Flag | Description |
|------|-------------|
| `--output-format text` | Human-readable (default) |
| `--output-format json` | Single JSON object on completion: `result`, `total_cost_usd`, `is_error`, `session_id`, `duration_ms`, `num_turns` |
| `--output-format stream-json` | NDJSON stream — one event per line, real-time |

### Multi-turn via stream-json

Write follow-up prompts to `stdin` as NDJSON. `ResultMessage.SessionID` can be stored and passed back via `--resume` to continue across process restarts.

### Other useful flags

- `--bare` — skips CLAUDE.md, hooks, MCP, skills auto-discovery. Fastest for scripted calls. Note: `--bare` ignores `CLAUDE_CODE_OAUTH_TOKEN`; needs `ANTHROPIC_API_KEY` or `apiKeyHelper`.
- `claude auth status` — exit 0 = authenticated; JSON output for scripted pre-flight check.
- `claude agents --json` — live background sessions as JSON array.

**No local HTTP server or socket.** Only interface is stdin/stdout subprocess.

---

## 3. OpenClaw — Technical Implementation

OpenClaw (Node.js/TypeScript, not Go) is the closest reference implementation.

### `openclaw onboard` flow

1. Detects `claude` binary and auth state
2. Installs gateway daemon (launchd/systemd user service)
3. Writes `~/.openclaw/openclaw.json` with model routing (`claude-cli/<model-id>`)
4. Sets up workspace at `~/.openclaw/workspace` with identity files (AGENTS.md, SOUL.md, TOOLS.md)

### CLI backend implementation

- Model prefix `claude-cli/` → subprocess backend; `anthropic/` → direct HTTP API
- Spawns `claude -p --output-format stream-json --verbose ...` as persistent subprocess per session
- Keeps process alive across turns by writing NDJSON messages to stdin
- Session persistence via stored `session_id`; invalidated on credential rotation
- Token refresh: reads `claudeAiOauth.accessToken` from `~/.claude/.credentials.json`, caches in `~/.openclaw/agents/main/agent/auth-profiles.json` (30s TTL), refreshes via `POST https://claude.ai/v1/oauth/token`

**Historical note:** OpenClaw originally extracted OAuth tokens and called the Anthropic API directly. **This was broken by Anthropic's February 20, 2026 policy change** and OpenClaw migrated to the subprocess model.

---

## 4. Claude Code SDK / Programmatic API

Anthropic's official **Claude Agent SDK** (released June 2025) covers TypeScript and Python only.

### Unofficial Go SDKs

All use the same subprocess pattern — spawn `claude` CLI and speak stream-json:

| Package | Notes |
|---------|-------|
| `github.com/partio-io/claude-agent-sdk-go` | Most complete; zero external deps; Go 1.26+ |
| `github.com/bpowers/go-claudecode` | Transport-layer wrapper; bidirectional JSON-RPC 2.0 over stdin/stdout; sandbox integration |
| `github.com/schlunsen/claude-agent-sdk-go` | Community port |
| `github.com/dotcommander/agent-sdk-go` | Another variant |

None handle credentials directly — they assume CLI is already authenticated.

---

## 5. Credential Reuse in Go — What Works vs What Is Blocked

### Credential priority in Claude Code CLI (descending)

1. `CLAUDE_CODE_USE_BEDROCK` / `CLAUDE_CODE_USE_VERTEX` / `CLAUDE_CODE_USE_FOUNDRY`
2. `ANTHROPIC_AUTH_TOKEN` (Bearer header to proxy/gateway)
3. `ANTHROPIC_API_KEY` (X-Api-Key header to Anthropic API)
4. `apiKeyHelper` script output
5. `CLAUDE_CODE_OAUTH_TOKEN` (long-lived token from `claude setup-token`)
6. Interactive OAuth from `/login`

### Direct OAuth token extraction — DEAD END (as of Feb 2026)

Read `~/.claude/.credentials.json`, extract `sk-ant-oat01-...`, send as `Authorization: Bearer` to Anthropic API. **Returns:** `"OAuth authentication is currently not supported."` Sending via `x-api-key` returns `"invalid x-api-key"`.

**Anthropic blocked this on February 20, 2026** (anthropics/claude-code issue #28091). Consumer OAuth tokens no longer work for direct API calls from third parties.

### Legitimate path

Use `CLAUDE_CODE_OAUTH_TOKEN` (from `claude setup-token`) but **only** as an env var fed into the CLI subprocess — not extracted for use with `anthropic-sdk-go` directly.

---

## 6. Claude Code CLI as MCP Server

`steipete/claude-code-mcp` wraps Claude Code CLI as an MCP server:
- Exposes single tool: `claude_code` with args `prompt`, `workFolder`, `sessionId`, `permissionMode`
- Spawns `claude` subprocess under the hood; streams response back via MCP JSON-RPC

**AKB48 integration:** Register `claude-code-mcp` as an MCP subprocess alongside GBrain. Register `claude_code` tool with `claudecli_` prefix in ToolRegistry. AKB48's LLM can delegate agentic sub-tasks (code edits, bash, refactoring) to Claude Code without AKB48 managing credentials.

---

## 7. Risks and Constraints

| Risk | Detail |
|------|--------|
| **ToS (critical)** | Direct OAuth token extraction violates Consumer Terms as of Feb 20, 2026. Subprocess only. |
| **Rate limits** | Pro/Max subscription: interactive + agentic share a monthly pool. As of June 2026, `claude -p` Agent SDK usage draws from a separate monthly Agent SDK credit pool. |
| **Credential rotation** | CLI handles OAuth refresh transparently. `setup-token` tokens are 1-year; invalidated by password changes. |
| **Binary dependency** | All subprocess paths require `claude` in PATH. Large Node.js bundle (~hundreds of MB). Version drift: use `VersionError` sentinel. |
| **Session invalidation** | Resuming via `--resume` + stored `session_id` fails after credential rotation. |
| **No `--bare` + OAuth token** | `--bare` mode ignores `CLAUDE_CODE_OAUTH_TOKEN`; needs `ANTHROPIC_API_KEY` or `apiKeyHelper`. |

---

## 8. Implementation Paths for AKB48 — Ranked

### Path A — Subprocess backend (recommended)

Replace `anthropic-sdk-go` direct calls with a subprocess wrapper. AKB48 spawns `claude` per message via stream-json protocol. **This is proven working** — see Section 10 for the exact implementation extracted from Electrum Agent Gateway.

**Credential priority for AKB48 subprocess mode:**
- `CLAUDE_CODE_OAUTH_TOKEN` env var (from `claude setup-token`) → subscription billing
- Else: CLI uses its stored Keychain/`.credentials.json` OAuth transparently
- Fallback: `ANTHROPIC_API_KEY` env var if set → passed explicitly to subprocess env

**Config addition:**

```toml
[llm]
backend = "api"           # "api" (default) | "claude-cli"
model   = "claude-sonnet-4-6"
api_key_env = "ANTHROPIC_API_KEY"   # also used as subprocess env injection when set
```

**Pros:** No credential extraction. ToS-safe. Works with subscription and API key. Full agentic loop (tools, MCP) runs inside subprocess.  
**Cons:** Binary dependency (`claude` in PATH). No persistent subprocess across turns — each turn spawns fresh process (cold start ~200ms). No access to intermediate tool calls from gateway side.

---

### Path B — MCP wrapper (parallel capability, not replacement)

Run `steipete/claude-code-mcp` as MCP subprocess alongside GBrain. Register `claude_code` tool in ToolRegistry. AKB48's main LLM (still direct API) delegates agentic sub-tasks to Claude Code.

**Pros:** Architecturally clean; AKB48 stays as orchestrator; no credential change needed.  
**Cons:** Not a full backend replacement; still needs `ANTHROPIC_API_KEY` for AKB48's own calls.

---

### Path C — Direct OAuth extraction (DEAD END)

Read `~/.claude/.credentials.json`, extract `sk-ant-oat01-...`, pass to `anthropic-sdk-go`.  
**Blocked as of February 20, 2026. Do not implement.**

---

## 9. Recommended AKB48 Implementation

**Phase A (onboard detection — minimal change):**
- Add `akb48 --onboard` flag
- Run `claude auth status` subprocess
- If logged in + no `ANTHROPIC_API_KEY` set: set `llm.backend = "claude-cli"` in config, print instructions to run `claude setup-token` for long-lived token
- If not: guide through `ANTHROPIC_API_KEY` setup (current path)

**Phase B (subprocess LLM caller):**
- New `internal/llm/claude_cli.go` — port the Electrum `executor.go` design (Section 10) to AKB48
- Interface: `CLIExecutor` port with `Execute(ctx, CLIRequest) (<-chan CLIEvent, error)`
- Non-fatal wiring: `New()` returns `(nil, error)` if binary absent — api-mode continues
- Config switch: `backend = "api"` (existing `anthropic-sdk-go`) | `backend = "claude-cli"` (subprocess)
- `runtime.go` picks caller at startup based on config
- Session context (history) injected via `--append-system-prompt` — do NOT use `--resume`

**Phase C (optional MCP delegation):**
- Register `claude-code-mcp` as second MCP server in GBrain config
- Expose `claudecli_execute` tool for agentic sub-tasks

---

## 10. Proven Implementation — Electrum Agent Gateway

**Source:** `electrum-agent-gateway/internal/nalar/adapter/outbound/claudecli/`  
**Status:** Production-deployed, fully tested.

This is the reference implementation. Port this directly to AKB48 rather than writing from scratch.

---

### 10.1 Port Interfaces

Two clean interfaces. Define these in AKB48's equivalent of `internal/types/` or `internal/llm/`:

```go
// CLIRequest is the input for a claude CLI agent turn.
// The gateway owns conversation context: history and memory are pre-formatted
// and injected via AppendSystemPrompt. No --resume; each invocation is stateless.
type CLIRequest struct {
    Prompt             string
    SystemPrompt       string   // full agent system prompt (sent every turn)
    AppendSystemPrompt string   // dynamic context per turn: memory facts + conversation history
    Model              string   // bare model name, e.g. "claude-sonnet-4-6" (no provider prefix)
    MaxTurns           int      // 0 = claude default
    MaxBudgetUSD       float64  // 0 = no limit
    AllowedTools       []string // claude built-in tools: ["Bash","Read"]; nil = no built-in tools
    MCPConfig          string   // JSON string for --mcp-config; empty = no MCP
}

// CLIEvent is one streamed output from the claude subprocess.
type CLIEvent struct {
    Type    string  // "text" | "done" | "error"
    Content string  // set for "text"
    CostUSD float64 // set for "done"
    Err     error   // set for "error"
}

// CLIExecutor spawns a claude subprocess and streams back CLIEvents.
// New returns nil (not an error) when the binary is absent — api-mode agents still work.
type CLIExecutor interface {
    Execute(ctx context.Context, req CLIRequest) (<-chan CLIEvent, error)
}
```

---

### 10.2 Executor Implementation

```go
// internal/llm/claude_cli.go
package claudecli

type executor struct {
    binPath string
    apiKey  string // ANTHROPIC_API_KEY value; injected into subprocess env if set
    log     zerolog.Logger
}

// New returns a CLIExecutor. Returns error if claude not in PATH.
// Callers should treat this as non-fatal: api-mode still works without it.
func New(apiKey string, log zerolog.Logger) (CLIExecutor, error) {
    path, err := exec.LookPath("claude")
    if err != nil {
        return nil, fmt.Errorf("claude binary not found in PATH: %w", err)
    }
    return &executor{binPath: path, apiKey: apiKey, log: log}, nil
}

func (e *executor) Execute(ctx context.Context, req CLIRequest) (<-chan CLIEvent, error) {
    args := buildArgs(req)

    cmd := exec.CommandContext(ctx, e.binPath, args...) // #nosec G204 — binPath is operator-configured
    cmd.Env = os.Environ()
    if e.apiKey != "" {
        cmd.Env = append(cmd.Env, "ANTHROPIC_API_KEY="+e.apiKey)
    }
    cmd.Dir = os.TempDir() // CRITICAL: avoids picking up local CLAUDE.md / settings

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
        scanner.Buffer(make([]byte, 1<<20), 1<<20) // 1MB buffer for large responses
        for scanner.Scan() {
            evt, ok := parseStreamLine(scanner.Bytes(), &lastMsgID, &lastTextLen, e.log)
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

---

### 10.3 Args Builder

```go
func buildArgs(req CLIRequest) []string {
    args := []string{
        "--print",
        "--verbose",
        "--output-format", "stream-json",
        "--include-partial-messages", // enables incremental text events
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
    if req.MaxBudgetUSD > 0 {
        args = append(args, "--max-budget-usd", fmt.Sprintf("%.4f", req.MaxBudgetUSD))
    }
    if len(req.AllowedTools) > 0 {
        args = append(args, "--tools", strings.Join(req.AllowedTools, ","))
    } else {
        args = append(args, "--tools", "") // explicit empty = no built-in tools
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
```

**NEVER add `--resume` to args.** The gateway owns session state via `--append-system-prompt`. `--resume` would re-run previous tool calls and corrupt state.

---

### 10.4 Stream Line Parser — Delta Extraction

This is the most non-obvious part. With `--include-partial-messages`, each `assistant` event contains the **full cumulative text** so far, not a delta. The parser tracks state across calls to extract incremental tokens:

```go
// cliMessage mirrors the relevant fields from claude's stream-json assistant message.
type cliMessage struct {
    ID      string `json:"id"`
    Content []struct {
        Type string `json:"type"`
        Text string `json:"text"`
    } `json:"content"`
}

func parseStreamLine(line []byte, lastMsgID *string, lastTextLen *int, log zerolog.Logger) (CLIEvent, bool) {
    var raw struct {
        Type    string      `json:"type"`
        Subtype string      `json:"subtype"`
        Message *cliMessage `json:"message"`
        CostUSD float64     `json:"cost_usd"`
    }
    if err := json.Unmarshal(line, &raw); err != nil {
        log.Debug().Str("line", string(line)).Msg("cli: unparseable stream line")
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
        // Guard: text shrinks when a tool_use block replaces a partial text block.
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

    // system:init, tool_use, and all other types silently ignored.
    return CLIEvent{}, false
}
```

**Why `lastTextLen` shrinks:** When a partial `assistant` event has `[text_block, partial_tool_use]`, then the next event may have only `[tool_use]` (no text block). The text disappears from `content`. Guard with `if *lastTextLen > len(fullText) { *lastTextLen = 0 }`.

---

### 10.5 CLILLMClient — Adapter for Internal Use Cases

When no direct API key is configured but the CLI is available, wrap `CLIExecutor` as a `LLMClient` for internal single-shot calls (e.g. fact extraction, triple indexing):

```go
// CLILLMClient wraps CLIExecutor to implement LLMClient.
// Used for internal model calls (e.g. extraction) when no direct API key is configured.
type CLILLMClient struct {
    executor CLIExecutor
}

func NewCLILLMClient(executor CLIExecutor) LLMClient {
    return &CLILLMClient{executor: executor}
}

func (c *CLILLMClient) Stream(ctx context.Context, req StreamRequest) (<-chan StreamEvent, error) {
    // Extract last user message as prompt
    var prompt string
    for i := len(req.Messages) - 1; i >= 0; i-- {
        if req.Messages[i].Role == "user" {
            prompt = req.Messages[i].Content
            break
        }
    }
    // Strip provider prefix: "anthropic:claude-haiku-4-5-20251001" → "claude-haiku-4-5-20251001"
    model := req.Model
    if idx := strings.Index(model, ":"); idx >= 0 {
        model = model[idx+1:]
    }

    cliCh, err := c.executor.Execute(ctx, CLIRequest{
        Prompt:       prompt,
        SystemPrompt: req.System,
        Model:        model,
        MaxTurns:     1, // single-shot for internal calls
    })
    if err != nil {
        return nil, err
    }

    out := make(chan StreamEvent, 32)
    go func() {
        defer close(out)
        for evt := range cliCh {
            switch evt.Type {
            case "text":
                out <- StreamEvent{Type: "text", Content: evt.Content}
            case "done":
                out <- StreamEvent{Type: "done"}
            case "error":
                out <- StreamEvent{Type: "error", Err: evt.Err}
            }
        }
    }()
    return out, nil
}
```

---

### 10.6 Non-Fatal Wiring Pattern

In the DI/startup wiring, `CLIExecutor` is optional. Api-mode continues working when `claude` is absent:

```go
// Wiring (fx/modules.go pattern — adapt to AKB48's runtime.go)
exec, err := claudecli.New(cfg.APIKeys.Anthropic, log)
if err != nil {
    log.Warn().Err(err).Msg("CLIExecutor unavailable: cli-mode agents will return errors")
    // exec = nil — passed as nil to chatUseCase; chatCLI returns error if called
}
```

And in the caller (AKB48's `runtime.go` equivalent):

```go
if u.cli == nil {
    // return error to user — don't panic
    return fmt.Errorf("cli backend not configured (claude binary missing?)")
}
```

---

### 10.7 Context Injection Pattern

**The gateway — not the subprocess — owns conversation history and memory.** Injected via `--append-system-prompt` every turn:

```go
var appendParts []string

// 1. Memory facts from vector search
if len(facts) > 0 {
    appendParts = append(appendParts, formatMemoryBlock(facts))
}

// 2. KG triples relevant to the query
if kgBlock := u.kgAutoSearch(ctx, agent.Slug, req.Message); kgBlock != "" {
    appendParts = append(appendParts, kgBlock)
}

// 3. Conversation history (last 20 turns; assistant truncated to 500 chars)
if historyBlock := formatHistoryBlock(conv.Messages); historyBlock != "" {
    appendParts = append(appendParts, "## Conversation history\n"+historyBlock)
}

cliReq := CLIRequest{
    Prompt:             req.Message,
    SystemPrompt:       agentSystemPrompt,
    AppendSystemPrompt: strings.Join(appendParts, "\n\n"),
    Model:              model, // bare name, no "anthropic:" prefix
    MaxTurns:           maxTurns,
    AllowedTools:       builtinTools,  // only tools without ":" — MCP tools go via MCPConfig
    MCPConfig:          agent.MCPConfigJSON,
}
```

**Tool separation rule:**
- Built-in tools (no colon): `"Bash"`, `"Read"` → `--tools Bash,Read`
- MCP tools (`"server:tool"`): stripped, server URL passed via `--mcp-config` JSON

---

### 10.8 Defensive Flush

If the subprocess closes stdout without emitting a `result` event (crash, OOM, etc.), don't leave the caller hanging:

```go
if !doneEmitted && fullText.Len() > 0 {
    log.Warn().Msg("chat(cli): defensive flush — subprocess closed without done/error")
    // persist accumulated text, emit done
    out <- ChatEvent{Type: ChatEventDone}
}
```

---

### 10.9 Key Design Decisions (Non-Obvious)

| Decision | Rationale |
|----------|-----------|
| `cmd.Dir = os.TempDir()` | Prevents subprocess from reading local `CLAUDE.md`, hooks, skill files in the project directory — subprocess must be stateless |
| `scanner.Buffer(make([]byte, 1<<20), 1<<20)` | Default bufio scanner buffer is 64KB; long LLM responses overflow it silently. 1MB covers all normal responses |
| No `--resume` flag | Gateway owns session state. Using `--resume` would re-execute previous tool calls, corrupting turn state |
| `--include-partial-messages` not `stdin` streaming | Single-shot per turn: spawn → stream stdout → close. Not a long-lived stdin/stdout process. Simpler; no reconnect complexity |
| Delta extraction with `lastMsgID`/`lastTextLen` | `--include-partial-messages` sends cumulative text. Delta = `fullText[lastTextLen:]`. Reset on new message ID |
| `--tools ""` when no tools | Without an explicit empty `--tools`, Claude may default to its built-in tools. Pass empty string to disable |
| API key in subprocess env (not `CLAUDE_CODE_OAUTH_TOKEN`) | If `ANTHROPIC_API_KEY` is set, inject it explicitly. CLI auth (OAuth) is the fallback when the key is absent |

---

## Sources

- [Claude Code CLI Reference](https://docs.anthropic.com/en/docs/claude-code/cli-reference)
- [Claude Code Authentication Docs](https://docs.anthropic.com/en/docs/claude-code/authentication)
- [OAuth token deprecation — anthropics/claude-code #28091](https://github.com/anthropics/claude-code/issues/28091)
- [OpenClaw CLI backends](https://docs.openclaw.ai/gateway/cli-backends)
- [Switching OpenClaw to CLI backends (brtkwr.com)](https://brtkwr.com/posts/2026-04-09-switching-openclaw-to-cli-backends-for-claude-and-codex/)
- [opencode-claude-auth plugin](https://github.com/griffinmartin/opencode-claude-auth)
- [claude-agent-sdk-go (partio-io)](https://pkg.go.dev/github.com/partio-io/claude-agent-sdk-go)
- [go-claudecode (bpowers)](https://pkg.go.dev/github.com/bpowers/go-claudecode)
- [claude-code-mcp (steipete)](https://github.com/steipete/claude-code-mcp)
- [OAuth Token vs API Key in 2026 (Medium)](https://lalatenduswain.medium.com/claude-code-on-claude-max-plan-understanding-oauth-token-vs-api-key-authentication-in-2026-96a6213d2cde)
