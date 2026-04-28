# AI Agent Platform Blueprint: Chat-Controlled Autonomous Agents

## For: Engineering Manager building a chat-driven agent system with long-term ownership trajectory

---

## 1. Platform Landscape

### 1.1 Agent Frameworks

**LangGraph (LangChain)** — Graph-based workflow orchestration

LangGraph models agent interactions as nodes in a directed graph with shared state. It is the most production-ready framework in 2026, offering built-in checkpointing, time-travel debugging via LangSmith, per-node token streaming, and conditional branching. It is model-agnostic and has the largest community.

- *Does well:* Fine-grained control over execution flow, stateful workflows, human-in-the-loop, best observability (LangSmith), deepest MCP integration (tools become first-class graph nodes).
- *Limitations:* Steepest learning curve. State schemas must be well-defined upfront, which gets messy in complex agent networks. Memory handling through LangChain's modules has historically been tricky. Over-abstraction is a real complaint.
- *Suitability:* **High.** Your system needs conditional routing (code → test → deploy), human approval gates, and long-running stateful workflows. LangGraph is purpose-built for this.

**CrewAI** — Role-based team orchestration

CrewAI models agents as "crew members" with roles, backstories, and goals. You assemble a crew and assign tasks. It has the lowest barrier to entry of any framework.

- *Does well:* Fastest prototyping. Intuitive mental model (Agent = role, Task = job, Crew = team). Built-in memory concept. Growing A2A protocol support.
- *Limitations:* No built-in checkpointing for long-running workflows. Limited control over agent-to-agent communication (mediated through task outputs, not direct messaging). Coarse-grained error handling. Debugging is painful — logging inside Tasks is broken. Teams that start with CrewAI for prototyping often migrate to LangGraph for production.
- *Suitability:* **Medium.** Good for quick prototyping of the PRD-writing or research agents, but you will likely hit walls when adding deployment pipelines and multi-step CI/CD flows.

**Microsoft AutoGen / AG2** — Conversational agent teams

AutoGen models collaboration as multi-turn conversations between agents. The v0.4 rewrite (AG2) added an event-driven core and async execution. GroupChat is its primary coordination pattern.

- *Does well:* Most diverse conversation patterns (debate, consensus, sequential dialogue). Strong code execution support. Good for quality-sensitive workflows where thoroughness > speed.
- *Limitations:* Expensive. Every agent turn in a GroupChat is a full LLM call with accumulated history. A 4-agent debate with 5 rounds = 20+ LLM calls minimum. Microsoft has shifted AutoGen to maintenance mode in favor of the broader Microsoft Agent Framework. Community is smallest of the three.
- *Suitability:* **Low.** The token cost and latency make it impractical for the kind of frequent, chat-triggered workflows you're building. Maintenance-mode status is a red flag.

**Verdict:** Start with **LangGraph** for orchestration. Use CrewAI only if you need a quick prototype to validate an agent design before building it properly.

### 1.2 Workflow Orchestration (Infrastructure Layer)

**Temporal** — Durable execution engine

Temporal is not an AI framework — it is distributed systems infrastructure. It guarantees that workflows run to completion despite crashes, network failures, and API timeouts. Workflows can run for days, months, or years without losing state.

- *Does well:* Durable execution (survives crashes, replays from exact failure point). Built-in retries, task queues, signals, timers. Excellent observability UI. Supports human-in-the-loop via Signals. Language-agnostic (Python, Go, TypeScript, Java). Already has an official GA integration with the OpenAI Agents SDK as of March 2026, and patterns for LangGraph.
- *Limitations:* Workflow code must be deterministic (Activities handle the non-deterministic LLM calls). Adds infrastructure complexity — you need to run Temporal Server (or use Temporal Cloud). Learning curve for the Workflow/Activity separation model.
- *Suitability:* **Critical for Phase 2+.** When your agents start running real deployments and multi-step CI/CD, you need durable execution. A crashed LangGraph process means restarting from scratch. A crashed Temporal workflow resumes exactly where it left off. This is the difference between a demo and a production system.

**n8n** — Visual workflow automation

- *Does well:* 400+ integrations. Visual drag-and-drop workflow builder. Good for non-code automations (Slack → GitHub → deploy).
- *Limitations:* Not designed for complex agent reasoning loops. Limited state management. Not a replacement for LangGraph or Temporal for agent orchestration.
- *Suitability:* **Supplementary.** Useful for the glue between systems (e.g., "when a PR is merged, trigger the deploy agent"), not for the agents themselves.

