# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

---

## Project Overview

AKB48 (Agen Kecerdasan Buatan 48) is a **thin, self-hosted AI agent runtime** ("claw") for a solo operator. It connects Telegram/Discord to a compounding knowledge brain (GBrain), routes user intent to markdown skill files, and gets smarter without code deploys. The philosophy is **"thin harness, fat skills"**: the runtime is ~2,000–2,500 lines of Go; the intelligence lives in skill files and GBrain.

The project is currently in **design/planning phase**. All PRDs are in `akb48-prd/`, research in `akb48-rsh/`, and development/implementation plan are in `akb48-dev-plan/`. The first milestone is "Hello World" (PRDs 01–05), which proves the chat → LLM → brain → persistence pipeline end-to-end.

---

## Running the Project

```bash
# Build and run
go build -o akb48 ./cmd/akb48/
./akb48

# Run without building
go run ./cmd/akb48/

# Validate config without starting
./akb48 --validate

# Use a custom config file
./akb48 --config path/to/config.toml

# GBrain must be running first (PRD-03 manages this automatically)
gbrain serve
```

Environment variables required (never put secrets in `config.toml`):
```bash
export ANTHROPIC_API_KEY=...
export TELEGRAM_BOT_TOKEN=...   # if telegram adapter enabled
```

---

## Architecture

```
Telegram / Discord / CLI
        │
        ▼
ChannelAdapter (normalized interface)
        │
        ▼
Session Resolver → loads JSONL session from ~/.akb48/sessions/
        │
        ▼
Context Assembler
    ├── AGENTS.md + SOUL.md + USER.md (always, cached via prompt caching)
    ├── Skill resolver → injects ONE SKILL.md (if intent matches)
    ├── Session history (last N turns as Claude API messages)
    └── Brain context (LLM decides to search via tools, not pre-fetched)
        │
        ▼
LLM Caller (Claude Sonnet 4.6, streaming, goroutines + channels)
        │
        ▼
Tool Executor (idempotency-checked UUID per call)
    ├── gbrain_* tools (all exposed via MCP from `gbrain serve`)
    ├── Web search
    └── GitHub API (Phase 2)
        │
        ▼
Post-Turn Hooks (fire-and-forget via errgroup, never break main loop)
    ├── Persist session turn → JSONL append
    └── Log metrics → tool-calls.jsonl
```

**Model routing:**
- `claude-sonnet-4-6` — generation, reasoning, code
- `claude-haiku-4-5` — fact extraction, summarization (12× cheaper; use for all extraction)

**Streaming:** LLM tokens are sent over a `chan string`; the adapter goroutine consumes the channel and forwards to the user.

---

## Project Directory Layout

```
~/.akb48/               # Runtime root
├── cmd/akb48/
│   └── main.go             # Entry point
├── internal/
│   ├── config/
│   │   └── config.go       # Config struct + TOML loader
│   ├── runtime/
│   │   └── runtime.go      # AKB48Runtime — wires all components
│   ├── llm/
│   │   └── llm.go          # LLMCaller — Anthropic SDK, streaming, tool loop
│   ├── tools/
│   │   └── tools.go        # ToolRegistry — register/dispatch/log tool calls
│   └── types/
│       └── types.go        # Shared types: Message, Response, ToolCall, etc.
├── adapters/
│   ├── adapter.go          # ChannelAdapter interface
│   ├── cli/
│   │   └── cli.go          # stdin/stdout (~50 lines)
│   └── telegram/
│       └── telegram.go     # go-telegram-bot-api/v5, long-polling
├── skills/
│   ├── RESOLVER.md         # Intent → skill routing table (for reference)
│   ├── note-capture/SKILL.md
│   ├── research/SKILL.md
│   └── ...                 # Add new skills by adding a SKILL.md file
├── identity/
│   ├── AGENTS.md           # Operational rules (what agent can/cannot do)
│   ├── SOUL.md             # Personality and tone
│   └── USER.md             # Operator profile (loaded at startup, not agent-modifiable)
├── sessions/               # <session-id>.jsonl — append-only, one file per session
├── logs/
│   └── tool-calls.jsonl    # Audit trail for every LLM call and tool invocation
├── go.mod
├── go.sum
└── config.toml
```

