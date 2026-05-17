# AKB48 — Document 3: Build Strategy, Implementation Plan & Risks

## For: Solo CTO/CEO building a personal AI agent system
## Architecture: GBrain-style (thin claw + knowledge graph + skill files)
## Date: April 2026

---

## 1. Build Strategy: Three Phases

### Phase 1: Working Prototype (Weeks 1-4)

**Goal:** Chat → agent → brain pipeline that executes one workflow end-to-end.

**Stack:**
- **Claw:** Custom Python adapter (~200 lines per platform)
  - Telegram: `python-telegram-bot` or `grammY` (if TypeScript)
  - Discord: `discord.py`
  - CLI: stdin/stdout (for dev/testing)
- **Brain:** GBrain installed, connected via MCP (`gbrain serve`)
- **LLM:** Claude Sonnet 4.6 via Anthropic API (primary), Haiku 4.5 (extraction)
- **Session:** JSONL files under `~/.akb48/sessions/`
- **Skills:** 3-5 skill files for most common workflows

**Week-by-week plan:**

**Week 1** — Telegram bot that forwards messages to Claude API, returns responses. No skills, no brain. Just a working chat loop. Validates your infra.

**Week 2** — Connect gbrain via MCP. Now your bot can `gbrain query` and `gbrain put`. Every conversation gets stored as a brain page. You already have memory.

**Week 3** — Add the skill resolver. Create 3-5 skill files:
- `skills/research/SKILL.md` — web research with gbrain storage
- `skills/daily-briefing/SKILL.md` — morning prep with calendar/brain context
- `skills/note-capture/SKILL.md` — capture ideas/decisions to brain

The resolver is simple: intent keywords → skill file path → load skill into system prompt → execute.

**Week 4** — Add cron scheduler:
- Daily briefing at 7am
- Weekly review on Sunday evening
- Overnight dream cycle (`gbrain maintain` + enrichment)

**What you skip in Phase 1:** Code sandbox, GitHub integration, deploy agent, complex compaction, multi-agent routing.

**Cost estimate:** $70-250/month (LLM API + VPS)

---

### Phase 2: Production System (Months 2-4)

**Goal:** Full agent suite with durable execution, code tools, and automated learning.

