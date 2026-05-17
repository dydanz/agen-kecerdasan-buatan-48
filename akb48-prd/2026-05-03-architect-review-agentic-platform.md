# Architect Review — AKB48 as Agentic AI Platform for Engineering Org

**Reviewer:** Senior AI Architect (independent)
**Date:** 2026-05-03
**Scope:** PRD-00 through PRD-05 + 2026-05-01 memory-management-design
**Question asked:** Is the proposed solution technically sensible, extensible, and impactful for building an *Agentic AI Platform to support an engineering organization*?

---

## Verdict

**Weak — for the stated framing of "Agentic AI Platform for an engineering organization."**
**Strong — for what the PRDs actually describe: a personal AI for a solo operator.**

This is a category mismatch, not a quality problem. The PRDs are well-scoped, internally coherent, and explicitly self-limit to a single-operator use case. PRD-00 §5.3 lists multi-tenant, plugin ecosystem, and dashboards as **non-goals**. The research file `01-akb48-platform-landscape.md` explicitly rejects LangGraph/CrewAI/AutoGen as "team-platform orchestration" and chooses a "claw-class" runtime instead.

If the goal genuinely is a platform for an engineering organization, this design needs a structural rebuild, not a Phase 2 expansion. Calling the gaps below "Phase 2" undersells the work — the foundational primitives required (durable workflows, multi-tenancy, approval gates, eval) are not natural extensions of the current architecture; they replace pieces of it.

---

## Key Strengths (max 5)

1. **Disciplined scope and "thin harness, fat skills" philosophy.** ~2,000 LOC runtime + behavior-as-markdown is a defensible architecture for a solo operator. Skill files hot-reload without code deploys. Adding capability ≠ adding code. This is the right posture — but only at solo scale.
2. **Explicit knowledge delegation to GBrain over MCP.** Treating memory as a separable subsystem behind a stable JSON-RPC interface is the most extensible decision in the entire design. GBrain is process-isolated, swappable, and already speaks the protocol the LLM uses for tools. This single decision is what gives the platform any real upgrade path.
3. **Memory taxonomy in the 2026-05-01 design doc.** Five entity types (`person | project | decision | product | policy`) with a `scope` field is the only forward-looking primitive that would generalize to teams. This is genuinely good design and should be elevated from Phase 2 to Phase 1 if the org-platform framing is real.
4. **Idempotency keys on tool calls (with the recent fix).** Side-effecting actions are gated by stable keys derived from `(turnID, toolName, input)`. The architecture takes "never double-deploy" seriously enough to bake in the primitive — even if persistence of those keys is still in-memory only.
5. **4-tier prompt-cache discipline.** Identity + skill + session-tail are cached; cold opener and live turn are not. This is the kind of cost-engineering most "agentic" demos skip and pay for in production. Documented carefully.

---

## Critical Gaps / Risks (max 5)

### 1. No orchestration layer — only a single tool-use loop

The runtime is one `Caller.Call()` invocation per turn. Inside it: a bounded loop of LLM → tool → LLM, capped at 5 rounds. That is **not** workflow orchestration. An engineering-org agent platform needs durable, multi-step, branching, long-running workflows:

- "Open a PR → wait for CI → if green, request review from owner → if approved, deploy to staging → poll health for 10 min → roll back on regression."

This is days of wall-clock time, dozens of state transitions, multiple human-in-the-loop checkpoints. None of it fits in a 5-round tool loop. You need Temporal, Restate, or a hand-rolled durable state machine over the JSONL log. Currently the `tool-calls.jsonl` and `sessions/*.jsonl` are passive audit logs, not workflow checkpoints.

### 2. No multi-tenancy, no RBAC, no scoped knowledge

The auth model is `allowed_user_ids = [123456789]`. The brain is single-instance with no scope enforcement. The session ID format hints at `group:` vs `main:` prefixes but the sandboxing is design-only. The 2026-05-01 doc adds `scope: org | group:...` to entity records but defers enforcement.

For an engineering org you need: SSO/identity federation, per-team knowledge scopes, per-role tool allowlists ("only SREs can call `deploy_prod`"), and audit chains tied to identity. None of this exists. The runtime treats the operator as the trust boundary.

### 3. Integration surface is empty for engineering work

Phase 1 integrations: Anthropic API + GBrain + Telegram + CLI. That's it.

