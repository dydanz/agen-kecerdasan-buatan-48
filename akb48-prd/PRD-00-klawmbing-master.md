# PRD-00: AKB48 — Master Product Requirements Document

**Status:** Draft v1.0
**Author:** Dandi (CTO/CEO, Solo Operator)
**Created:** April 26, 2026
**Last Updated:** April 26, 2026
**Revision History:**

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | 2026-04-26 | Dandi | Initial draft |

---

## 1. Problem Statement

### 1.1 Background

A solo operator building a company without hiring people needs to function simultaneously as CEO, CTO, product manager, engineer, researcher, and operations lead. The cognitive load is unsustainable — context-switching between strategic thinking and execution destroys both quality and throughput.

Current AI tools (ChatGPT, Claude chat, Copilot) are session-bound: every conversation starts from zero. They don't remember your codebase conventions, your meeting history, your investor relationships, or the decision you made last Tuesday about database architecture. They are tools, not teammates.

### 1.2 Core Problem

**There is no personal AI system that compounds knowledge over time, operates autonomously on schedule, and is controllable from a mobile chat interface — while remaining fully self-hosted and owned by the operator.**

Existing solutions fall into two traps:

- **Framework trap:** LangGraph, CrewAI, AutoGen — designed for teams building multi-tenant platforms. Over-engineered for a solo operator. Require significant infrastructure. Behavior changes need code deploys.
- **SaaS trap:** Managed AI assistants (Jasper, Writer, etc.) — vendor-locked, no memory ownership, no autonomous operation, no extensibility.

### 1.3 Impact of Not Solving

Without a compounding personal agent:
- The operator re-explains context in every conversation (estimated 20-40% of interaction time is context setup)
- Research insights are lost after the session ends
- No autonomous daily briefings, meeting prep, or overnight processing
- Scaling the company requires hiring people for tasks that a well-configured agent could handle
- Competitive disadvantage against operators who have this infrastructure

### 1.4 Who Has This Problem

Solo founders, solo CTOs, indie hackers, and technical executives who:
- Make 50+ decisions per day across domains
- Need persistent memory of people, companies, decisions, and technical context
- Want agents that run 24/7 (not just when a chat window is open)
- Require full ownership of their data and agent infrastructure
- Communicate primarily via mobile chat (Telegram, Discord)

---

## 2. Vision

### 2.1 Product Vision

AKB48 is a **thin, self-hosted agent runtime** that connects a mobile chat interface (Telegram/Discord) to a compounding knowledge brain (GBrain). The operator chats naturally; AKB48 routes intent to skill files, executes with tools, stores knowledge, and gets smarter overnight — without code deploys, without managed services, without vendor lock-in.

### 2.2 One-Line Description

**"A personal AI claw you own — chat-controlled, brain-backed, skill-driven."**

### 2.3 Success Criteria (North Star)

The system is successful when:
1. The operator can send a Telegram message at 11pm and wake up to a completed research brief in the morning
2. Every conversation makes the next conversation better (compounding knowledge)
3. Adding new agent capabilities means writing a markdown file, not deploying code
4. The entire system runs on a single $20/month VPS with no external dependencies except LLM APIs
5. The operator trusts the system enough to let it run autonomously overnight

---

## 3. Actors & Personas

### 3.1 Primary Actor: The Operator

**Persona:** Dandi — Solo CTO/CEO building a company without employees.

**Characteristics:**
- Backend engineering background (Go, Python)
- Makes strategic and technical decisions daily
- Communicates via Telegram (primary) and Discord (secondary)
- Works across time zones — needs async agent execution
- Values ownership, simplicity, and systems that compound

**Goals:**
- Reduce cognitive load by delegating formulaic work to agents
- Build persistent memory of people, decisions, and technical context
- Have agents run autonomously (daily briefings, overnight research, meeting prep)
- Gradually extend capabilities via skill files without rebuilding infrastructure

**Frustrations:**
- Re-explaining context in every AI conversation
- Session-bound tools that forget everything
- Over-engineered frameworks that require a team to operate
- Vendor lock-in on data and capabilities

### 3.2 Secondary Actor: The Agent (AKB48 itself)

**Characteristics:**
- Receives messages from chat platforms
- Routes intent to skill files
- Executes tools (gbrain, GitHub, web search, shell)
- Persists session state and extracts knowledge
- Runs scheduled tasks autonomously

### 3.3 Tertiary Actor: External Contacts (future)

**Characteristics:**
- People who message the operator's bot in a Discord server or Telegram group
- Sandboxed: limited permissions, no access to operator's full brain
- Read-only or restricted tool access

**Note:** Tertiary actor support is a Phase 2+ concern. Phase 1 focuses exclusively on the Primary Actor.

---

## 4. User Stories