GBrain lives separately at `~/brain/` — it has its own lifecycle independent of the claw runtime.

---

## Key Design Patterns

### Session IDs
Format: `{scope}:{channel}:{identifier}`

| Session | ID | Permissions |
|---------|-----|-------------|
| Operator via CLI | `main:cli:local` | Full (no sandbox) |
| Operator via Telegram DM | `main:telegram:{user_id}` | Full |
| Group chat (Phase 2) | `group:telegram:{chat_id}` | Sandboxed |

The `main:` prefix = full tool access. `group:` prefix = restricted.

### Skill Resolution
Skill files have YAML frontmatter with `triggers` (keyword phrases). The resolver:
1. Lowercases the message
2. Checks each skill's triggers for substring match
3. Most specific match wins (longest trigger string)
4. Only ONE skill is injected per turn
5. Skills are hot-reloaded on every message (no restart needed after edits)
6. No match → general mode (identity-only context)

### Idempotency
Every side-effecting tool call gets a UUID (`idempotency_key`) checked before execution. This prevents double-deploys and double-PRs on message retries or crashes.

### Prompt Caching
The identity layer (AGENTS.md + SOUL.md + USER.md) is identical every turn — mark it with `cache_control` for Claude's prompt caching via `anthropic-sdk-go`. The skill injection sits after the cache boundary since it changes per turn.

### Tool Registry
Handlers are registered in a `sync.RWMutex`-protected map. Handler signature:

```go
func(ctx context.Context, input json.RawMessage) (string, error)
```

Tool dispatch is logged to `tool-calls.jsonl` before and after execution. Idempotency key is checked before invoking any side-effecting handler.

### Post-Turn Hooks
Hooks execute after every agent response via `errgroup.Go()`. **Failures in hooks must never propagate to the main loop** — recover all panics, log errors, continue. Built-in hooks: session persistence + metrics logging. Phase 2 adds memory flush and compaction check.

