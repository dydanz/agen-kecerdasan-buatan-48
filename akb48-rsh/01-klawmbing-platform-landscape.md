# AKB48 — Document 1: Platform Landscape & Architecture Decision

## For: Solo CTO/CEO building a personal AI agent system
## Architecture: GBrain-style (Hermes + skill files + knowledge graph)
## Date: April 2026

---

## 1. Agent Framework Landscape (2026)

### LangGraph (LangChain) — Graph-based workflow orchestration
- **Production readiness:** Highest in 2026. Built-in checkpointing, time-travel debugging via LangSmith, per-node streaming
- **Strengths:** Fine-grained control, conditional branching, deepest MCP integration, model-agnostic, largest community
- **Weaknesses:** Steepest learning curve. State schemas rigid upfront. Over-abstraction complaints. Memory handling via LangChain historically tricky
- **Verdict for AKB48:** NOT NEEDED. LangGraph solves the team-platform orchestration problem. AKB48 uses skill files + a simple intent resolver instead of a code-defined graph

### CrewAI — Role-based team orchestration
- **Strengths:** Fastest prototyping, intuitive mental model, built-in memory, growing A2A support
- **Weaknesses:** No checkpointing, coarse error handling, debugging is painful (logging broken inside Tasks), limited agent-to-agent control
- **Verdict for AKB48:** SKIP. Same category problem — AKB48 doesn't need a multi-agent framework

### Microsoft AutoGen / AG2 — Conversational agent teams
- **Strengths:** Diverse conversation patterns, strong code execution, good for quality-sensitive offline workflows
- **Weaknesses:** Expensive (20+ LLM calls per GroupChat task). Microsoft shifted to maintenance mode. Smallest community
- **Verdict for AKB48:** AVOID. Maintenance mode + high token cost + wrong architecture

### OpenClaw / Hermes Agent — Thin harness, fat skills
- **This is AKB48's category.** The "claw-class" runtime: a thin gateway that receives messages, routes intent to skill files, calls LLM APIs, executes tools, manages sessions
- **OpenClaw:** Hub-and-spoke around a WebSocket Gateway. Channel adapters (Telegram, Discord, WhatsApp, Signal, iMessage). AGENTS.md + SOUL.md + TOOLS.md prompt composition. Session-scoped security. Plugin system
- **Hermes Agent:** Nous Research fork with self-improving skills, FTS5 session search, Honcho user modeling, multi-gateway, auto-migration from OpenClaw
- **Verdict for AKB48:** BUILD YOUR OWN inspired by these. The harness is ~2,000-3,000 lines. The intelligence lives in GBrain + skill files

---

## 2. Architecture Comparison: Blueprint vs GBrain-style

### Blueprint (LangGraph + Temporal + Mem0)

**Pros:**
- Explicit orchestration in code — conditional branching, parallel execution, error recovery all testable
- LangGraph's graph model maps naturally to CI/CD pipelines
- Temporal gives industrial-grade durability (battle-tested at Uber/Netflix scale)
- Each component swappable independently
- Larger community and hiring pool

**Cons:**
- Four systems to operate (LangGraph + Temporal + Mem0 + custom bot)
- Behavior changes require code deploys
- Memory is separate from orchestration — always "fetching context"
- Higher baseline cost
- Over-engineering risk

### GBrain-style (Hermes + skill files + knowledge graph)

**Pros:**
- Behavior lives in markdown skill files — changing behavior = editing a document
- Agent can "skillify" failures into permanent fixes without code deploys
- Memory is the core, not a separate layer — agent operates inside the brain
- Deterministic-first routing = dramatically lower token costs
- Compounding effect: brain gets smarter overnight automatically
- Simpler infra: one Postgres, one git repo, one process

**Cons:**
- Tightly coupled to OpenClaw/Hermes ecosystem (fragmented after community crisis)
- 100+ skills with overlapping triggers become hard to reason about (15% unreachable in real deployments)
- No equivalent to LangGraph's conditional graph execution for complex branching
- Less framework protection for error handling/retries/state management
- Minions handles 80% of background work but isn't Temporal
- TypeScript/Bun stack (language mismatch if team is Go/Python)