### 4.1 Milestone 0 — "Hello World" (This PRD's scope)

| ID | As a... | I want to... | So that... | Priority |
|----|---------|-------------|------------|----------|
| US-001 | Operator | Send a message via Telegram and receive an AI response | I can verify the end-to-end chat → LLM → chat loop works | P0 |
| US-002 | Operator | Send a message via CLI and receive an AI response | I can develop and test without needing Telegram | P0 |
| US-003 | Operator | Have AKB48 load its identity from AGENTS.md and SOUL.md | Responses reflect my configured personality and rules | P0 |
| US-004 | Operator | Have AKB48 connect to GBrain via MCP | The agent can query and store knowledge | P0 |
| US-005 | Operator | Ask "What do you know about X?" and get a gbrain-backed answer | Memory retrieval works end-to-end | P0 |
| US-006 | Operator | Tell the agent "Remember that our database uses port 5433" and have it persist | Memory storage works end-to-end | P0 |
| US-007 | Operator | Have session history persist across restarts | I don't lose context when the process crashes | P1 |
| US-008 | Operator | See which skill file was invoked for a given message | I can debug routing issues | P1 |
| US-009 | Operator | Have AKB48 stream responses token-by-token in Telegram | The UX feels responsive, not blocked | P1 |

### 4.2 Post-Hello World (covered by sub-PRDs, not implemented in Milestone 0)

| ID | Story | Phase |
|----|-------|-------|
| US-010 | Run a daily briefing at 7am via cron | Phase 1, Week 4 |
| US-011 | Ask AKB48 to research a topic and store findings in gbrain | Phase 1, Week 3 |
| US-012 | Have sessions compact after N turns with memory flush | Phase 2 |
| US-013 | Open a GitHub PR via chat command | Phase 2 |
| US-014 | Run tests on a PR via chat command | Phase 2 |
| US-015 | Deploy to staging with approval gate | Phase 3 |

---

## 5. Solution Space

### 5.1 Architecture Overview

AKB48 follows the **"thin harness, fat skills"** pattern:

```
┌─────────────────────────────────────────────────────────┐
│                    CHAT INTERFACE                        │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐        │
│  │  Telegram   │  │  Discord   │  │    CLI     │        │
│  │  Adapter    │  │  Adapter   │  │  Adapter   │        │
│  └─────┬──────┘  └─────┬──────┘  └─────┬──────┘        │
│        └───────────────┼───────────────┘                │
│                        ▼                                │
│              ┌─────────────────┐                        │
│              │ Message Router  │                        │
│              │ & Session Mgr   │                        │
│              └────────┬────────┘                        │
└───────────────────────┼─────────────────────────────────┘
                        │
┌───────────────────────┼─────────────────────────────────┐
│                  CORE RUNTIME                           │
│                        ▼                                │
│              ┌─────────────────┐                        │
│              │    Context      │                        │
│              │   Assembler     │                        │
│              │                 │                        │
│              │ AGENTS.md       │                        │
│              │ + SOUL.md       │                        │
│              │ + skill (1)     │                        │
│              │ + session ctx   │                        │
│              │ + gbrain memory │                        │
│              └────────┬────────┘                        │
│                       ▼                                 │
│              ┌─────────────────┐                        │
│              │   LLM Caller    │                        │
│              │  (Claude API)   │                        │
│              │   + streaming   │                        │
│              └────────┬────────┘                        │
│                       ▼                                 │
│              ┌─────────────────┐                        │
│              │  Tool Executor  │                        │
│              │  (MCP client)   │                        │
│              └────────┬────────┘                        │
│                       ▼                                 │
│              ┌─────────────────┐                        │
│              │  Post-Turn Hook │                        │
│              │  (persist, log) │                        │
│              └─────────────────┘                        │
└─────────────────────────────────────────────────────────┘
                        │
┌───────────────────────┼─────────────────────────────────┐
│                   BRAIN LAYER                           │
│                        ▼                                │
│              ┌─────────────────┐                        │
│              │     GBrain      │                        │
│              │   (via MCP)     │                        │
│              │                 │                        │
│              │ search, put,    │                        │
│              │ get, query,     │                        │
│              │ graph, enrich   │                        │
│              └─────────────────┘                        │
│              ┌─────────────────┐                        │
│              │   PostgreSQL    │                        │
│              │   + pgvector    │                        │
│              └─────────────────┘                        │
└─────────────────────────────────────────────────────────┘
```

