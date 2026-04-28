# Klawmbing — Document 5: Reference Library

## Curated sources organized by architectural concern
## Date: April 2026

---

## 1. Thin Agent Runtimes (Klawmbing's category)

| Source | URL | Why it matters |
|--------|-----|----------------|
| OpenClaw (canonical) | https://github.com/openclaw/openclaw | The reference for gateway + channel adapters + skill files + session management |
| OpenClaw Architecture (Paolo Perazzo) | https://ppaolo.substack.com/p/openclaw-system-architecture-overview | Best third-party architecture deep dive |
| OpenClaw Agent Runtime docs | https://docs.openclaw.ai/concepts/agent | Session resolution, context assembly, execution loop |
| Hermes Agent | https://github.com/nousresearch/hermes-agent | Self-improving skills, FTS5 session search, multi-gateway, OpenClaw migration |
| Hermes Agent docs | https://hermes-agent.nousresearch.com/docs/ | Operating manual |
| Turing Post — Hermes vs OpenClaw | https://www.turingpost.com/p/hermes | Architectural comparison of the two approaches |
| ZeroClaw (Rust reimplementation) | https://github.com/zeroclaw-labs/zeroclaw | Edge deployment, single binary, SQLite hybrid memory |
| Zeroclawed (security fork) | https://github.com/bglusman/zeroclawed | Router/agent separation, Starlark policy daemon |
| Claw Code Agent (Python) | https://github.com/HarnessLab/claw-code-agent | Pure Python claw with zero dependencies — study for Klawmbing |
| awesome-openclaw-agents | https://github.com/mergisi/awesome-openclaw-agents | 162 production SOUL.md/skill templates |
| awesome-hermes-agent | https://github.com/0xNyk/awesome-hermes-agent | Curated skills, plugins, GUIs for Hermes |

---

## 2. GBrain & Knowledge Systems

| Source | URL | Why it matters |
|--------|-----|----------------|
| GBrain (Garry Tan) | https://github.com/garrytan/gbrain | THE brain: knowledge graph, hybrid search, dream cycle, Minions, 29 skills |
| GBrain Skillpack docs | https://github.com/garrytan/gbrain/blob/master/docs/GBRAIN_SKILLPACK.md | Skill/convention reference |
| Thin Harness, Fat Skills (ethos) | https://github.com/garrytan/gbrain/blob/master/docs/ethos/THIN_HARNESS_FAT_SKILLS.md | Design philosophy document |
| GBrain evals repo | https://github.com/garrytan/gbrain-evals | BrainBench corpus, scorecards, adapter comparisons |
| Vibe Sparking — GBrain explainer | https://www.vibesparking.com/en/blog/ai/openclaw/2026-04-11-gbrain-garry-tan-opinionated-knowledge-brain/ | Practical walkthrough of compiled-truth + timeline |
| Karpathy LLM Wiki gist | https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f | Original "compiled truth + timeline" concept |
| DAIR.AI — LLM Knowledge Bases | https://academy.dair.ai/blog/llm-knowledge-bases-karpathy | Four-phase pipeline (ingest, compile, query, maintain) |
| Beyond RAG: Karpathy's LLM Wiki Pattern | https://levelup.gitconnected.com/beyond-rag-how-andrej-karpathys-llm-wiki-pattern-builds-knowledge-that-actually-compounds-31a08528665e | Clearest breakdown of three-layer pattern |

---

## 3. Memory Systems (alternatives / comparison)