What an engineering-org platform needs and where it sits in the plan:
- GitHub (PRs/issues/reviews/CI) — Phase 2
- Code sandbox / shell execution — Phase 2 ("when exposing to others")
- Slack — not in any PRD
- Linear/Jira/Notion — not in any PRD
- Sentry/Datadog/Grafana — not in any PRD
- k8s/AWS APIs — not in any PRD
- CI runners (GitHub Actions, Buildkite) — not in any PRD

The architecture supports adding these via the tool registry, but the platform claim cannot be evaluated when none of the integrations that would deliver engineering value are scoped. The "AI agent for engineering org" thesis lives or dies on these.

### 4. No eval, no replay, no observability for agent quality

`tool-calls.jsonl` records token counts and latency. That tells you the agent ran, not whether it did the right thing. There is no:
- Golden-set test suite per skill
- Replay-with-different-prompt harness
- Success-rate dashboard
- Regression detection on skill changes
- A/B framework for prompt or model tweaks

Phase 3 mentions "eval framework" as future work. For an engineering platform shipping autonomous actions to production, eval is not Phase 3 — it's the gate that lets you ship anything autonomous at all. Without it, every skill change is a blind deploy of a probabilistic system.

### 5. Approval-gate UX is missing where it matters most

The hard rules in CLAUDE.md ("never auto-deploy", "require explicit operator approval") are enforced by prompt instruction, not by runtime primitives. There is no `require_approval: true` flag on tool registrations. There is no Telegram inline-keyboard approval button (Phase 2). There is no signed approval token. There is no audit chain linking "human said yes" → "tool executed."

For an org platform handling production-affecting actions, approval gates must be a runtime primitive enforced by the tool registry, not a behavioral instruction the LLM can be jailbroken out of. This is a security-critical gap, not a UX nicety.

---

## Recommendations (practical, actionable)

### Recommendation A — Decide the actual target, then commit

The PRDs and the framing of this review describe two different products. Before any further build, pick one:

- **Path 1: Personal AI for solo operator.** Keep the current scope. Drop the "agentic platform" framing. Ship Hello World, then 3–5 skills, then GitHub PR creation. This is achievable in 8–12 weeks and the design supports it well.
- **Path 2: Agentic AI Platform for engineering org.** Throw out PRD-02 and PRD-04 in their current form. Build durable workflows + multi-tenancy + approval gates as Phase 1, not as Phase 2/3. This is a 6–12 month rebuild, not a Phase 2.

Trying to do both is the failure mode. The architecture decisions diverge at the foundation: a single-process Go binary with in-memory idempotency cache cannot be retrofitted into a durable, multi-tenant workflow engine without substantial rewrite.

### Recommendation B — If Path 2: introduce a workflow primitive at the runtime layer

Replace the implicit "one Call = one turn = one tool loop" model with explicit `Workflow` objects:

```go
type Workflow struct {
    ID          string
    DefinitionURI string // markdown skill or YAML
    State       string  // pending | running | awaiting_approval | done | failed
    Checkpoints []Checkpoint // persisted to disk on every transition
    HumanApprovals []ApprovalGate
}
```

Tool calls live inside workflow steps. Idempotency keys persist to disk before tool execution, not in a `sync.Map`. Approvals are a runtime primitive: `tool_executor` refuses to run any handler tagged `requires_approval` until a signed approval record exists for the workflow.

Either build this on Temporal/Restate or accept you're building your own. Don't pretend the current Caller does it.

### Recommendation C — Make the knowledge taxonomy a Phase 1 commitment

The `(entity_type, scope, source, decided_at)` record format from the memory-design doc is the right schema for org knowledge. Verify GBrain actually supports `entity_types` filter (the doc flags this as unverified — close that loop now). Ship the typed taxonomy in Phase 1, not Phase 2. This is the single piece of the current design that scales from 1 user to 100 with no architectural change.

### Recommendation D — Build eval before you ship any autonomous action

Before any skill calls a tool that mutates external state (PR, deploy, push), require:
- A golden-set of 10+ representative inputs per skill
- A nightly replay run that scores outputs against expected behavior
- A regression gate on skill edits (no merge if golden-set degrades)

This is more important than any feature in Phase 2. The cost of shipping a regressed deploy skill is unbounded; the cost of golden-sets is one engineer-week per skill.

### Recommendation E — Treat integrations as the product, not the wrapper

