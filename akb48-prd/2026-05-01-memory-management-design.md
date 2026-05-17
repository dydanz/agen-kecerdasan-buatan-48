# Memory Management Design — Cache-First Layered Context

**Date:** 2026-05-01
**Status:** Approved
**Scope:** PRD review and redesign of context assembly, session caching, org knowledge taxonomy, and cost guards

---

## Problem Statement

The AKB48 PRDs (01–05) correctly establish a layered memory model (brain, session, identity, skills) but leave four gaps that limit effectiveness and inflate cost:

1. Session history is sent uncached every turn — the largest token block, with no cache strategy
2. Cold sessions start dumb — the agent has to discover what it knows via tool-call round trips
3. Org knowledge has no taxonomy — GBrain is treated as a bag of strings, degrading retrieval as it grows
4. No guardrails on tool loops or tool result size — cost can spike silently

This design adds a fourth PRD-level concern: **context assembly as a first-class cost and quality concern**.

---

## Design Overview

Option A — Cache-first layered context — was selected over:
- Option B (Haiku enrichment pipeline per turn — over-engineered before usage data)
- Option C (minimal delta — leaves cold sessions and org taxonomy unaddressed)

The design has four components:
1. Four-tier context assembly pipeline with explicit cache boundaries
2. Cold session opener (hybrid LLM-driven + proactive warm-up)
3. Org knowledge taxonomy for GBrain
4. Cost guards (tool rounds, result truncation, token budget)

---

## Component 1 — Context Assembly Pipeline

Context is assembled as four explicit tiers, ordered to maximise prompt cache hits.

```
┌─────────────────────────────────────────────────────────┐
│  TIER 1 — Identity  [cache_control: ephemeral]          │
│  AGENTS.md + SOUL.md + USER.md + resolved SKILL.md      │
│  ~500–2000 tokens. Stable per session. Always cached.   │
│                                                         │
│  TIER 2 — Session history tail  [cache_control: eph.]   │
│  All turns except the last 2. Append-only, never edits. │
│  Cached after turn 1. Cache hit on every subsequent msg.│
│                                                         │
│  TIER 3 — Cold opener context  [no cache]               │
│  Injected ONCE on session start or resume (>30min gap). │
│  Top-3 brain recall results for this session's topic.   │
│  Cleared after turn 2 (no longer "cold").               │
│                                                         │
│  TIER 4 — Live turn  [no cache]                         │
│  Last 2 turns + current user message + new tool results.│
│  Always fresh, never cached.                            │
└─────────────────────────────────────────────────────────┘
```

### Why this order

Claude's prompt cache is prefix-based. A cache hit requires the prefix to be byte-identical. Tiers 1 and 2 are stable (identity never changes mid-session; old session turns are never edited). Putting them first and dynamic content last means cache hits compound across turns: Tier 1 hits from turn 1, Tier 2 grows its cached prefix with every appended turn.

A 30-turn session at Sonnet pricing saves roughly 85–90% on Tier 1+2 tokens versus the uncached baseline in the current PRD design.

### Skill placement

SKILL.md is placed at the end of Tier 1 (not in a separate tier). A skill change busts the Tier 1 cache but not the Tier 2 cache. Since Tier 2 token savings dominate, this is acceptable. Skills change infrequently within a session.

### Tier 3 and 4 are intentionally small

Tier 3 is capped at ~400 tokens (3 brain results). Tier 4 is the last 2 turns + current message — typically 200–600 tokens. The uncached portion of every turn stays under ~1,000 tokens in normal use.

### ContextAssembler changes (vs PRD-04)

The existing `ContextAssembler` in PRD-04 is extended with:
- `cacheBoundaryAfter`: field on the `ContextAssembler` struct that marks which message index gets `cache_control: ephemeral` appended
- Tier 2 assembly: collects `session.Turns[:len(session.Turns)-2]` as cached messages, `session.Turns[len(session.Turns)-2:]` into Tier 4
- Tier 3 slot: a `coldContext string` parameter passed to `Build()`, inserted between Tier 2 and Tier 4; empty string means no cold context

---

## Component 2 — Cold Session Opener

### When it fires

```go
isCold := session.TurnCount() == 0 ||
    time.Since(session.UpdatedAt) > time.Duration(cfg.ColdResumeThresholdMinutes)*time.Minute
```

Default threshold: 30 minutes. Configurable via `session.cold_resume_threshold_minutes`.

### What it does

1. Extract a lightweight search query from the user message: first 10–15 words, stop words stripped. No Haiku call — simple heuristic, zero extra latency beyond the brain search.
2. Run `gbrain_search(query, limit=3, entity_types=["person","project","decision","product","policy"])`.
3. Format results into a compact Tier 3 block:

```
[What I recall that may be relevant]
- Dandi decided to use PostgreSQL + pgvector for brain storage (decision, 2025-03-15)
- GBrain serve must be running before akb48 starts (project:akb48)
- Startup sequence: gbrain serve → ./akb48 (project:akb48)
```

4. On subsequent turns, `isCold` naturally evaluates to `false`: `TurnCount() > 0` and `UpdatedAt` is recent. No extra flag required. Tier 3 is omitted automatically.

### Fallback

If the brain is in degraded mode, the cold opener is skipped silently. No error surfaced to the user.

### Why heuristic, not Haiku

Cold opens are rare (once per session). At that frequency, Haiku adds ~200ms and negligible cost. The reason to prefer heuristic is simplicity: no failure mode, no extra SDK call, no extra error handling path.

