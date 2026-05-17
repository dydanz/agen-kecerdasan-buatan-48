# AKB48 — Document 2: Agent Capabilities, Memory & Learning Strategy

## For: Solo CTO/CEO building a personal AI agent system
## Architecture: GBrain-style (thin claw + knowledge graph + skill files)
## Date: April 2026

---

## 1. Agent Capabilities Design

### 1.1 PRD Agent

**Purpose:** Generate and iterate on product requirement documents.

**Tools:** Web search, file system, gbrain memory retrieval, template engine (stored as procedural memory in a skill file)

**Flow:**
1. User: "Write a PRD for a notification service"
2. Agent retrieves: past PRD templates from gbrain, company tech standards, existing docs
3. Agent searches web for: notification service patterns, competing approaches
4. Agent generates PRD with sections: Problem, Goals, Non-goals, Technical Approach, Milestones, Success Metrics
5. Returns draft to chat with inline review options
6. User provides feedback → agent iterates
7. Final PRD committed to project repo + key decisions stored in gbrain

**Key insight:** The PRD skill file should contain your personal template preferences (learned from past corrections). After each PRD approval, extract what the user changed and update the skill's template understanding.

### 1.2 Coder Agent

**Purpose:** Generate, modify, and refactor code.

**Tools:** GitHub API (PyGithub), code sandbox (Docker), file system, gbrain (project architecture, conventions)

**Flow:**
1. User: "Add rate limiting to /api/orders"
2. Agent retrieves from gbrain: project structure, existing middleware patterns, coding conventions
3. Reads relevant files via GitHub API
4. Generates code changes
5. Runs in sandbox to verify compilation
6. Opens PR with changes, links to original request
7. Reports back to chat with PR link

**Hard rule:** Never push to main. Always PRs with human review.
**Hard rule:** Read existing patterns before generating code. The #1 complaint about AI code is ignoring existing conventions.

### 1.3 Test Agent

**Tools:** Test runner (pytest/jest/go test), code sandbox, coverage reporter, GitHub API

**Flow:** Triggered after Coder Agent completes a PR → runs existing tests + generates new tests for changed code → reports pass/fail to chat

### 1.4 Deploy Agent

**Tools:** CI/CD trigger (GitHub Actions API), K8s/Docker API, health check endpoints, rollback mechanisms

**Flow:** Triggered after Test Agent passes → fires CI/CD pipeline → monitors health → rolls back if checks fail → confirms in chat

**Hard rule:** Always require explicit human approval via chat (button click / confirmation message) before production deploys. No autonomous production deployments until months of trust.

### 1.5 Research Agent

**Tools:** Web search (multiple queries), web fetch, file system, gbrain

**Flow:**
1. User: "Research Go vs Rust for our CLI tool"
2. Agent plans: 3-5 queries per dimension (performance, ecosystem, hiring)
3. Executes searches, fetches full articles
4. Synthesizes structured comparison
5. Stores key findings in gbrain for future reference
6. Returns concise report to chat

**Key insight:** Check gbrain FIRST. If this topic was researched before, start from there and search only for updates.

### 1.6 Task Delegation Model

```
User request → Coordinator (intent resolver, not a worker)
     │
     ├── "Write a PRD for X"           → PRD skill
     ├── "Add feature X"               → Coder skill → Test skill
     ├── "Deploy to staging"           → Deploy skill (approval gate)
     ├── "Research X vs Y"             → Research skill
     ├── "Build feature X end-to-end"  → PRD → Coder → Test → Deploy (chained)
     └── "What did we decide about X"  → gbrain query (no skill needed)
```

For multi-step workflows, the Coordinator chains skills sequentially. Each handoff is a checkpoint. If any step fails, report failure point + options to user.

---

## 2. Memory Architecture: Three Tiers

### Hot Memory (context window)
What the agent sees right now. Last N messages, current task context, retrieved warm memories. This IS the prompt. Keep it lean — every unnecessary token costs money and dilutes attention.

### Warm Memory (structured knowledge store → GBrain)
Extracted facts, user preferences, project state, agent skills. This is what makes the agent "remember."

GBrain stores things like:
- "The project uses Go 1.22 with Chi router"
- "User prefers concise PRDs, max 2 pages"
- "Staging deploys go to k8s cluster staging-ap-southeast-1"
- "Last deploy failed because of missing env var DATABASE_URL"

Organized as:
- **People pages** — everyone you interact with, auto-enriched
- **Company pages** — every company mentioned, with timeline
- **Concept pages** — decisions, patterns, technical choices
- **Meeting pages** — transcripts with attendee cross-references

Each page follows **compiled truth + timeline** format:
```markdown
---
type: person
title: Jordan Lee
tags: [investor, board-member, series-a]
---

Jordan Lee is a board member and Series A lead investor at Acme Ventures.
Key context: prefers data-driven updates, monthly cadence, focus on ARR.

---

- 2026-01-15: Board meeting — discussed Q4 results, approved hiring plan
- 2026-02-20: 1:1 call — flagged concern about burn rate
- 2026-03-10: Email — requested updated cap table
```

