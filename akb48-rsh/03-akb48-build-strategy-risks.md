# AKB48 — Document 3: Build Strategy, Implementation Plan & Risks

## For: Solo CTO/CEO building a personal AI agent system
## Architecture: GBrain-style (thin claw + knowledge graph + skill files)
## Language: Go 1.25
## Date: April 2026 (revised May 2026)

---

## 1. Build Strategy: Three Phases

### Phase 1: Working Prototype (Weeks 1-4)

**Goal:** Chat → agent → brain pipeline that executes one workflow end-to-end.

**Stack:**

| Component | Choice | Notes |
|-----------|--------|-------|
| Runtime | Go 1.25, single binary | `go build -o akb48 ./cmd/akb48/` |
| Telegram adapter | `github.com/go-telegram-bot-api/telegram-bot-api/v5` | Long-polling, ~180 lines |
| CLI adapter | `bufio.Scanner` + `os.Stdin/Stdout` | ~50 lines, dev/testing |
| LLM SDK | `github.com/anthropics/anthropic-sdk-go` | Streaming support |
| Config | `github.com/BurntSushi/toml` | Typed, human-readable |
| Idempotency | `github.com/google/uuid` | UUID v4 per tool call |
| Concurrency | `golang.org/x/sync/errgroup` | Post-turn hooks |
| Brain | GBrain `gbrain serve` via MCP stdio | Connected at startup |
| Session | JSONL files under `~/.akb48/sessions/` | Append-only |
| Skills | Markdown files (`skills/*/SKILL.md`) | Hot-reloaded |
| Logging | `log/slog` (JSONHandler) | Structured, auditable |

**Week-by-week plan:**

**Week 1** — Compile a Go binary that connects to the Anthropic API and returns a response. No skills, no brain, no Telegram. Just a working `./akb48` CLI loop. Validates: SDK integration, streaming, config loading, `go vet` passes.

```
Milestone: `./akb48` reads stdin, calls Claude Sonnet 4.6, streams response tokens to stdout.
Exit criteria: `go build ./...` clean, `go vet ./...` clean, streaming works.
```

**Week 2** — Connect GBrain via MCP. Spawn `gbrain serve` as a child process, discover tools via `tools/list`, register them in `ToolRegistry` with `gbrain_` prefix. Now the agent can `gbrain_search` and `gbrain_put` from the CLI loop.

```
Milestone: Ask the agent something → it searches GBrain → returns a result.
Exit criteria: Tool call logged to tool-calls.jsonl. Brain page created.
```

**Week 3** — Add the skill resolver. Create 3-5 skill files, wire `ContextAssembler` to load identity files and inject the resolved skill. Enable Telegram adapter with allowlist.

```
Skills to create:
  skills/research/SKILL.md     — web research with brain storage
  skills/note-capture/SKILL.md — capture facts/decisions to brain
  skills/daily-briefing/SKILL.md — morning prep with context

Milestone: "Research Go vs Rust for this project" → research skill injected.
           "Remember that port 5433 is staging DB" → note-capture skill.
Exit criteria: slog shows `skill resolved=research path=... est_tokens=N`
```

**Week 4** — Add session persistence and post-turn hooks. `SessionManager.LoadAll()` on startup. `PersistSessionHook` appends JSONL. `LogMetricsHook` appends to tool-calls.jsonl. Graceful shutdown via `signal.NotifyContext`.

```
Milestone: Kill process → restart → previous session context loads.
Exit criteria: Operator sends 3 messages, kills process, restarts, message 4
              references context from messages 1-3. Hello World complete.
```

**What you skip in Phase 1:** Cron scheduler, code sandbox, GitHub integration, session compaction, group chat.

**Cost estimate:** $70-250/month (LLM API + VPS)

---

### Phase 2: Production System (Months 2-4)

**Goal:** Full agent suite with durable scheduling, code tools, and automated learning.

**Add:**