### 1.3 Chat Interface Layer

**Custom Bot (discord.py / python-telegram-bot)** — Build your own

- *Does well:* Full control over message handling, threading, permissions, UI (buttons, modals, reactions). No vendor lock-in. Both libraries are mature and well-documented.
- *Limitations:* You build everything: command routing, session management, error presentation, rate limiting. Moderate effort (1-2 weeks for a solid foundation).
- *Suitability:* **Recommended.** The chat interface is your UX. You want full control over how tasks are dispatched, how progress is reported, and how errors are surfaced. A 200-line bot adapter is simpler than adopting a framework that adds its own opinions.

**LangBot** — Open-source multi-platform bot framework

- *Does well:* Supports 13+ messaging platforms (Discord, Telegram, Slack, WhatsApp, LINE, WeChat). Pipeline architecture with plugin isolation. Built-in MCP support and vector DB options (Chroma, Qdrant, Milvus, pgvector).
- *Limitations:* Another layer of abstraction between you and the platform APIs. Opinionated about how agents connect.
- *Suitability:* **Worth evaluating** if multi-platform support matters early. Otherwise, a custom adapter is simpler.

**Claude Code Channels** — Anthropic's research preview (March 2026)

- *Does well:* Native Claude Code integration. Telegram and Discord support out of the box. MCP-based architecture. Runs locally — code never leaves your machine.
- *Limitations:* Research preview — not production-ready. Tied to Claude Code's session model. Not designed for multi-agent orchestration.
- *Suitability:* **Niche.** Interesting for personal dev workflows, but not the foundation for a multi-agent system.

### 1.4 Memory & Knowledge Systems

**Mem0** — Managed memory API (most popular)

- *Architecture:* Three-tier memory: user, session, agent scopes. Hybrid storage combining vectors, graph relationships, and key-value lookups. Cloud-first with open-source self-hosted option.
- *Does well:* Largest ecosystem (50K+ GitHub stars). Integrations with LangGraph, CrewAI, OpenAI, Vercel AI SDK. Memory compression engine claims 80% token reduction. SOC 2 compliant.
- *Limitations:* Graph memory (the real power) is paywalled at $249/month Pro tier. Scores only 49% on LongMemEval temporal retrieval — significantly below competitors. Open-source version is limited.
- *Suitability:* **Good starting point.** Free tier gets you running fast. Evaluate whether you hit the graph paywall before committing long-term.

**Zep / Graphiti** — Temporal knowledge graph