Above the `---`: **compiled truth** (current best understanding, rewritten when evidence changes).
Below: **timeline** (append-only evidence trail, never edited).

### Cold Memory (full archive → PostgreSQL)
Complete conversation logs, full PRD versions, all code diffs, raw research outputs. Stored in PostgreSQL. Rarely retrieved directly — warm memory is distilled from cold memory via background processes.

---

## 3. The Learning Loop

### After every completed task:

**1. Fact extraction** — A background process (Haiku, cheap) scans the conversation and extracts structured facts:
- "The orders API now has rate limiting at 100 req/min"
- "User rejected first PRD draft because it lacked success metrics"

**2. Pattern detection** — Over time, the system identifies recurring patterns:
- "User always asks for success metrics in PRDs" → update PRD skill template
- "Deploys fail 30% due to env var issues" → add pre-deploy env var check to deploy skill

**3. Skill formation** — Successful multi-step patterns become stored procedures:
- "New API endpoint flow: check router patterns → generate handler → add tests → add to OpenAPI spec → open PR"

**4. Feedback integration** — When user corrects output, capture as negative example:
- "Don't use sync.Mutex here, use channels" → stored as project-specific convention in gbrain

### The GBrain Dream Cycle (nightly)

Runs autonomously while you sleep:
1. **Collect** — Scan all conversations from the day
2. **Consolidate** — Extract entities, update person/company pages, fix citations
3. **Evaluate** — Score memories by importance (relevance × frequency × recency)
4. **Prune** — Archive or forget low-value memories

The brain gets smarter every night without explicit engineering.

### Deterministic-First Routing (cost reduction)

GBrain's critical pattern: don't use LLMs for what regex and rules can do.

- **Entity extraction:** Regex patterns for person names, company names, dates → zero LLM calls
- **Link typing:** Pattern matching ("CEO of X" → works_at, "invested in" → invested_in)
- **Intent classification:** Keyword/regex first, LLM fallback only when ambiguous

The system tracks classifier accuracy over time:
```
gbrain doctor
→ intent classifier: 87% deterministic, up from 40% in week 1
```

Every LLM fallback is logged. Periodic review generates better regex patterns from the failures.

**Routing rule:**
- Same input → same steps → same output = **Minions** (Postgres job, $0 tokens)
- Input requires judgment/assessment = **LLM subagent**

---

## 4. Memory Approach Comparison

| Approach | When to Use | Limitations | AKB48 Role |
|----------|-------------|-------------|----------------|
| **RAG (vector search)** | Starting point. Semantic similarity. | Read-only. No learning. Retrieves by similarity, not relevance-over-time. | GBrain's search pipeline uses this as ONE signal (combined with keyword + graph) |
| **Knowledge graph** | Tracking relationships between entities. "Who works at X?" "What did Y invest in?" | Requires extraction pipeline. | GBrain auto-wires this with zero LLM calls |
| **Compiled truth + timeline** | Maintaining evolving understanding of people, companies, concepts. | Requires consolidation cycles. | GBrain's core page format |
| **Procedural memory (skills)** | Encoding repeatable workflows as reusable instructions. | Skills can become stale or conflict. | Skill files + the "skillify" loop |
| **Feedback loops** | Agent corrections become training data. | Requires capturing and applying corrections. | Post-turn hooks extract corrections → gbrain |
| **Fine-tuning** | Last resort when prompt engineering + memory insufficient. | Expensive, slow, risk of catastrophic forgetting. | AVOID until Phase 3 at earliest |

---

## 5. Context Compaction Strategy

Sessions grow. Context windows don't. AKB48 needs compaction:

**Before compacting — Memory Flush:**
1. Run cheap model (Haiku) to extract structured facts from oldest turns
2. Write facts to gbrain via `gbrain put`
3. The brain preserves what the session forgets

**Then compact:**
1. Summarize oldest half of conversation into a paragraph
2. Keep only: summary + recent N turns + system prompt
3. Persist compacted session to disk (JSONL)

**Compaction trigger:** After every N turns (start with N=30, tune based on your usage)

**Critical:** Always flush to memory BEFORE compacting. Without this step, compaction destroys knowledge — summaries lose details like "the database port changed to 5433."

---

## 6. Session Management

### Session types (adopted from OpenClaw):
- `main` — you, full access, no sandbox
- `dm:<channel>:<id>` — someone messaging your bot, sandboxed
- `group:<channel>:<id>` — group chat, sandboxed, mention-gated

### Session persistence:
- JSONL files under `~/.akb48/sessions/`
- Each session = append-only event log
- Crash recovery: reload last session file, resume from last turn

### Idempotency:
- Every side-effecting tool call gets a UUID
- Before execution, check if UUID already completed
- Prevents double-deploys, double-PRs on retries