### Decision for AKB48: GBrain-style

**Rationale:** Solo operator. No team to manage platform complexity. The compounding brain effect is the killer feature — every interaction makes the next one better. Build a thin Python claw, use GBrain via MCP for the hard memory/knowledge work.

---

## 3. Memory System Landscape

### Mem0 — Most popular, managed API
- 50K+ GitHub stars, framework-agnostic, three-tier memory (user/session/agent)
- Graph memory paywalled at $249/mo Pro tier
- Scores 49% on LongMemEval temporal retrieval
- **For AKB48:** Unnecessary. GBrain replaces this entirely

### Zep / Graphiti — Temporal knowledge graph
- Bi-temporal model with validity windows on edges
- 63.8% on LongMemEval (best temporal reasoning)
- Graph at $25/mo (vs Mem0's $249)
- **For AKB48:** Interesting complement to GBrain if you need temporal queries beyond what GBrain's timeline model provides

### Letta (MemGPT) — OS-inspired memory tiers
- Core/recall/archival memory tiers, LLM manages its own context
- Claims 74% on LoCoMo with just filesystem storage
- **For AKB48:** The "filesystem is all you need" benchmark validates GBrain's markdown-file approach

### GBrain — The chosen path
- PostgreSQL + pgvector + auto-wiring knowledge graph
- Hybrid search: vector + keyword + RRF fusion
- Zero-LLM entity extraction via regex/rules
- Compiled truth + timeline page format
- Dream cycle for nightly consolidation
- 30+ MCP tools out of the box
- **Benchmark (self-reported):** P@5 49.1%, R@5 97.9% on BrainBench corpus

---

## 4. Chat Interface Options

### Custom bot (discord.py / python-telegram-bot / grammY)
- Full control over message handling, threading, permissions
- ~200 lines of adapter code per platform
- **For AKB48:** RECOMMENDED. The chat interface is your UX — own it

### Claude Code Channels
- Anthropic's research preview (March 2026)
- MCP-based, Telegram + Discord support
- Tied to Claude Code session model
- **For AKB48:** Interesting reference architecture but not the foundation

### LangBot
- 13+ messaging platforms, pipeline architecture, MCP support
- **For AKB48:** Evaluate if multi-platform matters early

---

## 5. Durable Execution Options

### Temporal
- Industrial-grade. Workflows survive crashes, run for months
- GA integration with OpenAI Agents SDK (March 2026)
- **For AKB48 Phase 2+:** Add when workflows span multiple services with approval gates

### GBrain Minions
- Postgres-backed job queue. Deterministic work: 753ms, $0 tokens
- Survives gateway restarts, parent-child DAGs
- **For AKB48 Phase 1:** Covers 80% of background work needs

### n8n
- Visual workflow automation, 400+ integrations
- **For AKB48:** Supplementary glue between systems, not for agent orchestration

---

## 6. AKB48 Architecture (Final)

```
Telegram / Discord
      │
      ▼
  ChannelAdapter (normalized interface)
      │
      ▼
  Session Resolver (main / dm / group + permissions)
      │
      ▼
  Context Assembler
      ├── Load AGENTS.md + SOUL.md
      ├── Resolve intent → inject ONE skill (via RESOLVER.md)
      ├── Load session history (compacted)
      ├── Query gbrain for relevant memory
      └── Compose final system prompt
      │
      ▼
  LLM Call (Claude Sonnet 4.6 API, streaming)
      │
      ▼
  Tool Executor (idempotency-checked)
      ├── gbrain MCP tools (search, put, get, graph)
      ├── Shell (with session-scoped permissions)
      ├── GitHub API
      └── Web search
      │
      ▼
  Post-Turn Hook
      ├── Persist session state (JSONL)
      ├── Memory flush (extract facts → gbrain)
      └── Compaction check (if session > N turns)
```

~2,000-2,500 lines of Python. GBrain handles the brain. AKB48 handles everything else.