---

## Component 3 — Org Knowledge Taxonomy

### Five entity types

| Type | What it captures | Example |
|------|-----------------|---------|
| `person` | Team members — role, timezone, working style | "Andi — backend lead, UTC+8, Go-primary" |
| `project` | Active and past projects — status, tech stack | "github.com/dydanz/akb48 — self-hosted agent runtime, Go, planning phase" |
| `decision` | Architectural/product/business decisions with rationale | "Chose stdio MCP over HTTP SSE — simpler, co-located VPS" |
| `product` | Products you operate or build, their boundaries | "GBrain — knowledge brain, separate lifecycle from akb48" |
| `policy` | Standing rules and constraints | "Never push to main — always PRs, always human approval" |

### Entity schema

```json
{
  "type": "decision",
  "title": "Chose PostgreSQL + pgvector for brain storage",
  "body": "Selected over purpose-built vector DBs. GBrain handles all indexing. Hybrid search: vector + keyword + graph.",
  "tags": ["github.com/dydanz/akb48", "gbrain", "infra"],
  "scope": "org",
  "created_at": "2025-03-15T10:00:00Z"
}
```

### `scope` as the multi-user boundary

Phase 1: all entities use `scope: org` (shared, operator-accessible from any session).

Phase 2: group sessions use `scope: group:telegram:{chat_id}`. The cold opener and LLM tool calls filter by scope — group sessions cannot read org-scoped decisions unless explicitly granted. This is the permission hook for multi-user without a schema redesign.

### What note-capture skill must write

The note-capture SKILL.md is updated to instruct the LLM to extract a typed, tagged, scoped entity before calling `gbrain_put` — not a raw string. The skill prompt includes:

```
Before storing, identify:
- type: one of person / project / decision / product / policy
- title: one sentence
- body: 2-3 sentences with full context and rationale
- tags: 1-3 relevant project or domain tags
- scope: org (default)
Then call gbrain_put with the structured entity.
```

This is the difference between a searchable knowledge graph and a bag of strings.

### USER.md relationship

USER.md remains as the static startup bootstrap — the LLM's initial operator context. The agent is also instructed (in AGENTS.md) to maintain a live `person` entity for the operator in GBrain. Over time, GBrain holds the canonical dynamic profile; USER.md is just the seed.

---

## Component 4 — Cost Guards

### Max tool rounds

```toml
[llm]
max_tool_rounds = 5
```

The LLM caller in `internal/llm/llm.go` tracks round count per LLM call. On hitting the limit, the tool loop exits and the last assistant message is returned. A warning is logged. No error surfaced to the user. Five rounds is generous for normal use; runaway loops typically reach 8–15 before this fires.

### Tool result truncation

```toml
[llm]
max_tool_result_tokens = 500
```

Before appending any tool result to message history, the tool registry truncates to `max_tool_result_tokens` and appends `[truncated]`. For brain search results, the structure prioritises title + 2-sentence summary; full body is included only when `limit=1`.

### Session history token budget

The PRD sets `max_turns_in_context = 50` as a turn count. Added: a complementary token ceiling.

```toml
[session]
max_turns_in_context = 50
max_context_tokens = 32000
```

When assembling Tier 2, token count from newest turn to oldest, stopping when the budget is hit. Oldest turns are dropped first — this preserves the longest stable cache prefix (dropping from the front keeps the cached tail intact) and prevents context window exhaustion.

### Steady-state cost estimate (30-turn session, Sonnet pricing)

| Layer | Tokens | Cached? | Cost vs uncached |
|-------|--------|---------|-----------------|
| Identity + skill | ~1,500 | Yes (Tier 1) | ~5% |
| Session history | ~12,000 | Yes (Tier 2) | ~5% |
| Cold opener | ~400 | No (Tier 3, first turn only) | Full rate, once |
| Live turn | ~400 | No (Tier 4) | Full rate |
| Tool results | ≤500/call | No | Full rate |

Effective cost reduction on the dominant token blocks (Tier 1+2): ~85–90% versus the current PRD baseline.

---

## PRD Impact Summary

| PRD | Change |
|-----|--------|
| PRD-01 (Core Runtime) | Add `max_tool_rounds`, `max_tool_result_tokens` to config schema and LLM caller |
| PRD-03 (GBrain) | Cold opener uses `gbrain_search` with `entity_types` filter; update tool call signature |
| PRD-04 (Identity & Skills) | ContextAssembler gains 4-tier structure with `cache_control` boundaries; note-capture SKILL.md updated with entity prompt template |
| PRD-05 (Session) | SessionManager gains `isCold` detection, `cold_resume_threshold_minutes` config, `max_context_tokens` cap, Tier 2 cache assembly |

No changes needed to PRD-02 (Channel Adapters).

---

## What was kept from the original PRDs

- LLM-driven brain search as the default (not pre-fetch on every turn)
- One skill per turn
- Degraded mode for brain unavailability
- Haiku for extraction tasks (Phase 2)
- Append-only JSONL sessions
- Post-turn hooks as fire-and-forget
- Manual Phase 1 knowledge extraction (note-capture skill, not auto-extraction)

---

## Open questions (not blocking Phase 1)

- GBrain `entity_types` filter: confirm this is a supported parameter in `gbrain_search` or add it as a tag-based workaround
- `max_context_tokens` token counting: use `anthropic.CountTokens()` or `len(text) / 4` heuristic (heuristic acceptable for Phase 1)
- Phase 2 `scope` enforcement: needs access control design when group sessions are introduced