**Add:**
- **Coder + Test skills** with GitHub API integration (PyGithub)
- **Code sandbox** (Docker containers for safe execution)
- **Session compaction** with memory flush to gbrain before compacting
- **Automated fact extraction** — post-turn hook calls Haiku to extract facts → `gbrain put`
- **Idempotency keys** on all side-effecting tool calls
- **Session resolver** — main/dm/group with permission boundaries
- **Minions** (gbrain's Postgres job queue) for background work
- **Model routing** — Sonnet for generation, Haiku for extraction/routing

**Cost estimate:** $200-600/month

---

### Phase 3: Full Ownership (Months 6-12)

**Goal:** Self-contained system. No managed service dependencies except LLM APIs.

**Add:**
- **Deploy skill** with GitHub Actions integration + approval gates
- **Skillify loop** — agent converts failures into permanent skills
- **Custom memory layer** if gbrain's becomes limiting (PostgreSQL + pgvector + optional Neo4j)
- **Evaluation framework** — automated testing of agent outputs against golden datasets
- **Model routing via OpenRouter** — route different tasks to optimal models
- **Monitoring dashboard** — task success rate, time to completion, token costs

**Cost estimate:** $300-1,000/month

---

## 2. Critical Patterns to Adopt from OpenClaw

### MUST ADOPT:

**Session Resolution Model** — Every message maps to a session type with security boundaries. Even as solo operator, you'll eventually expose AKB48 to a group chat. Without session-scoped permissions, one prompt injection can access everything. Implementation: session ID convention + permission lookup table.

**System Prompt Composition** — AGENTS.md (rules) + SOUL.md (personality) + selective skill injection per turn. Never dump all skills into every prompt. Route intent → inject only the relevant skill.

**Session Compaction with Memory Flush** — Always extract facts to gbrain BEFORE compacting conversation history. Without this, compaction destroys knowledge.

**Idempotency Keys** — UUID per tool call, checked before execution. Prevents double-deploys, double-PRs on message retries.

**Channel Adapter Interface** — Normalized interface per platform:
```python
class ChannelAdapter:
    async def authenticate(self)
    async def parse_inbound(self, raw_event) -> Message
    def check_access(self, message) -> bool
    async def send(self, session_id, content, attachments)
```

### CAN SKIP:

- Canvas / A2UI (web UIs in chat — irrelevant for text operator)
- Voice Wake / Talk Mode (add later if desired)
- Multi-Agent Routing (one agent, one brain, multiple channels)
- Device Pairing (you're one person, bot tokens sufficient)
- Docker Sandboxing per Session (add only when exposing to others)
- Plugin Loader / Extension System (you're writing the code, import directly)

---

## 3. AKB48 Component Breakdown

```
~/.akb48/
├── klawmbing.py              # Entry point, starts gateway
├── config.json               # API keys, channel tokens, model config
│
├── adapters/
│   ├── base.py               # ChannelAdapter interface
│   ├── telegram.py           # Telegram adapter (~150 lines)
│   ├── discord.py            # Discord adapter (~150 lines)
│   └── cli.py                # CLI adapter for testing
│
├── core/
│   ├── session.py            # Session resolver + JSONL persistence
│   ├── context.py            # Context assembler (AGENTS.md + SOUL.md + skill injection)
│   ├── executor.py           # LLM caller + tool executor + streaming
│   ├── compactor.py          # Session compaction with memory flush
│   ├── idempotency.py        # UUID-based idempotency for tool calls
│   └── cron.py               # Cron scheduler for recurring skills
│
├── skills/
│   ├── RESOLVER.md           # Intent → skill routing table
│   ├── research/SKILL.md
│   ├── daily-briefing/SKILL.md
│   ├── note-capture/SKILL.md
│   ├── prd/SKILL.md
│   ├── coder/SKILL.md
│   └── deploy/SKILL.md
│
├── identity/
│   ├── AGENTS.md             # Operational rules (what agent can/cannot do)
│   ├── SOUL.md               # Personality and tone
│   └── USER.md               # Who you are (context for personalization)
│
├── sessions/
│   └── <session-id>.jsonl    # Append-only session logs
│
└── logs/
    └── tool-calls.jsonl      # Audit trail of all tool invocations
```

**Total estimated code:** ~2,000-2,500 lines of Python.

---

## 4. Risks & Mitigations

### Over-complexity
**Risk:** Building 5 agents + memory + cron + sandbox before one agent works.
**Mitigation:** Week 1 = one chat loop. Week 2 = add brain. Week 3 = add skills. Every practitioner says the same: start with a single agent.

### Hallucination
**Risk:** Agent generates wrong code, incorrect deploy commands, fabricated research.
**Mitigation:**
- Never auto-merge code (PRs with human review)
- Never auto-deploy to production without explicit approval
- Research agent must cite sources
- Use structured outputs (JSON schemas) to constrain behavior

### Debugging Difficulty
**Risk:** When a multi-step chain fails, which step broke? LLM non-determinism makes reproduction hard.
**Mitigation:**
- Log every LLM call: full prompt, response, tool calls (tool-calls.jsonl)
- Build replay capability: re-run a failed session with same inputs
- Idempotency keys let you safely retry without double-execution

### Ecosystem Fragmentation
**Risk:** OpenClaw community crisis → fragmented into Hermes, ZeroClaw, NanoClaw, IronClaw, etc. Building on a fragmenting ecosystem.
**Mitigation:** AKB48 is YOUR claw. You control the runtime. GBrain is MIT-licensed. If GBrain development stalls, the brain repo + PostgreSQL + pgvector are all standard technology you can maintain yourself.

### Skill Drift
**Risk:** After months of accumulating skills, 15% become unreachable (measured in real OpenClaw deployments). Skills overlap, conflict, or reference stale conventions.
**Mitigation:**
- Use `gbrain check-resolvable` to audit skill tree reachability
- Periodic review: is each skill still triggered correctly?
- Keep skill count manageable (start with 5, grow to 15-20 max)

### Token Cost Explosion
**Risk:** Agentic workflows use 10-100x more tokens than simple chat. Multi-step chains compound costs.
**Mitigation:**
- Set per-task token budgets from day one
- Monitor with alerts (Anthropic console has spending limits)
- Deterministic-first routing: regex/rules before LLM calls
- Use Haiku ($0.25/MTok) for extraction, Sonnet ($3/MTok) for generation only
- Expected: $200-2,000/month per active engineer for agentic systems

### Security
**Risk:** Agent with access to GitHub, deploys, and shell is a significant attack surface.
**Mitigation:**
- Principle of least privilege per session type
- Code execution in Docker sandbox only (never host)
- Audit trail for every tool invocation (JSONL logs)
- No secrets in agent context — use env vars / secrets manager
- Session-scoped permissions (group sessions can't run shell)

### Session Data Loss
**Risk:** JSONL session files grow large, corrupt, or get accidentally deleted.
**Mitigation:**
- Backup sessions directory daily
- Memory flush before compaction preserves critical facts in gbrain
- Cap session file size (compact aggressively after 50+ turns)
- Known Claude Code issue: 3.8GB session files hang the process — implement size guard

---

## 5. Cost Model

| Component | Phase 1 | Phase 2 | Phase 3 |
|-----------|---------|---------|---------|
| Claude Sonnet 4.6 API | $50-200/mo | $150-400/mo | $200-600/mo |
| Claude Haiku 4.5 API | $5-20/mo | $20-50/mo | $30-100/mo |
| VPS (Hetzner CX22 or similar) | $20/mo | $40/mo | $60-100/mo |
| GBrain (self-hosted, MIT) | $0 | $0 | $0 |
| Supabase (if gbrain uses it) | $0 (free tier) | $25/mo | $25/mo |
| Domain + Tailscale | $0-10/mo | $0-10/mo | $0-10/mo |
| **Total** | **$75-250/mo** | **$235-525/mo** | **$315-835/mo** |

---

## 6. What to Avoid

1. **AutoGen / AG2** — Maintenance mode, high token cost, wrong architecture
2. **Fine-tuning** — Exhaust prompt engineering + memory + tools first
3. **Building your own orchestration framework** in Phase 1 — use skill files
4. **Autonomous production deployments** — human approval gate always
5. **Premature multi-agent complexity** — one well-prompted agent with good tools beats a poorly coordinated 5-agent system
6. **Storing everything in memory** — implement forgetting from day one. Stale memories actively harm performance
7. **Treating RAG as memory** — RAG retrieves documents. Memory is structured, evolving, agent-writable knowledge
8. **LangGraph/CrewAI** — Wrong abstraction for a solo operator with skill files
9. **Purpose-built vector databases** (Pinecone, etc.) — PostgreSQL + pgvector is sufficient. Don't add infra you don't need
10. **Over-investing in framework-specific patterns** — Keep core logic (prompts, tools, evaluation) portable
