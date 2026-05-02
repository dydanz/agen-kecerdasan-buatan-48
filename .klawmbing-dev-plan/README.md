# Klawmbing — Development Plan

**Last Updated:** 2026-05-01
**Language:** Go
**Target:** Hello World milestone (PRD-00 through PRD-05)

---

## Overview

Klawmbing is a thin, self-hosted Go AI agent runtime that connects Telegram/Discord to a compounding knowledge brain (GBrain). This plan decomposes the Hello World milestone into 21 GitHub-ready tickets across 6 phases.

**Hello World Definition of Done (PRD-00 §6):**
1. Operator starts Klawmbing: `./klawmbing`
2. GBrain is running: `gbrain serve`
3. Operator sends "Hello, who are you?" via Telegram → personality-consistent response
4. Operator sends "Remember that staging cluster is ap-southeast-1" → stored in GBrain
5. Operator sends "What do you know about our staging cluster?" → retrieved from GBrain
6. Operator restarts Klawmbing → previous session context available

---

## Phase Summary

| Phase | Name | Goal | Tickets | SP | Status |
|-------|------|------|---------|-----|--------|
| 0 | Foundation | Core Go packages (types, config, tools, LLM) | KLW-001–003 | 11 | ✅ Done |
| 1 | Session & Runtime | CLI agent with persistent session history | KLW-004–009 | 27 | 🔲 |
| 2 | Identity & Skills | Personality, skill routing, 4-tier context caching | KLW-010–014 | 18 | 🔲 |
| 3 | GBrain Integration | MCP client, knowledge query/store, degraded mode | KLW-015–017 | 16 | 🔲 |
| 4 | Telegram Adapter | Mobile chat interface with streaming responses | KLW-018–019 | 10 | 🔲 |
| 5 | Hello World Validation | End-to-end tests + VPS deployment | KLW-020–021 | 10 | 🔲 |

**Total remaining:** 18 tickets · **81 story points**

---

## Dependency Graph

```
KLW-001 (types/config)
    ├── KLW-002 (tool registry)
    │       └── KLW-016 (gbrain bridge)
    ├── KLW-003 (LLM caller)
    │       └── KLW-007 (runtime)
    ├── KLW-004 (session model)
    │       └── KLW-005 (session manager)
    │               └── KLW-006 (hooks)
    │                       └── KLW-007 (runtime)
    │                               ├── KLW-008 (CLI adapter)
    │                               │       └── KLW-009 (entry point)
    │                               ├── KLW-011 (context assembler)
    │                               │       ├── KLW-012 (identity files)
    │                               │       └── KLW-014 (cold opener)
    │                               ├── KLW-017 (gbrain degraded mode)
    │                               └── KLW-018 (telegram core)
    │                                       └── KLW-019 (telegram streaming)
    └── KLW-010 (skill resolver)
            ├── KLW-011 (context assembler)
            └── KLW-013 (skill files)
KLW-015 (MCP client)
    └── KLW-016 (gbrain bridge)
            └── KLW-017 (gbrain degraded mode)
All Phase 1–4 → KLW-020 (integration tests)
                └── KLW-021 (VPS deployment)
```

---

## Story Point Scale

| Points | Size | Typical Duration |
|--------|------|-----------------|
| 1 | Trivial | < 1 hour |
| 2 | Small | 2–4 hours |
| 3 | Medium | ~half day |
| 5 | Large | ~1 day |
| 8 | Max | ~2 days |

---

## GitHub Projects Setup

**Board columns:** `Backlog → In Progress → In Review → Done`

**Labels to create:**
- `phase/1`, `phase/2`, `phase/3`, `phase/4`, `phase/5`
- `type/chore`, `type/user-story`
- `size/S` (1–2), `size/M` (3–5), `size/L` (8)
- `component/session`, `component/identity`, `component/brain`, `component/adapter`, `component/runtime`

**Milestones:**
- `Phase 1: Session & Runtime Core`
- `Phase 2: Identity & Skills`
- `Phase 3: GBrain Integration`
- `Phase 4: Telegram Adapter`
- `Phase 5: Hello World`

---

## Plan Documents

| File | Content |
|------|---------|
| [phase-1-session-runtime.md](./phase-1-session-runtime.md) | KLW-004 through KLW-009 |
| [phase-2-identity-skills.md](./phase-2-identity-skills.md) | KLW-010 through KLW-014 |
| [phase-3-gbrain-integration.md](./phase-3-gbrain-integration.md) | KLW-015 through KLW-017 |
| [phase-4-telegram-adapter.md](./phase-4-telegram-adapter.md) | KLW-018 through KLW-019 |
| [phase-5-hello-world.md](./phase-5-hello-world.md) | KLW-020 through KLW-021 |

---

## Key Architecture Decisions (locked)

| Area | Decision |
|------|----------|
| Language | Go 1.25+ |
| LLM primary | `claude-sonnet-4-6-20260326` |
| LLM extraction | `claude-haiku-4-5-20251001` |
| Anthropic SDK | `github.com/anthropics/anthropic-sdk-go` |
| Telegram | `github.com/go-telegram-bot-api/telegram-bot-api/v5` |
| Config | TOML via `github.com/BurntSushi/toml` |
| YAML skills | `gopkg.in/yaml.v3` |
| Session storage | Append-only JSONL, one file per session |
| Brain transport | stdio (JSON-RPC 2.0 over subprocess) |
| Concurrency | goroutines + channels; `sync.RWMutex` for shared state |
| Post-turn hooks | `golang.org/x/sync/errgroup` with 5s deadline |
| Context caching | 4-tier: Identity (cached) → History (cached) → Cold opener → Live turn |