### 5.2 Key Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Runtime language | Python | Operator's familiarity, ecosystem depth, fast iteration |
| Brain | GBrain via MCP | MIT-licensed, auto-wiring graph, dream cycle, 30+ tools |
| Primary LLM | Claude Sonnet 4.6 | Best cost/quality for code + reasoning tasks |
| Extraction LLM | Claude Haiku 4.5 | 12x cheaper, sufficient for fact extraction |
| Chat primary | Telegram | Mobile-first, rich bot API, operator preference |
| Chat secondary | Discord | Server-based, good for workspace channels (Phase 2) |
| Session storage | JSONL files | Simple, append-only, human-readable, crash-recoverable |
| Skill format | Agent Skills standard (SKILL.md) | Cross-platform, adopted by Claude/Codex/Copilot/Cursor |
| Tool protocol | MCP (Model Context Protocol) | Industry standard, language-agnostic, GBrain native |
| Deployment | VPS + Docker + Tailscale | $20/mo, zero public ports, secure remote access |

### 5.3 Non-Goals (Explicitly Out of Scope)

- Multi-tenant / multi-user platform
- Web UI / dashboard (chat is the interface)
- Voice input/output
- Fine-tuning models
- Running local LLMs (all inference via API)
- Mobile app (Telegram IS the mobile app)
- Plugin marketplace / extension ecosystem
- Canvas / rich rendering in chat (plain text + files)

---

## 6. Milestone Breakdown

This master PRD decomposes into five sub-PRDs, each building toward "hello world":

| PRD | Title | Scope | Depends On |
|-----|-------|-------|------------|
| PRD-01 | Core Runtime & Message Loop | Entry point, config, LLM caller, response streaming | — |
| PRD-02 | Channel Adapters | CLI adapter, Telegram adapter, normalized interface | PRD-01 |
| PRD-03 | GBrain Integration | MCP client, gbrain connection, query/put tools | PRD-01 |
| PRD-04 | Identity & Skill System | AGENTS.md, SOUL.md, skill resolver, context assembly | PRD-01 |
| PRD-05 | Session Management | Session persistence, session resolver, post-turn hooks | PRD-01, PRD-02 |

### "Hello World" Definition of Done

All five sub-PRDs are complete when:

1. Operator starts AKB48 with `python klawmbing.py`
2. GBrain MCP server is running (`gbrain serve`)
3. Operator sends "Hello, who are you?" via Telegram
4. AKB48 responds with a personality-consistent answer (from SOUL.md)
5. Operator sends "Remember that our staging cluster is ap-southeast-1"
6. AKB48 stores this in gbrain and confirms
7. Operator sends "What do you know about our staging cluster?"
8. AKB48 retrieves from gbrain and responds with the stored fact
9. Operator restarts AKB48
10. Previous session context is available (session persistence works)

---

## 7. Technical Constraints

### 7.1 Infrastructure

- Single VPS: 2 vCPU, 4GB RAM, 40GB SSD (Hetzner CX22 or equivalent)
- Docker Compose for GBrain + PostgreSQL
- Tailscale for secure remote access (zero public ports)
- Anthropic API key with spending limits configured

### 7.2 Dependencies

| Dependency | Version | Purpose |
|------------|---------|---------|
| Python | 3.11+ | Runtime |
| python-telegram-bot | 21.x | Telegram adapter |
| anthropic | latest | Claude API client |
| gbrain | latest | Knowledge brain (via MCP) |
| PostgreSQL | 16+ | GBrain storage backend |
| Docker + Docker Compose | latest | Container orchestration |
| Tailscale | latest | Secure networking |

### 7.3 Security Requirements

- No secrets in agent context (env vars / config file with restricted permissions)
- Telegram bot token stored in config, not hardcoded
- All tool calls logged with full audit trail
- GBrain accessible only via localhost / Tailscale
- No public ports exposed

---

## 8. Success Metrics

### 8.1 Hello World Metrics

| Metric | Target | How to Measure |
|--------|--------|----------------|
| End-to-end latency (message → first token) | < 3 seconds | Timestamp logging |
| Memory roundtrip (store → retrieve) | 100% accuracy | Manual test: store 5 facts, retrieve all 5 |
| Session persistence | Survives restart | Kill process, restart, verify last session loads |
| Uptime after deploy | 24h continuous | Process stays alive for 24h without crash |

### 8.2 Phase 1 Metrics (post-hello-world)

| Metric | Target |
|--------|--------|
| Daily active usage | Operator uses AKB48 5+ times/day |
| Skill count | 5-8 active skills |
| GBrain pages | 100+ pages after 2 weeks |
| Token cost | < $200/month |
| Cron jobs running | 3+ (daily briefing, weekly review, dream cycle) |

---

## 9. Open Questions & TODOs