For an engineering platform, the runtime is the boring part. The product is the integrations: GitHub, CI, observability, ticket systems, k8s. Spec these as PRDs at the same depth as PRD-01 through PRD-05 before building more runtime. Right now the runtime is over-specified relative to the integrations it would expose, which is a telltale sign of building infrastructure ahead of demand.

---

## What to Change to Make It Truly "Agentic"

The current design has agentic primitives at the **action** layer (tool use, idempotency) and the **memory** layer (GBrain, identity). It is missing them at the **orchestration** and **self-improvement** layers — and orchestration is what separates "AI assistant with tools" from "agent."

Concrete additions, in priority order:

1. **Durable multi-step workflows** with checkpointing. Without this, every turn is amnesia after 5 rounds. The agent cannot operate on engineering timescales (PRs, CI runs, deploys, multi-day investigations).
2. **Multi-agent dispatch.** A router agent that delegates to specialized agents (researcher, coder, deployer, reviewer), each with its own skill set and tool allowlist. Currently one agent + one skill per turn — that is RAG with personality, not multi-agent.
3. **Approval gates as runtime primitives.** Inline-keyboard signed approvals → audit chain → tool execution. Not a prompt-level instruction.
4. **Self-improvement loop ("skillify").** When a skill fails or the operator corrects it, the agent proposes a SKILL.md edit as a PR. This is the highest-leverage feature in the entire research corpus and it sits in Phase 3. For an org platform it's the difference between linear and compounding value.
5. **Eval harness.** Mandatory before autonomous production actions. Without it the agent is unfalsifiable.
6. **A2A / agent-to-agent protocol.** If multi-agent ships, agents need a stable interface to call each other that isn't "embed in prompt." Reuse MCP — agents-as-MCP-servers — and you get tool calls, scoping, and audit for free.

If those six land, this is genuinely an agentic platform. Without them — even after Phase 2 and 3 as currently scoped — it's a well-engineered personal assistant with a brain.

---

## Impact Assessment

| Dimension | Solo operator (Path 1) | Engineering org platform (Path 2 as currently designed) |
|---|---|---|
| **Engineering velocity** | Real but bounded. PR creation + research briefs + note capture genuinely save hours/week for one person. | Negligible until GitHub/CI/sandbox integrations are scoped. The runtime has no engineering integrations in Phase 1. |
| **Operational efficiency** | Real. Auto-extraction of decisions/facts to GBrain compounds. | Negligible without scoped knowledge, RBAC, and multi-team scopes. |
| **Knowledge management** | Strong. GBrain delegation is the right call and the entity taxonomy is solid. | Strong primitives exist (taxonomy + scope) but enforcement is Phase 2. Cannot be deployed to a team yet. |
| **Risk envelope** | Acceptable for one user with clear hard rules. | Unacceptable for org use — no audit chain, no approval primitives, no isolation. |

---

## Bottom Line

The design is one of the better solo-operator agent runtimes I've reviewed. It has a clear philosophy, defensible decisions, intentional non-goals, and genuine craft in the cost-engineering and memory layers. As a personal AI it should ship.

It is not, today, an agentic platform for an engineering organization, and the gap to becoming one is not a Phase-2 increment. It is a different product with overlapping primitives. Decide which one you're building, and build that one.

If you want the platform: invest first in durable workflows, multi-tenancy + RBAC, approval gates, and eval. The runtime, identity files, and GBrain layer can be reused. The session manager and channel adapter cannot — they're solo-operator shaped.

---

## File References

- `akb48-prd/PRD-00-akb48-master.md` — north star, non-goals
- `akb48-prd/PRD-01-core-runtime.md` — runtime, tool registry, LLM caller
- `akb48-prd/PRD-02-channel-adapters.md` — Telegram + CLI only, no approval UX
- `akb48-prd/PRD-03-gbrain-integration.md` — MCP delegation (good)
- `akb48-prd/PRD-04-identity-skills.md` — single-skill-per-turn, substring resolver
- `akb48-prd/PRD-05-session-management.md` — JSONL, no compaction in Phase 1
- `akb48-prd/2026-05-01-memory-management-design.md` — entity taxonomy + scope (the org-relevant piece)
- `akb48-rsh/01-akb48-platform-landscape.md` — explicit "not a team platform" positioning
- `akb48-rsh/03-akb48-build-strategy-risks.md` — three-phase build plan, eval/skillify in Phase 3