| Feature | Go approach |
|---------|------------|
| Cron scheduler | `github.com/robfig/cron/v3` — daily briefing at 7am, weekly review, dream cycle |
| Code sandbox | Docker exec via `os/exec` — sandboxed shell tool for operator sessions only |
| GitHub API | `github.com/google/go-github/v60` — PR creation, branch management |
| Session compaction | Haiku `CallExtraction` call after `MaxTurnsBeforeCompaction` — flush → summarise → truncate |
| Memory flush hook | `MemoryFlushHook` in post-turn hooks — Haiku extracts facts → `gbrain_put` |
| Idempotency store | Persist UUID cache to disk (LevelDB or SQLite) — survives restarts |
| Discord adapter | `github.com/bwmarrin/discordgo` — same `ChannelAdapter` interface |
| Group session sandbox | Prefix `group:` → restrict to `gbrain_*` tools only; no shell/code |
| Prompt cache metrics | Track `CacheReadTokens` ratio; alert if hit rate < 60% on identity block |

**Cost estimate:** $200-600/month

---

### Phase 3: Full Ownership (Months 6-12)

**Goal:** Self-contained system. No managed service dependencies except LLM APIs.

**Add:**
- **Deploy skill** with GitHub Actions integration + approval gates (human in the loop via Telegram)
- **Skillify loop** — agent converts repeated failures into permanent SKILL.md files
- **Custom memory layer** if GBrain's becomes limiting (PostgreSQL + pgvector + pgx/v5)
- **Evaluation framework** — golden dataset tests for skill outputs
- **Model routing via OpenRouter** — route tasks to optimal models
- **Monitoring** — task success rate, token costs, p95 latency (Prometheus + Grafana, or structured slog + vector)

**Cost estimate:** $300-1,000/month

---

## 2. Critical Patterns to Adopt from OpenClaw

### MUST ADOPT:

**Session Resolution Model** — Every message maps to a session type with security boundaries. Session IDs follow `{scope}:{channel}:{identifier}`. The `main:` prefix grants full tool access; `group:` prefix triggers sandboxing. The `SessionManager` is ID-agnostic — it stores and returns sessions without interpreting permissions.

**System Prompt Composition** — AGENTS.md (rules) + SOUL.md (personality) + USER.md (operator profile) form the stable identity block. ONE skill is injected per turn. Never dump all skills. Prompt caching marks the last identity block with `CacheControl: &anthropic.CacheControlEphemeralParam{}`.

**Session Compaction with Memory Flush** — Always extract facts to GBrain BEFORE compacting conversation history. Without this, compaction destroys knowledge. Order: flush turns to brain via Haiku → summarise → truncate `session.Turns` → write compaction line to JSONL.

**Idempotency Keys** — UUID per tool call, checked with double-checked locking before execution. Prevents double-deploys, double-PRs on Telegram message retries or process crashes. See `ToolRegistry.Execute` in PRD-01.

**Channel Adapter Interface** — Normalized Go interface per platform. All adapters implement the same contract; the runtime holds `[]ChannelAdapter`.

```go
// adapters/adapter.go
type ChannelAdapter interface {
    Start(ctx context.Context) error
    Stop() error
    Send(ctx context.Context, sessionID, text string) error
    SendStreaming(ctx context.Context, sessionID string, tokens <-chan string) error
}

// MessageHandler is injected at construction. Adapters call it for every inbound message.
type MessageHandler func(ctx context.Context, msg Message) error
```

**Streaming via Go channels** — LLM tokens flow over a `chan string`. The adapter goroutine consumes the channel and delivers tokens to the user. The channel is closed when the final turn completes. The adapter must drain the channel fully even if `ctx` is cancelled.

```go
// LLM caller opens a channel; adapter consumes it.
tokenCh := make(chan string, 64)
go func() {
    resp, err := llmCaller.Call(ctx, messages, systemPrompt, tokenCh)
    // tokenCh is closed inside Call when streaming ends
}()
adapter.SendStreaming(ctx, sessionID, tokenCh)
```

**Post-Turn Hooks via errgroup** — Hooks run concurrently after every response. Failures never propagate. A 5-second deadline prevents slow hooks from blocking the next message.

```go
// core/hooks.go
func RunHooks(ctx context.Context, hooks []HookFunc, session *Session, turn SessionTurn) {
    hookCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()
    g, gCtx := errgroup.WithContext(hookCtx)
    for _, h := range hooks {
        h := h
        g.Go(func() error {
            if err := h(gCtx, session, turn); err != nil {
                slog.ErrorContext(gCtx, "post-turn hook failed", "err", err)
            }
            return nil // always nil — errors are logged, never propagated
        })
    }
    _ = g.Wait()
}
```