| Source | URL | Why it matters |
|--------|-----|----------------|
| Mem0 | https://github.com/mem0ai/mem0 | Most popular, framework-agnostic, graph at $249/mo Pro |
| Zep / Graphiti | https://github.com/getzep/graphiti | Bi-temporal knowledge graph, best temporal reasoning |
| Zep paper (arXiv) | https://arxiv.org/abs/2501.13956 | Foundational paper on temporal KG for agent memory |
| Graphiti MCP server | https://www.getzep.com/product/knowledge-graph-mcp/ | Knowledge graph as MCP tool surface |
| Letta (MemGPT) | https://github.com/letta-ai/letta | OS-inspired memory tiers, self-managing context |
| Letta filesystem benchmark | https://www.letta.com/blog/benchmarking-ai-agent-memory | "Is a filesystem all you need?" — validates GBrain's approach |
| Vectorize — 8 Memory Frameworks Compared | https://vectorize.io/articles/best-ai-agent-memory-systems | Most thorough comparison (2026) |
| Atlan — Memory Framework Guide | https://atlan.com/know/best-ai-agent-memory-frameworks-2026/ | Decision framework by use case |
| DEV.to — 5 Systems Compared | https://dev.to/varun_pratapbhardwaj_b13/5-ai-agent-memory-systems-compared-mem0-zep-letta-supermemory-superlocalmemory-2026-benchmark-59p3 | Benchmark-focused comparison |
| OpenAI Cookbook — Temporal Agents with KG | https://developers.openai.com/cookbook/examples/partners/temporal_agents_with_knowledge_graphs/temporal_agents | Working pipeline for time-stamped triplet extraction |
| Neo4j on Graphiti | https://neo4j.com/blog/developer/graphiti-knowledge-graph-memory/ | Why bi-temporal beats batch GraphRAG |

---

## 4. Agent Skills Standard & Fat Skills Pattern

| Source | URL | Why it matters |
|--------|-----|----------------|
| Anthropic — Agent Skills engineering post | https://www.anthropic.com/engineering/equipping-agents-for-the-real-world-with-agent-skills | Canonical article: SKILL.md format, progressive disclosure |
| Anthropic — Skills Guide (PDF) | https://resources.anthropic.com/hubfs/The-Complete-Guide-to-Building-Skill-for-Claude.pdf | Most thorough authoring guide |
| Claude API — Skill best practices | https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices | "Keep SKILL.md under 500 lines" |
| agentskills.io (open standard) | https://agentskills.io/home | Cross-platform standard adopted by Claude, Codex, Copilot, Cursor |
| Simon Willison — Agent Skills | https://simonwillison.net/2025/Dec/19/agent-skills/ | Independent, skeptical analysis |
| Lee Hanchung — Skills Deep Dive | https://leehanchung.github.io/blogs/2025/10/26/claude-skills-deep-dive/ | How progressive disclosure works at runtime |
| Dean Blank — Skills, Subagents, Plugins | https://levelup.gitconnected.com/a-mental-model-for-claude-code-skills-subagents-and-plugins-3dea9924bf05 | Clean conceptual map |
| GStack (Garry Tan) | https://github.com/garrytan/gstack | 23 role-based skills — flagship fat-skills example |
| HumanLayer — Writing a good CLAUDE.md | https://www.humanlayer.dev/blog/writing-a-good-claude-md | Counter-perspective on keeping always-loaded layer thin |
| awesome-claude-skills | https://github.com/travisvn/awesome-claude-skills | Community skill library |

---

## 5. Chat-Controlled Agent Systems

| Source | URL | Why it matters |
|--------|-----|----------------|
| grammY (Telegram framework) | https://github.com/grammyjs/grammY | De facto Telegram bot framework for TS/Deno |
| OpenClaw Telegram docs | https://docs.openclaw.ai/channels/telegram | Complete worked example of thin Telegram adapter |
| Claude Code Channels (official) | https://code.claude.com/docs/en/channels-reference | Anthropic's MCP-based channel architecture |
| Claude Code Channels guide | https://claudefa.st/blog/guide/development/claude-code-channels | Practical async workflow patterns |
| MindStudio — Channels setup | https://www.mindstudio.ai/blog/claude-code-channels-telegram-discord-setup | End-to-end explainer |
| zebbern/claude-code-discord | https://github.com/zebbern/claude-code-discord | Self-hosted Discord bot with sandbox configs |
| chadingTV/claudecode-discord | https://github.com/chadingTV/claudecode-discord | Multi-machine Discord bot, channels = projects |
| AIXerum/AI-Telegram-Assistant | https://github.com/AIXerum/AI-Telegram-Assistant | Telegram orchestrates email/calendar/Notion sub-agents |