| ID | Question | Impact | Status |
|----|----------|--------|--------|
| TODO-001 | GBrain MCP transport: stdio or HTTP SSE? Stdio is simpler but requires co-located processes. HTTP SSE allows remote gbrain. | Architecture | **Decide before PRD-03 implementation.** Default: stdio (simpler, co-located on same VPS) |
| TODO-002 | Telegram streaming: use `editMessageText` polling or Bot API 9.5 `sendMessageDraft`? Former is proven, latter is newer with less documentation. | UX | **Decide before PRD-02 implementation.** Default: `editMessageText` (proven pattern) |
| TODO-003 | Skill resolver: keyword-based lookup table or lightweight classifier? Keyword is simpler but brittle with overlapping skills. | Architecture | **Decide before PRD-04 implementation.** Default: keyword table (start simple, migrate to classifier when 10+ skills) |
| TODO-004 | Session compaction: what turn count triggers compaction? Too low = frequent summarization overhead. Too high = context window pressure. | Performance | **Decide during Phase 2.** Default: N=30 turns |
| TODO-005 | Which GBrain version to target? v0.12+ has auto-wiring graph. Earlier versions have simpler setup. | Dependency | **Resolve before implementation.** Target latest stable |
| TODO-006 | Python async framework: asyncio native or trio? python-telegram-bot uses asyncio. Need consistency across adapters. | Architecture | **Decide before PRD-01 implementation.** Default: asyncio (matches python-telegram-bot) |
| TODO-007 | Config format: JSON, TOML, or YAML? Need to store API keys, channel tokens, model configs, skill paths. | Developer experience | Default: TOML (human-readable, typed, Python stdlib support via tomllib) |
| TODO-008 | Discord adapter: discord.py or nextcord? discord.py is maintained again but had a hiatus. nextcord is the community fork. | Dependency | **Decide before Discord adapter implementation.** Default: discord.py (original, maintained) |
| TODO-009 | How to handle Telegram rate limits (30 msg/sec global, 1 msg/sec per chat for edits)? Need throttling for streaming responses. | Reliability | **Resolve during PRD-02 implementation.** Default: 1 edit per second for streaming |
| TODO-010 | GBrain brain directory location: inside `~/.akb48/` or separate `~/brain/`? Affects backup strategy. | Operations | Default: `~/brain/` (independent lifecycle from claw runtime) |
| TODO-011 | Operator identity: should USER.md be a gbrain page or a local file? If gbrain page, it's queryable by agents. If local, it's simpler but not searchable. | Architecture | Default: local file (simpler, loaded at startup, not agent-modifiable) |
| TODO-012 | Error presentation in chat: verbose (full stack trace) or concise (one-line + log reference)? | UX | Default: concise in chat + full details in log file |

---

## 10. Appendix: Glossary

| Term | Definition |
|------|-----------|
| **Claw** | A thin agent runtime that receives messages, routes to skills, calls LLMs, executes tools. AKB48 is our claw. |
| **Brain** | A knowledge storage and retrieval system. GBrain is our brain. |
| **Skill file** | A markdown file (SKILL.md) that encodes agent behavior for a specific capability. Injected into the system prompt when relevant. |
| **Skill resolver** | The component that maps user intent to the correct skill file. |
| **MCP** | Model Context Protocol — an open standard for connecting AI agents to external tools and data sources. |
| **Compiled truth** | The top section of a brain page — current best understanding, rewritten when evidence changes. |
| **Timeline** | The bottom section of a brain page — append-only evidence trail, never edited. |
| **Dream cycle** | A nightly autonomous process that consolidates, enriches, and prunes brain knowledge. |
| **Minions** | GBrain's Postgres-backed job queue for deterministic (non-LLM) background work. |
| **Session** | A conversation thread with persistent state. Stored as JSONL files. |
| **Compaction** | Summarizing older turns in a session to fit within context window limits. |
| **Memory flush** | Extracting structured facts from a session into the brain before compaction. |
| **Idempotency key** | A UUID attached to each side-effecting tool call to prevent double-execution on retries. |
| **Channel adapter** | A normalized interface that translates platform-specific events (Telegram, Discord) into a common message format. |
| **Post-turn hook** | Processing that runs after each agent response: persist session, extract facts, check compaction. |

---

## 11. Related Documents

| Document | Purpose |
|----------|---------|
| PRD-01: Core Runtime & Message Loop | Entry point, config, LLM caller |
| PRD-02: Channel Adapters | CLI + Telegram + normalized interface |
| PRD-03: GBrain Integration | MCP client, knowledge query/store |
| PRD-04: Identity & Skill System | AGENTS.md, SOUL.md, resolver |
| PRD-05: Session Management | Persistence, session resolver, post-turn hooks |
| 01-klawmbing-platform-landscape.md | Platform research and architecture decision |
| 02-klawmbing-capabilities-memory.md | Agent capabilities and memory strategy |
| 03-klawmbing-build-strategy-risks.md | Build phases, risks, cost model |
| 05-klawmbing-reference-library.md | 100+ curated reference links |