### CAN SKIP (Phase 1):

- Canvas / A2UI (web UIs in chat — irrelevant for text operator)
- Voice Wake / Talk Mode
- Multi-Agent Routing (one agent, one brain, multiple channels)
- Device Pairing
- Docker Sandboxing per Session (add only when exposing to others)
- Plugin Loader / Extension System (import directly in Go)

---

## 3. AKB48 Component Breakdown

```
~/.akb48/
├── cmd/
│   └── akb48/
│       └── main.go             # Entry point: parse flags, signal handling, start runtime
│
├── internal/
│   ├── config/
│   │   └── config.go           # Config struct, TOML loader, Validate() error
│   ├── runtime/
│   │   └── runtime.go          # AKB48Runtime — wires all components, HandleMessage loop
│   ├── llm/
│   │   └── llm.go              # LLMCaller — anthropic-sdk-go wrapper, streaming tool loop
│   ├── tools/
│   │   └── tools.go            # ToolRegistry — Register/Execute/Definitions, idempotency
│   ├── brain/
│   │   ├── bridge.go           # GBrainBridge — lifecycle, health monitor, registration
│   │   ├── mcp_client.go       # MCPClient — subprocess, JSON-RPC 2.0 over stdio
│   │   └── types.go            # ToolDefinition, BrainConfig, ErrBrainUnavailable
│   ├── skills/
│   │   └── resolver.go         # SkillResolver — hot-reload, trigger matching, parseFrontmatter
│   ├── assembly/
│   │   └── context.go          # ContextAssembler — identity + skill + session → system prompt
│   ├── session/
│   │   ├── manager.go          # SessionManager — ResolveOrCreate, Persist, LoadAll
│   │   └── hooks.go            # HookFunc, RunHooks, PersistSessionHook, LogMetricsHook
│   └── types/
│       └── types.go            # Message, Response, ToolCall, ToolResult, TokenUsage, SessionTurn
│
├── adapters/
│   ├── adapter.go              # ChannelAdapter interface + MessageHandler type
│   ├── cli/
│   │   └── cli.go              # ~50 lines: bufio.Scanner + io.Writer
│   └── telegram/
│       ├── telegram.go         # TelegramAdapter: Start, Stop, Send, SendStreaming
│       └── split.go            # splitMessage pure function (paragraph-aware)
│
├── skills/
│   ├── research/SKILL.md
│   ├── note-capture/SKILL.md
│   ├── daily-briefing/SKILL.md
│   └── ...                     # Add skills by adding a SKILL.md. No code changes.
│
├── identity/
│   ├── AGENTS.md               # Operational rules (always loaded, always in system prompt)
│   ├── SOUL.md                 # Personality and tone
│   └── USER.md                 # Operator profile (read-only, loaded at startup)
│
├── sessions/
│   └── <session_id>.jsonl      # Append-only session logs. One file per session.
│
├── logs/
│   └── tool-calls.jsonl        # Audit trail for every LLM call and tool invocation
│
├── go.mod                      # module github.com/dydanz/akb48
├── go.sum
└── config.toml
```

**Total estimated code:** ~2,000–2,500 lines of Go across all packages. No framework — just the standard library plus four well-chosen dependencies.

**Key dependency list:**

```go
// go.mod
module github.com/dydanz/akb48

go 1.25

require (
    github.com/BurntSushi/toml              v1.4+   // config
    github.com/anthropics/anthropic-sdk-go  v1.38+  // LLM SDK
    github.com/google/uuid                  v1.6+   // idempotency keys
    github.com/go-telegram-bot-api/telegram-bot-api/v5 v5.7+ // Telegram
    golang.org/x/sync                       v0.10+  // errgroup for hooks
    gopkg.in/yaml.v3                        v3.0+   // SKILL.md frontmatter
)
```

---

## 4. Concurrency Model

Go's concurrency primitives map cleanly onto AKB48's architecture:

