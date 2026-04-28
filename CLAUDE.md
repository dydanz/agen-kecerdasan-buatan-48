# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

---

## Project Overview

Klawmbing is a **thin, self-hosted AI agent runtime** ("claw") for a solo operator. It connects Telegram/Discord to a compounding knowledge brain (GBrain), routes user intent to markdown skill files, and gets smarter without code deploys. The philosophy is **"thin harness, fat skills"**: the runtime is ~2,000–2,500 lines of Python; the intelligence lives in skill files and GBrain.

The project is currently in **design/planning phase**. All PRDs are in `klawmbing-prd/`, research in `klawmbing-rsh/`. No implementation exists yet. The first milestone is "Hello World" (PRDs 01–05), which proves the chat → LLM → brain → persistence pipeline end-to-end.

---

## Running the Project

```bash
# Start Klawmbing (entry point)
python klawmbing.py

# Validate config without starting
python klawmbing.py --validate

# Use a custom config file
python klawmbing.py --config path/to/config.toml

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
Session Resolver → loads JSONL session from ~/.klawmbing/sessions/
        │
        ▼
Context Assembler
    ├── AGENTS.md + SOUL.md + USER.md (always, cached via prompt caching)
    ├── Skill resolver → injects ONE SKILL.md (if intent matches)
    ├── Session history (last N turns as Claude API messages)
    └── Brain context (LLM decides to search via tools, not pre-fetched)
        │
        ▼
LLM Caller (Claude Sonnet 4.6, streaming, asyncio)
        │
        ▼
Tool Executor (idempotency-checked UUID per call)
    ├── gbrain_* tools (all exposed via MCP from `gbrain serve`)
    ├── Web search
    └── GitHub API (Phase 2)
        │
        ▼
Post-Turn Hooks (fire-and-forget, never break main loop)
    ├── Persist session turn → JSONL append
    └── Log metrics → tool-calls.jsonl
```

**Model routing:**
- `claude-sonnet-4-6` — generation, reasoning, code
- `claude-haiku-4-5` — fact extraction, summarization (12× cheaper; use for all extraction)

---

## Project Directory Layout

```
~/.klawmbing/               # Runtime root
├── klawmbing.py            # Entry point
├── config.toml             # Config (no secrets — use env vars)
├── core/
│   ├── config.py           # Config loader + pydantic validation
│   ├── runtime.py          # KlawmbingRuntime — wires all components
│   ├── llm.py              # LLMCaller — Anthropic SDK, streaming, tool loop
│   ├── tools.py            # ToolRegistry — register/dispatch/log tool calls
│   └── types.py            # Shared dataclasses: Message, Response, ToolCall, etc.
├── adapters/
│   ├── base.py             # ChannelAdapter ABC
│   ├── cli.py              # stdin/stdout (~50 lines)
│   └── telegram.py         # python-telegram-bot v21+, long-polling
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
└── logs/
    └── tool-calls.jsonl    # Audit trail for every LLM call and tool invocation
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
The identity layer (AGENTS.md + SOUL.md + USER.md) is identical every turn — mark it with `cache_control` for Claude's prompt caching. The skill injection sits after the cache boundary since it changes per turn.

### Post-Turn Hooks
Hooks execute after every agent response via `asyncio.gather`. **Failures in hooks must never propagate to the main loop** — catch all exceptions, log them, continue. Built-in hooks: session persistence + metrics logging. Phase 2 adds memory flush and compaction check.

### GBrain MCP Connection
GBrain is managed as a child subprocess (`gbrain serve` over stdio). Klawmbing:
1. Spawns the process at startup
2. Discovers tools dynamically via `tools/list` (don't hardcode tool names)
3. Registers all tools with `gbrain_` prefix in ToolRegistry
4. Health-checks every 30s, auto-restarts up to 3 times
5. If brain is unavailable: all `gbrain_*` tools return an error string; the LLM handles it gracefully; the process continues (degraded mode)

### Session Persistence
JSONL files, one per session. **Append-only** — each turn is one line. Never rewrite the full file. On startup, load the most recent session file per session ID. Session file naming: replace `:` with `_` (e.g., `main_telegram_123456789.jsonl`). Warn if any file exceeds 10MB.

---

## Configuration Reference

`config.toml` structure (keys that matter for implementation):

```toml
[llm]
model = "claude-sonnet-4-6-20260326"
extraction_model = "claude-haiku-4-5-20251001"
api_key_env = "ANTHROPIC_API_KEY"   # Read from env, never stored

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
```

Fail fast on startup for any missing required field. Use `pydantic` for config validation (not stdlib).

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