- *Architecture:* Knowledge graph with `valid_at` / `invalid_at` timestamps. Tracks how facts change over time, not just what they are now.
- *Does well:* Best temporal reasoning (63.8% on LongMemEval vs Mem0's 49%). Graph memory at every tier starting at $25/month. Ideal for tracking evolving project states ("the deploy target was staging last week, production now").
- *Limitations:* Self-hosting requires Neo4j infrastructure. Uses 340x more memory per conversation than Mem0 for marginal gains on most non-temporal benchmarks. More complex setup.
- *Suitability:* **Strong contender for Phase 2.** Your agents will need to reason about changing project state, deployment histories, and evolving requirements — temporal reasoning matters.

**LangMem** — LangGraph-native memory

- *Architecture:* Built directly into LangGraph Store. Memory operations happen through explicit tool calls. Agents manage their own memory.
- *Does well:* Zero new services to deploy if you're on LangGraph. Free under MIT license. Procedural memory — agents can rewrite their own system prompts based on learned patterns.
- *Limitations:* LangGraph-locked. 59.82s p95 search latency — never use synchronously. Documentation is thin.
- *Suitability:* **Use this** if you go with LangGraph for orchestration. It is the natural fit. Just run memory operations async.

**PostgreSQL + pgvector** — Self-hosted vector search

- *Does well:* You already know Postgres. pgvector adds vector similarity search to a database you're already operating. ACID-compliant. No new infrastructure.
- *Limitations:* No built-in memory compression, entity extraction, or knowledge graph. You build the retrieval pipeline yourself.
- *Suitability:* **Phase 3 foundation.** When you replace managed services with your own stack, Postgres + pgvector is the storage backbone. Combine with a custom extraction pipeline.

---

## 2. System Architecture

### 2.1 Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│                        CHAT INTERFACE LAYER                        │
│                                                                     │
│   ┌──────────┐  ┌──────────┐  ┌──────────┐                        │
│   │ Discord  │  │ Telegram │  │   CLI    │                        │
│   │   Bot    │  │   Bot    │  │ (dev)    │                        │
│   └────┬─────┘  └────┬─────┘  └────┬─────┘                        │
│        │              │              │                              │
│        └──────────────┴──────────────┘                              │
│                       │                                             │
│              ┌────────▼────────┐                                    │
│              │  Message Router │ ← Parses intent, routes to agent  │
│              │  & Session Mgr  │ ← Tracks user sessions, auth     │
│              └────────┬────────┘                                    │
└───────────────────────┼─────────────────────────────────────────────┘
                        │
┌───────────────────────┼─────────────────────────────────────────────┐
│                  ORCHESTRATION LAYER                                │
│                       │                                             │
│              ┌────────▼────────┐                                    │
│              │   Coordinator   │ ← LangGraph graph / Temporal WF   │
│              │     Agent       │ ← Decomposes tasks, delegates     │
│              └───┬────┬────┬──┘                                    │
│                  │    │    │                                        │
│       ┌──────────┘    │    └──────────┐                             │
│       ▼               ▼               ▼                             │
│  ┌─────────┐   ┌──────────┐   ┌──────────┐                        │
│  │  PRD    │   │  Coder   │   │ Research │                        │
│  │  Agent  │   │  Agent   │   │  Agent   │                        │
│  └────┬────┘   └────┬─────┘   └────┬─────┘                        │
│       │              │              │                              │
│       │         ┌────┴─────┐        │                              │
│       │         ▼          ▼        │                              │
│       │    ┌────────┐ ┌────────┐    │                              │
│       │    │  Test  │ │ Deploy │    │                              │
│       │    │ Agent  │ │ Agent  │    │                              │
│       │    └────────┘ └────────┘    │                              │
└───────┼──────────┼──────────┼───────┼───────────────────────────────┘
        │          │          │       │
┌───────┼──────────┼──────────┼───────┼───────────────────────────────┐
│                      TOOLING LAYER                                  │
│                                                                     │
│  ┌─────────┐ ┌──────────┐ ┌────────┐ ┌──────────┐ ┌────────────┐  │
│  │ GitHub  │ │ Docker / │ │ Web    │ │ File     │ │ CI/CD      │  │
│  │   API   │ │ K8s API  │ │ Search │ │ System   │ │ (GH Act.)  │  │
│  └─────────┘ └──────────┘ └────────┘ └──────────┘ └────────────┘  │
│                                                                     │
│  ┌─────────────────┐  ┌──────────────┐  ┌───────────────────────┐  │
│  │ LLM APIs        │  │ Code Sandbox │  │ MCP Servers           │  │
│  │ (Claude, GPT,   │  │ (isolated    │  │ (extensible tool      │  │
│  │  local models)  │  │  execution)  │  │  protocol)            │  │
│  └─────────────────┘  └──────────────┘  └───────────────────────┘  │
└─────────────────────────────────────────────────────────────────────┘
        │          │          │       │
┌───────┼──────────┼──────────┼───────┼───────────────────────────────┐
│                      MEMORY LAYER                                   │
│                                                                     │
│  ┌──────────────────┐  ┌───────────────────┐  ┌──────────────────┐ │
│  │   HOT MEMORY     │  │   WARM MEMORY     │  │   COLD MEMORY    │ │
│  │                  │  │                   │  │                  │ │
│  │ Current context  │  │ Mem0 / LangMem    │  │ PostgreSQL +     │ │
│  │ window + last N  │  │ Structured facts  │  │ pgvector         │ │
│  │ messages         │  │ User preferences  │  │ Full history     │ │
│  │                  │  │ Project state     │  │ Archived logs    │ │
│  └──────────────────┘  └───────────────────┘  └──────────────────┘ │
│                                                                     │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │                    LEARNING LOOP                              │   │
│  │                                                              │   │
│  │  Feedback → Extract patterns → Update agent prompts/skills   │   │
│  │  Failed tasks → Root cause → Add to procedural memory        │   │
│  │  Successful patterns → Compress → Store as reusable skills   │   │
│  └──────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────┘
```

### 2.2 Data Flow

1. User sends message in Discord/Telegram
2. Message Router parses intent (is this a new task? a follow-up? feedback on a result?)
3. Coordinator Agent decomposes the request into sub-tasks
4. Sub-agents execute using tools (GitHub API, code sandbox, web search)
5. Results flow back through Coordinator → Message Router → Chat
6. After completion, the Learning Loop extracts facts and patterns into Warm/Cold memory
7. Next request benefits from accumulated knowledge

---

## 3. Agent Capabilities Design

### 3.1 PRD Agent

**Purpose:** Generate and iterate on product requirement documents.

**Tools required:**
- Web search (competitive research, market context)
- File system (read existing docs, write PRD output)
- Memory retrieval (past PRDs, company standards, user preferences)
- Template engine (consistent PRD structure)

**Execution flow:**
1. User: "Write a PRD for a notification service"
2. Agent retrieves: past PRD templates from memory, company tech standards, any existing docs on notifications
3. Agent searches web for: notification service patterns, competing approaches
4. Agent generates PRD draft with sections: Problem, Goals, Non-goals, Technical Approach, Milestones, Success Metrics
5. Returns draft to chat with inline review options
6. User provides feedback → agent iterates
7. Final PRD is committed to the project repo

**Key design decision:** The PRD agent should use a structured template stored in procedural memory, not freestyle every time. After each PRD is approved, the agent updates its template understanding based on what the user changed.

### 3.2 Coder Agent

**Purpose:** Generate, modify, and refactor code based on requirements or bug reports.

**Tools required:**
- GitHub API (read repo structure, create branches, open PRs)
- Code sandbox (isolated execution environment for testing generated code)
- File system (read/write code files)
- LLM with strong code capabilities (Claude Sonnet 4.6 or GPT-4o)
- Memory (project architecture, coding conventions, past decisions)

**Execution flow:**
1. User: "Add rate limiting to the /api/orders endpoint"
2. Agent retrieves: project structure, existing middleware patterns, coding conventions from memory
3. Agent reads relevant files from the repo via GitHub API
4. Agent generates code changes
5. Agent runs the code in a sandbox to verify it compiles/parses correctly
6. Agent opens a PR with the changes, linking to the original request
7. Reports back to chat with PR link and summary of changes

**Key design decision:** The coder agent should never push directly to main. It opens PRs. It should also read the project's existing patterns before generating code — the number one complaint about AI-generated code is that it ignores existing conventions.

### 3.3 Test & Deploy Agents

**Test Agent tools:** Test runner (pytest, jest, go test), code sandbox, coverage reporter, GitHub API (read test files, check CI status)

**Deploy Agent tools:** CI/CD trigger (GitHub Actions API), K8s API / Docker API, monitoring/health check endpoints, rollback mechanisms

**Execution flow (combined):**
1. Coder Agent completes a PR → notifies Test Agent
2. Test Agent runs the existing test suite + generates new tests for changed code
3. If tests pass → Test Agent reports to chat and optionally notifies Deploy Agent
4. Deploy Agent triggers CI/CD pipeline, monitors deployment health
5. If health checks fail → Deploy Agent rolls back and reports to chat
6. If successful → Deploy Agent confirms in chat with environment URL

**Key design decision:** Deploy should always require explicit human approval via the chat interface (a button click, a confirmation message). No autonomous production deployments until trust is established over months of operation.

### 3.4 Research Agent

**Purpose:** Conduct technical, product, and business research.

**Tools required:**
- Web search (multiple queries, iterative)
- Web fetch (read full articles, documentation pages)
- File system (save research outputs)
- Memory (store research findings for future reference)

**Execution flow:**
1. User: "Research Go vs Rust for our new CLI tool — consider performance, ecosystem, and hiring"
2. Agent plans research: 3-5 search queries per dimension
3. Agent executes searches, fetches full articles for top results
4. Agent synthesizes findings into a structured comparison
5. Agent stores key findings in memory for future reference
6. Returns a concise report to chat with option to generate a full document

**Key design decision:** The research agent should cite sources and indicate confidence levels. It should also check memory first — if this topic was researched before, start from there and only search for updates.

### 3.5 Task Delegation Model

The Coordinator Agent is the router. It does not do work — it decomposes and delegates.

```
User request
     │
     ▼
Coordinator analyzes intent
     │
     ├── "Write a PRD for X"          → PRD Agent
     ├── "Add feature X to the code"  → Coder Agent → Test Agent
     ├── "Deploy to staging"          → Deploy Agent (with approval gate)
     ├── "Research X vs Y"            → Research Agent
     ├── "Build feature X end-to-end" → PRD Agent → Coder Agent → Test Agent → Deploy Agent
     └── "What did we decide about X" → Memory retrieval (no agent needed)
```

For multi-step workflows ("Build feature X end-to-end"), the Coordinator chains agents sequentially, passing outputs as inputs. Each handoff is a checkpoint in the orchestration layer. If any step fails, the Coordinator reports the failure point and options to the user.

---

## 4. Learning & Memory Strategy

### 4.1 Memory Architecture: Three Tiers

**Hot Memory (context window):**
What the agent sees right now. Last N messages, current task context, retrieved warm memories. This is the prompt. Keep it lean — every unnecessary token costs money and dilutes attention.

**Warm Memory (structured knowledge store):**
Extracted facts, user preferences, project state, agent skills. This is what makes the agent feel like it "remembers." Implemented with Mem0 (Phase 1) or LangMem + Zep (Phase 2).

Warm memory stores things like:
- "The project uses Go 1.22 with Chi router"
- "User prefers concise PRDs, max 2 pages"
- "Staging deploys go to k8s cluster `staging-ap-southeast-1`"
- "Last deploy failed because of a missing env var DATABASE_URL"

**Cold Memory (full archive):**
Complete conversation logs, full PRD versions, all generated code diffs, raw research outputs. Stored in PostgreSQL. Rarely retrieved directly — instead, warm memory is distilled from cold memory via a background compression process.

### 4.2 The Learning Loop

This is the critical differentiator. Most agent systems are stateless — they restart from zero every conversation. Your system should compound knowledge.

**After every completed task:**

1. **Fact extraction:** A background process (can be a smaller, cheaper model like Haiku) scans the completed task conversation and extracts structured facts.
   - "The orders API now has rate limiting at 100 req/min"
   - "User rejected the first PRD draft because it lacked success metrics"

2. **Pattern detection:** Over time, the system identifies recurring patterns.
   - "User always asks for success metrics in PRDs" → update PRD template
   - "Deploys to staging fail 30% of the time due to env var issues" → add pre-deploy env var check

3. **Skill formation:** Successful multi-step patterns become stored procedures.
   - "When building a new API endpoint: check existing router patterns → generate handler → add tests → add to OpenAPI spec → open PR"

4. **Feedback integration:** When a user corrects an agent or rejects output, capture the correction as a negative example.
   - "Don't use `sync.Mutex` for this pattern, use channels instead" → stored as a project-specific coding convention

### 4.3 Approach Comparison

| Approach | When to Use | Limitations |
|----------|-------------|-------------|
| **RAG (vector search)** | Good starting point. Retrieves semantically similar past content. | Read-only. No learning. Retrieves by similarity, not by relevance-over-time. Returns raw text chunks, not structured knowledge. |
| **Structured memory (Mem0/Zep)** | Production memory. Stores extracted facts, tracks entity relationships, supports temporal queries. | Requires extraction pipeline. Mem0 graph is expensive ($249/mo). Zep needs Neo4j infra. |
| **Git/database as memory** | Store PRDs, code decisions, deployment logs in Git. Searchable, versioned, auditable. | Not semantically searchable without additional indexing. No automatic extraction. |
| **Feedback loops** | Essential. Agent corrections become training data for improved behavior. | Requires engineering the feedback capture and application pipeline. |
| **Fine-tuning** | Last resort. Only when prompt engineering and memory are insufficient. | Expensive, slow iteration cycle, risk of catastrophic forgetting, requires significant data volume. Avoid until Phase 3 at earliest. |

### 4.4 Recommended Strategy

**Phase 1:** Mem0 (managed) for warm memory + PostgreSQL for cold storage. Simple RAG retrieval. Manual feedback via chat reactions (thumbs up/down).

**Phase 2:** Replace Mem0 with LangMem (if on LangGraph) + Zep for temporal reasoning. Add automated fact extraction pipeline. Build the skill formation system.

**Phase 3:** Custom memory layer on PostgreSQL + pgvector + optional Neo4j for graph. Full control over extraction, compression, retrieval, and forgetting policies.

---

## 5. Build vs Use Strategy

### Phase 1: Fast Start (Weeks 1-4)

**Goal:** Working prototype — chat to agent pipeline that executes one workflow end-to-end.

**Stack:**
- **Chat:** Custom Discord bot using `discord.py` (or Telegram with `python-telegram-bot`). ~200 lines of adapter code.
- **Orchestration:** LangGraph for agent definition and workflow. Single-process, in-memory state.
- **LLM:** Claude Sonnet 4.6 via API (best cost/quality ratio for code tasks). Fall back to Haiku for extraction and summarization.
- **Memory:** Mem0 managed (free tier to start). PostgreSQL for conversation logs.
- **Tools:** GitHub API (PyGithub), web search (Tavily or SerpAPI), file system.
- **Hosting:** Single VPS or local machine. No Kubernetes yet.

**What you build:** Coordinator Agent + one specialist (start with PRD Agent or Research Agent — lowest risk, no code execution needed).

**What you skip:** Temporal, CI/CD integration, code sandbox, deploy agent, graph memory. Add these as you prove value.

### Phase 2: Customization (Months 2-4)

**Goal:** Production-grade system with multiple agents, durable execution, and real tool integrations.

**Changes:**
- **Add Temporal** for durable workflow execution. Wrap LangGraph agent calls as Temporal Activities. This gives you crash recovery, retry logic, and observability for free.
- **Add code sandbox** (Docker containers or Firecracker VMs) for safe code execution.
- **Add Coder + Test agents** with GitHub Actions integration.
- **Replace Mem0 managed** with self-hosted Mem0 or LangMem + Zep. Add the automated fact extraction pipeline.
- **Add MCP servers** for extensible tool integration. MCP is becoming the standard protocol for agent-tool communication in 2026 — invest in it now.
- **Add structured logging** and basic evaluation (track task success rate, time to completion, user satisfaction).

### Phase 3: Own Platform (Months 6-12)

**Goal:** Fully self-controlled architecture. No managed service dependencies except LLM APIs.

**Changes:**
- **Custom memory layer** on PostgreSQL + pgvector + Neo4j (if graph is needed). Custom extraction, compression, and retrieval pipelines.
- **Custom agent runtime** — replace LangGraph with your own orchestration layer if LangGraph's abstractions become limiting. Keep Temporal underneath for durability.
- **Model routing** — use OpenRouter or a custom proxy to route different tasks to different models (Claude for code, GPT for research, local models for extraction/summarization to reduce costs).
- **Evaluation framework** — automated testing of agent outputs against golden datasets. Regression detection when prompts or models change.
- **Multi-tenant support** — if you expand beyond personal use to a team platform.

---

## 6. Risks & Trade-offs

### Over-complexity

**Risk:** Building a 5-agent system with Temporal, LangGraph, Mem0, Zep, and MCP before you have a single working agent.

**Mitigation:** Build one agent, one tool, one task first. Get that reliable. Then add complexity incrementally. The advice from every practitioner who has built these systems is the same: start with a single agent. Multi-agent systems are debugging nightmares if each individual agent is not solid.

### Hallucination

**Risk:** Agents confidently generate wrong code, incorrect deployment commands, or fabricated research.

**Mitigation:**
- Never auto-merge code. PRs with human review.
- Never auto-deploy to production without explicit approval.
- Research agent must cite sources. Add a verification step where a second LLM call checks claims against sources.
- Use structured outputs (JSON schemas, typed responses) to constrain agent behavior.

### Debugging Difficulty

**Risk:** When a 4-step agent chain produces wrong output, which agent failed? LLM non-determinism makes reproduction hard.

**Mitigation:**
- Log every LLM call with full prompt, response, and tool calls. LangSmith does this if you're on LangGraph.
- Temporal's event history gives you an exact audit trail of every decision.
- Build replay capability — re-run a failed workflow with the same inputs.

### Scaling Challenges

**Risk:** LLM API rate limits. Token costs growing linearly (or worse) with usage. Memory retrieval latency increasing with knowledge base size.

**Mitigation:**
- Set token budgets per agent per task from day one. Monitor with alerts.
- Use cheaper models for extraction, summarization, and routing. Reserve frontier models for the actual generation step.
- Implement memory compression and forgetting policies. Not everything is worth remembering.
- Expected cost: $200-$2,000/month per active engineer in API costs for agentic systems. Budget accordingly.

### Cost Considerations (Specific)

| Component | Phase 1 Cost | Phase 2 Cost |
|-----------|-------------|-------------|
| LLM API (Claude Sonnet) | $50-200/mo | $200-800/mo |
| Mem0 | Free tier | $25-249/mo (self-host to save) |
| Temporal | N/A | $0 (self-hosted) or $200/mo (Cloud) |
| VPS/Infrastructure | $20-50/mo | $100-300/mo |
| GitHub API | Free (within limits) | Free |
| **Total** | **$70-250/mo** | **$325-1,550/mo** |

### Security

**Risk:** Agents with access to GitHub, K8s, and deployment pipelines are a significant attack surface.

**Mitigation:**
- Principle of least privilege. Each agent gets only the API scopes it needs.
- Code execution in isolated sandboxes only. Never on the host.
- Audit trail for every tool invocation (Temporal gives you this).
- No secrets in agent context. Use a secrets manager; agents reference keys by name, not value.

---

## 7. Final Recommendation

### What stack should I start with?

```
Discord/Telegram Bot  (custom, ~200 lines)
        │
   LangGraph          (agent orchestration)
        │
   Claude Sonnet 4.6  (primary LLM via API)
   Claude Haiku 4.5   (extraction, routing)
        │
   Mem0 (managed)     (warm memory, free tier)
   PostgreSQL         (cold storage, conversation logs)
        │
   GitHub API         (code operations)
   Tavily/SerpAPI     (web search)
```

This gets you to a working system in 2-3 weeks. One Coordinator Agent + one specialist agent (PRD or Research). Add agents incrementally as each one proves reliable.

### What architecture pattern should I follow?

**Coordinator-Specialist pattern** with **explicit tool interfaces**.

The Coordinator agent is a router, not a worker. It decomposes tasks and delegates to specialist agents. Each specialist has a narrow scope, specific tools, and clear success criteria. Specialists do not talk to each other — they report back to the Coordinator, which decides the next step.

This pattern is simpler to debug, easier to evaluate, and naturally maps to LangGraph's node-and-edge model.

When you reach Phase 2, wrap the entire LangGraph execution in a Temporal Workflow for durability. The mental model stays the same — you're just adding a crash-proof shell around your existing logic.

### What should I avoid?

1. **Avoid AutoGen / AG2.** Maintenance mode, high token cost, smallest community. Not worth the investment.
2. **Avoid fine-tuning** until you have exhausted prompt engineering, memory, and tool improvements. Fine-tuning is expensive, slow, and introduces model management complexity you do not need yet.
3. **Avoid building your own orchestration framework** in Phase 1. LangGraph exists. Use it. Build your own only if (and when) you hit concrete limitations that cannot be worked around.
4. **Avoid autonomous deployments.** Always keep a human approval gate for anything that affects production. Trust is earned over months.
5. **Avoid premature multi-agent complexity.** A single well-prompted agent with good tools beats a poorly coordinated 5-agent system every time.
6. **Avoid storing everything in memory.** Implement forgetting from the start. Stale memories actively harm agent performance — the agent retrieves outdated facts and makes wrong decisions.
7. **Avoid treating RAG as memory.** RAG retrieves documents. Memory is structured, evolving, agent-writable knowledge. They are complementary, not interchangeable.

---

## Appendix: Key Technologies Quick Reference

| Category | Recommended | Alternative | Avoid |
|----------|------------|-------------|-------|
| Agent Framework | LangGraph | CrewAI (prototyping only) | AutoGen/AG2 |
| Durable Execution | Temporal (Phase 2+) | — | Rolling your own |
| Chat Interface | Custom bot (discord.py) | LangBot | Heavy frameworks |
| Warm Memory | Mem0 → LangMem + Zep | Letta (if LLM-managed memory) | Plain RAG alone |
| Cold Storage | PostgreSQL + pgvector | — | Purpose-built vector DBs (overkill early) |
| Primary LLM | Claude Sonnet 4.6 | GPT-4o | — |
| Extraction LLM | Claude Haiku 4.5 | GPT-4o-mini, local (Qwen 2.5) | Frontier models (wasteful) |
| Tool Protocol | MCP | REST wrappers | Hardcoded integrations |
| Code Execution | Docker sandbox | Firecracker | Host execution |
| CI/CD Integration | GitHub Actions API | GitLab CI API | Manual triggers |