| Concern | Pattern |
|---------|---------|
| Multiple adapters running in parallel | Each `adapter.Start(ctx)` in its own goroutine, managed by `errgroup` in runtime |
| Streaming LLM tokens to adapter | `chan string` (buffered, capacity 64); LLM goroutine writes, adapter goroutine reads |
| Session-serialized message handling | `sync.Map[string]*sync.Mutex` keyed by `sessionID`; one in-flight turn per session |
| Post-turn hooks (concurrent, fire-and-forget) | `errgroup.WithContext` with 5s deadline per hook batch |
| MCP request/response matching | `sync.Map[int64]chan json.RawMessage` in `MCPClient` (see PRD-03) |
| Session cache (many readers, rare writes) | `sync.Map` for O(1) concurrent reads |
| Tool idempotency cache | `sync.Map[string]ToolResult` in `ToolRegistry` |
| Health monitor goroutine | Owned by `GBrainBridge`; cancelled via `ctx.Done()` |

**Goroutine lifecycle rule:** Every goroutine must have a clear owner and a clear exit condition (`ctx.Done()`, channel close, or explicit `Stop()` call). No fire-and-forget goroutines in the hot path.

**Graceful shutdown sequence:**

```go
// cmd/akb48/main.go
ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer cancel()

if err := rt.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
    slog.Error("runtime exited with error", "err", err)
    os.Exit(1)
}

shutdownCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
defer done()
rt.Shutdown(shutdownCtx)
```

SIGINT/SIGTERM cancels `ctx` → all adapters' `Start` loops exit → runtime waits via errgroup → 10s graceful shutdown for in-flight messages and file handle flushes.

---

## 5. Risks & Mitigations

### Over-complexity
**Risk:** Building 5 agents + memory + cron + sandbox before one agent works.
**Mitigation:** Week 1 = one CLI loop. Week 2 = add brain. Week 3 = add skills. Week 4 = persistence. One component at a time. The Go type system catches interface mismatches at compile time — mistakes are cheap to find early.

### Hallucination
**Risk:** Agent generates wrong code, incorrect deploy commands, fabricated research.
**Mitigation:**
- Never auto-merge code (PRs with human review via Telegram approval gate)
- Never auto-deploy to production without explicit operator approval (AGENTS.md rule)
- Research agent must cite sources
- Use `json.RawMessage` schemas to constrain tool inputs

### Debugging Difficulty
**Risk:** When a multi-step tool chain fails, which step broke? LLM non-determinism makes reproduction hard.
**Mitigation:**
- `tool-calls.jsonl` captures every call: timestamp, session_id, tool, input omitted (security), output length, latency, is_error
- Each `SessionTurn` in the JSONL has `tool_calls` with name + output snippet — read with `jq`
- Idempotency keys let you safely replay failed sessions without double-execution
- `slog.JSONHandler` on stderr: grep by `session_id` to reconstruct a full turn

### Ecosystem Fragmentation
**Risk:** OpenClaw community crisis → fragmented. Building on a fragmenting ecosystem.
**Mitigation:** AKB48 owns its runtime. Go's stdlib covers 80% of the needs. The four non-stdlib dependencies (anthropic-sdk-go, BurntSushi/toml, uuid, telegram-bot-api) are all stable, independently maintained libraries. If GBrain stalls: PostgreSQL + pgvector + pgx/v5 are battle-tested fallbacks.

### Go-Specific: Goroutine Leaks
**Risk:** A goroutine that blocks on a channel after its context is cancelled leaks indefinitely.
**Mitigation:**
- Every `chan string` in the streaming path must be drained to completion or the sender/receiver must respect `ctx.Done()` with a `select`
- `SendStreaming` in adapters must drain `tokens` even after `ctx` is cancelled (prevents LLM goroutine from blocking on channel send)
- Use `go vet` and `goleak` in tests to catch leaks

### Go-Specific: Race Conditions
**Risk:** Concurrent reads/writes to session state from the message handler goroutine and hook goroutines.
**Mitigation:**
- `Session` has its own `sync.RWMutex` — `AddTurn` acquires write lock, `GetContextTurns` acquires read lock
- `ToolRegistry` uses `sync.RWMutex` to protect the handlers map
- Run `go test -race ./...` in CI