---

## 6. Solo CTO/CEO Agent Use Cases

| Source | URL | Why it matters |
|--------|-----|----------------|
| GBrain README (Garry Tan's actual setup) | https://github.com/garrytan/gbrain | 17,888 pages, 4,383 people, 21 cron jobs |
| TechCrunch on Garry Tan | https://techcrunch.com/2026/03/17/why-garry-tans-claude-code-setup-has-gotten-so-much-love-and-hate/ | Mainstream context |
| AI Builder Club — Garry Tan workflow | https://www.aibuilderclub.com/blog/garry-tan-ai-coding-workflow | Parallel-agent workflow (10-15 simultaneous sprints) |
| mean.ceo — Solo Founder AI Agent Stack | https://blog.mean.ceo/the-solo-founder-ai-agent-stack-that-is-replacing-entire-startup-teams/ | Triage framework: formulaic vs judgment × damage vs impact |
| MindStudio — AI Second Brain | https://www.mindstudio.ai/blog/ai-second-brain-claude-code-obsidian-architecture | Obsidian + Claude Code with heartbeat cycles |
| AIMaker — Karpathy's LLM Wiki as Second Brain | https://aimaker.substack.com/p/llm-wiki-obsidian-knowledge-base-andrej-karphaty | Operator-level personal knowledge agent build |
| The AI Corner — Karpathy workflow shift | https://www.the-ai-corner.com/p/andrej-karpathy-ai-workflow-shift-agentic-era-2026 | "I haven't typed a line of code since December" |

---

## 7. Session Management & Context Compaction

| Source | URL | Why it matters |
|--------|-----|----------------|
| Contextual Memory Virtualisation (arXiv) | https://arxiv.org/pdf/2602.22402 | DAG-based session management, structurally lossless trimming |
| Morph — Compaction vs Summarization | https://www.morphllm.com/compaction-vs-summarization | Three approaches compared with benchmarks |
| Google ADK — Context Compaction | https://google.github.io/adk-docs/context/compaction/ | Sliding-window LLM-summarization |
| Microsoft Agent Framework — Compaction | https://learn.microsoft.com/en-us/agent-framework/agents/conversations/compaction | Self-managed vs service-managed agents |
| Piebald-AI — Dream consolidation prompt | https://github.com/Piebald-AI/claude-code-system-prompts/blob/main/system-prompts/agent-prompt-dream-memory-consolidation.md | Concrete prompt template for nightly compaction |
| Claude Code session issues | https://github.com/anthropics/claude-code/issues/41591 | Real-world failure modes for JSONL persistence |

---

## 8. MCP Integration Patterns

| Source | URL | Why it matters |
|--------|-----|----------------|
| MCP official — Build with Agent Skills | https://modelcontextprotocol.io/docs/develop/build-with-agent-skills | MCP server scaffolding and discovery |
| MCP reference servers | https://github.com/modelcontextprotocol/servers | Memory, Filesystem, Git, Fetch — closest to GBrain off-the-shelf |
| Cloudflare — MCP design guidance | https://developers.cloudflare.com/agents/model-context-protocol/ | "Don't wrap your full API — build for specific user goals" |
| lastmile-ai/mcp-agent | https://github.com/lastmile-ai/mcp-agent | Router/orchestrator patterns over MCP + optional Temporal backend |
| Wikipedia — Model Context Protocol | https://en.wikipedia.org/wiki/Model_Context_Protocol | Neutral overview including unresolved security issues |

---

## 9. Deterministic-First Routing & Cost Reduction

| Source | URL | Why it matters |
|--------|-----|----------------|
| Roborhythms — Cut AI Agent API Bill 10-33x | https://www.roborhythms.com/cut-ai-agent-api-costs/ | Tag every LLM call, replace ~84% with regex/classifier/rules |
| Agentbus — Route LLM Traffic by Cost | https://agentbus.sh/posts/how-to-route-llm-traffic-by-cost-and-complexity/ | LiteLLM Router config with complexity classifier |
| arXiv — Routing Strategies Survey | https://arxiv.org/html/2502.00409v2 | Academic survey of HybridLLM, RouteLLM, DistilBERT routing |
| arXiv — MasRouter | https://arxiv.org/pdf/2502.11133 | Plug-and-play routing for multi-agent systems |

---

## 10. Dream Cycles & Cron Scheduling

| Source | URL | Why it matters |
|--------|-----|----------------|
| DEV.to — OpenClaw Dreaming Guide 2026 | https://dev.to/czmilo/openclaw-dreaming-guide-2026-background-memory-consolidation-for-ai-agents-585e | Three-phase model, six weighted scoring signals |
| LeoYeAI/openclaw-auto-dream | https://github.com/LeoYeAI/openclaw-auto-dream | Four-phase dream cycle plugin with HTML dashboard |
| OpenClaw Launch — Dreaming Guide | https://openclawlaunch.com/blog/openclaw-dreaming-background-memory-guide | Cron schedule examples and config flags |
| TheClawTips — Agent That Dreams | https://www.theclawtips.com/blog/how-to-build-an-ai-agent-that-dreams | autoDream config with self-healing memory |
| RogueCtrl/OpenClawDreams | https://github.com/RogueCtrl/OpenClawDreams | Encrypted dream store, surreal narrative generation |

---

## 11. Deployment & Infrastructure

| Source | URL | Why it matters |
|--------|-----|----------------|
| heyabhishek — Self-hosting with Docker + Tailscale | https://heyabhishek.com/blog/self-hosting-openclaw-docker-tailscale/ | Most practical deployment guide for $5 VPS |
| DEV.to — VPS + Tailscale zero public ports | https://dev.to/nunc/self-hosting-openclaw-ai-assistant-on-a-vps-with-tailscale-vpn-zero-public-ports-35fn | Firewall + Tailscale SSH pattern |
| DEV.to — AWS EC2 + Docker + Tailscale | https://dev.to/aws-builders/deploy-your-own-247-ai-agent-on-aws-ec2-with-docker-tailscale-the-secure-way-53aa | AWS variant with budget caps |
| Tailscale — Fly.io integration | https://tailscale.com/kb/1132/flydotio | Multistage Dockerfile for Fly + tailscaled |
| Tailscale — Self-host local AI stack | https://tailscale.com/blog/self-host-a-local-ai-stack | Proxmox + NixOS + Docker + Ollama reference |
| juanfont/headscale | https://github.com/juanfont/headscale | Self-hosted Tailscale control server |

---

## Top 10 "Read These First" Sources

1. **GBrain README** — https://github.com/garrytan/gbrain
2. **GBrain Skillpack docs** — https://github.com/garrytan/gbrain/blob/master/docs/GBRAIN_SKILLPACK.md
3. **Thin Harness, Fat Skills** — https://github.com/garrytan/gbrain/blob/master/docs/ethos/THIN_HARNESS_FAT_SKILLS.md
4. **OpenClaw Agent Runtime docs** — https://docs.openclaw.ai/concepts/agent
5. **OpenClaw Architecture (Paolo Perazzo)** — https://ppaolo.substack.com/p/openclaw-system-architecture-overview
6. **Anthropic Agent Skills post** — https://www.anthropic.com/engineering/equipping-agents-for-the-real-world-with-agent-skills
7. **Claude API Skill best practices** — https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices
8. **Karpathy LLM Wiki gist** — https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f
9. **Zep/Graphiti paper** — https://arxiv.org/abs/2501.13956
10. **Self-hosting with Docker + Tailscale** — https://heyabhishek.com/blog/self-hosting-openclaw-docker-tailscale/

Start with these ten. Everything else is supporting material.