### GBrain MCP Connection
GBrain is managed as a child subprocess (`gbrain serve` over stdio). AKB48:
1. Spawns the process at startup
2. Discovers tools dynamically via `tools/list` (don't hardcode tool names)
3. Registers all tools with `gbrain_` prefix in ToolRegistry
4. Health-checks every 30s, auto-restarts up to 3 times
5. If brain is unavailable: all `gbrain_*` tools return an error string; the LLM handles it gracefully; the process continues (degraded mode)

### Session Persistence
JSONL files, one per session. **Append-only** — each turn is one line. Never rewrite the full file. Use `encoding/json` for marshalling. On startup, load the most recent session file per session ID. Session file naming: replace `:` with `_` (e.g., `main_telegram_123456789.jsonl`). Warn if any file exceeds 10MB.

---

## Configuration Reference

`config.toml` structure (keys that matter for implementation):

```toml
[llm]
model = "claude-sonnet-4-6-20260326"
extraction_model = "claude-haiku-4-5-20251001"
api_key_env = "ANTHROPIC_API_KEY"   # Read from env, never stored
max_tool_rounds = 5
max_tool_result_tokens = 500

[adapters.telegram]
token_env = "TELEGRAM_BOT_TOKEN"
allowed_user_ids = [123456789]      # Silently reject all other users
streaming_interval_ms = 1000        # editMessageText cadence

[brain]
gbrain_command = "gbrain"
gbrain_args = ["serve"]
gbrain_working_dir = "~/brain"
tool_prefix = "gbrain"

[session]
max_turns_in_context = 50
max_turns_before_compaction = 30    # Phase 2
max_context_tokens = 32000
cold_resume_threshold_minutes = 30
```

Fail fast on startup for any missing required field. Use struct tags + a validation pass (e.g., `go-playground/validator`) rather than stdlib for config validation.

---

## Identity & Skill Files

### identity/AGENTS.md
Operational rules: what the agent can and cannot do. Always included in system prompt. Key rules (already decided):
- Never push directly to main — always PRs
- Never deploy to production without explicit operator approval
- Never share secrets in chat
- Always be concise (operator reads on mobile)

### identity/SOUL.md
Personality: sharp technical co-founder, thinks in systems, pushes back on over-engineering, never uses emojis or exclamation marks.

### identity/USER.md
Operator profile: Dandi, solo CTO/CEO, Go + Python background, Telegram-primary, UTC+7. Loaded at startup as read-only context. Not modified by the agent.

### Skill file format (SKILL.md)
```yaml
---
name: research
description: >
  One-line description of when this skill applies
triggers:
  - research
  - compare
  - "what are the options for"
---
```
Body: step-by-step process, output format, constraints. Keep under 2,000 tokens. Adding a skill = adding a file; no code changes required.

---

## PRD Structure (Implementation Reference)

| PRD | Component | Dependencies |
|-----|-----------|-------------|
| PRD-01 | Core Runtime — entry point, config, LLM caller, tool registry | — |
| PRD-02 | Channel Adapters — CLI and Telegram | PRD-01 |
| PRD-03 | GBrain Integration — MCP client, tool discovery | PRD-01 |
| PRD-04 | Identity & Skill System — context assembly, skill resolver | PRD-01 |
| PRD-05 | Session Management — JSONL persistence, post-turn hooks | PRD-01, PRD-02 |
| PRD-xx | <Placeholder for future development> | PRD-x1, PRD-x2 |

Read the full PRD before implementing any component. Each PRD contains the complete interface contract, data types, error handling table, and acceptance criteria.

---

## Hard Rules (Non-Negotiable)

- **Never push to main** — the coder agent always opens PRs; never `git push origin main`
- **Never auto-deploy to production** — always require explicit operator approval in chat
- **Secrets via env vars only** — `config.toml` stores key names, not values
- **Code execution in Docker sandbox only** — never execute arbitrary code on the host (Phase 2)
- **Memory flush before compaction** — always extract facts to GBrain before summarizing session history, or knowledge is permanently lost
- **Let the LLM decide when to search brain** — don't pre-fetch brain context on every turn; instruct the LLM in AGENTS.md to search proactively via tools

---

## What to Avoid

- LangGraph, CrewAI, AutoGen — wrong abstraction for this architecture
- Fine-tuning — not until Phase 3 at earliest
- Purpose-built vector databases — PostgreSQL + pgvector (GBrain handles this)
- Autonomous production deploys — human approval always required
- Pre-fetching brain context on every message — let the LLM use tools on demand
- Storing everything in memory — GBrain has forgetting policies; respect them
- Treating RAG as memory — GBrain uses hybrid search (vector + keyword + graph), not plain RAG

## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships.

Rules:
- For codebase questions, first run `graphify query "<question>"` when graphify-out/graph.json exists. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).

---

## SDLC

This project follows a lightweight multi-phase SDLC. Full agent: `.claude/agents/sdlc.md`.

**Three rules always active:**
1. **Draft, don't auto-execute.** Propose every GitHub action (issue, PR, comment). Wait for explicit operator confirmation.
2. **Event-driven.** Act on invocation or hook events. Never poll.
3. **Proportional.** Read `class:low/medium/high/hotfix` from issue labels. Scale ceremony to that class.

| Class | Spec | Gate | Test | Branch |
|---|---|---|---|---|
| `class:low` | None | Self-approval | CI | `fix/<klw-id>-slug` |
| `class:medium` | Spec in issue body | Self `/approve-spec` | `go test ./...` | `feature/<klw-id>-slug` |
| `class:high` | TRD in `akb48-prd/` | Self-approval + 2nd review | Full suite + manual | `feature/<klw-id>-slug` |
| `class:hotfix` | Skip; post-merge ≤24h | Self-approval + smoke | Smoke | `hotfix/<klw-id>-slug` |

**Ticket convention:** KLW-XXX (existing). PR description must include `Closes #N` to link GitHub issue.

**ADRs:** in `akb48-adr/`. Add `adr-required` label to any PR affecting component interfaces, session contracts, runtime wiring, or conventions. ADR bot enforces within 48h of merge.

**Dev plans:** in `akb48-dev-plan/`. Update if implementation deviates from plan.
