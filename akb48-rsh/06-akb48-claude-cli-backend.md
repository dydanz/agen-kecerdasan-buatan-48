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

Replace `anthropic-sdk-go` direct calls with a subprocess wrapper. AKB48 spawns `claude` per session via stream-json protocol.

**Go implementation skeleton:**

```go
// internal/llm/claude_cli.go
type CLICaller struct {
    cmd    *exec.Cmd
    stdin  io.WriteCloser
    stdout *bufio.Scanner
    mu     sync.Mutex
}

func NewCLICaller(ctx context.Context, model, oauthToken string) (*CLICaller, error) {
    cmd := exec.CommandContext(ctx, "claude",
        "--print",
        "--input-format",  "stream-json",
        "--output-format", "stream-json",
        "--model",         model,
        "--permission-mode", "bypassPermissions",
        "--no-session-persistence", // AKB48 manages sessions via JSONL
    )
    // Pass CLAUDE_CODE_OAUTH_TOKEN if set; otherwise let CLI use its own env
    if oauthToken != "" {
        cmd.Env = append(os.Environ(), "CLAUDE_CODE_OAUTH_TOKEN="+oauthToken)
    } else {
        cmd.Env = os.Environ()
    }
    // wire stdin/stdout ...
    return &CLICaller{cmd: cmd, ...}, nil
}
```

**Onboard command** (`akb48 --onboard`):
1. Run `claude auth status` → verify logged in
2. If yes: set `llm.backend = "claude-cli"` in config; no API key required
3. If no: prompt to run `claude auth login` or provide `ANTHROPIC_API_KEY`

**Credential priority for AKB48 subprocess mode:**
- `CLAUDE_CODE_OAUTH_TOKEN` env var (from `claude setup-token`) → subscription billing
- Else: CLI uses its stored Keychain/`.credentials.json` OAuth transparently
- Fallback: `ANTHROPIC_API_KEY` env var if set (Console pay-per-use)

**Config addition:**

```toml
[llm]
backend = "api"           # "api" (default) | "claude-cli"
model   = "claude-sonnet-4-6"
api_key_env = "ANTHROPIC_API_KEY"   # unused if backend = "claude-cli"
oauth_token_env = "CLAUDE_CODE_OAUTH_TOKEN"  # optional; CLI uses its own creds if unset
```

**Pros:** No credential extraction. ToS-safe. Works with subscription and API key. Streaming via NDJSON.  
**Cons:** Binary dependency. Subprocess startup latency per session. No `anthropic-sdk-go` streaming features.

---

### Path B — MCP wrapper (parallel capability, not replacement)

Run `steipete/claude-code-mcp` as MCP subprocess alongside GBrain. Register `claude_code` tool in ToolRegistry. AKB48's main LLM (still direct API) delegates agentic file-editing tasks to Claude Code.

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
- If logged in + no `ANTHROPIC_API_KEY` set: write `llm.backend = "claude-cli"` to config, print instructions to run `claude setup-token` for long-lived token
- If not: guide through `ANTHROPIC_API_KEY` setup (current path)

**Phase B (subprocess LLM caller):**
- New `internal/llm/claude_cli.go` implementing `LLMCaller` interface via stream-json subprocess
- Config switch: `backend = "api"` (existing) | `backend = "claude-cli"` (new)
- `runtime.go` picks caller at startup based on config
- Session ID managed by AKB48 JSONL layer; pass `--no-session-persistence` to subprocess

**Phase C (optional MCP delegation):**
- Register `claude-code-mcp` as second MCP server in GBrain config
- Expose `claudecli_execute` tool for agentic sub-tasks

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