### Skill Drift
**Risk:** After months of accumulating skills, 15% become unreachable.
**Mitigation:**
- `SkillResolver.List()` exposes all skill metadata — build a `/skills` Telegram command in Phase 2
- Periodic audit: does each trigger still match real operator messages?
- Keep skill count manageable (start with 5, grow to 15-20 max)

### Token Cost Explosion
**Risk:** Agentic workflows use 10-100x more tokens than simple chat.
**Mitigation:**
- Prompt caching: identity layer (AGENTS.md + SOUL.md + USER.md) is identical every turn — mark with `cache_control`. Target: 70%+ cache hit rate on the identity block
- Only ONE skill injected per turn (deterministic routing vs. "inject all skills")
- Haiku for extraction ($0.25/MTok), Sonnet for generation ($3/MTok) — 12× cost differential
- `TokenUsage.CacheReadTokens` tracked per turn → alert if cache hit rate drops below 60%

### Security
**Risk:** Agent with access to shell, GitHub, and deploys is a significant attack surface.
**Mitigation:**
- Telegram allowlist — `allowed_user_ids` in config; silent rejection for all others
- Session-scoped permissions via session ID prefix (`main:` vs `group:`)
- Idempotency keys prevent duplicate side-effects on Telegram message retries
- All tool calls logged to `tool-calls.jsonl` before and after execution
- Secrets via env vars only — `config.toml` stores key names, not values
- Docker sandbox for code execution (Phase 2)

### Session Data Loss
**Risk:** JSONL files grow large, corrupt, or get deleted.
**Mitigation:**
- Append-only JSONL — no rewrite, no truncation. Corrupt lines are skipped with `slog.Warn` on load
- Size guard: `slog.Warn` when any session file exceeds `max_file_size_mb` (default 10MB)
- Memory flush before compaction — facts go to GBrain first, then history is compacted
- Daily backup of `~/.akb48/sessions/` directory

---

## 6. Cost Model

| Component | Phase 1 | Phase 2 | Phase 3 |
|-----------|---------|---------|---------|
| Claude Sonnet 4.6 API | $50-200/mo | $150-400/mo | $200-600/mo |
| Claude Haiku 4.5 API | $5-20/mo | $20-50/mo | $30-100/mo |
| VPS (Hetzner CX22 or similar) | $20/mo | $40/mo | $60-100/mo |
| GBrain (self-hosted, MIT) | $0 | $0 | $0 |
| Supabase (if gbrain uses it) | $0 (free tier) | $25/mo | $25/mo |
| Domain + Tailscale | $0-10/mo | $0-10/mo | $0-10/mo |
| **Total** | **$75-250/mo** | **$235-525/mo** | **$315-835/mo** |

**Prompt caching impact:** With 70%+ cache hits on the identity block (~2,000 tokens), the effective input cost per turn drops by ~40%. At 50 turns/day, caching saves ~$30-60/month vs uncached.

---

## 7. What to Avoid

1. **LangGraph/CrewAI/AutoGen** — Wrong abstraction for a solo operator with skill files
2. **Python/Node for the runtime** — Decision made: Go. Single binary, no venv, no `package.json`. `go build` → done.
3. **Fine-tuning** — Exhaust prompt engineering + GBrain + skill files first
4. **Autonomous production deployments** — Human approval gate always (Telegram inline keyboard in Phase 2)
5. **Premature multi-agent complexity** — One well-prompted agent with good tools beats a poorly coordinated 5-agent system
6. **Storing everything in memory** — Implement forgetting from day one. Stale memories actively harm performance
7. **Treating RAG as memory** — GBrain uses hybrid search (vector + keyword + graph + RRF). Memory is structured, evolving, agent-writable knowledge — not document retrieval
8. **Purpose-built vector databases** (Pinecone, Weaviate) — PostgreSQL + pgvector is sufficient. GBrain handles this. Don't add infra you don't need
9. **HTTP transport for MCP** in Phase 1 — stdio is simpler, faster, and co-located. Add HTTP SSE only when GBrain needs to run on a different host
10. **Goroutines without lifecycle management** — Every goroutine must respect `ctx.Done()`. Use `errgroup` to manage groups. Never fire-and-forget in the hot path
