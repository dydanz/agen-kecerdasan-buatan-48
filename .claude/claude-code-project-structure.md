Here's a breakdown of each folder — purpose, mental model, and what files belong inside.

---

## `CLAUDE.md` (root file, not a folder)

**Purpose:** The brain stem. Claude Code reads this automatically at session start. Everything else in `.claude/` is only useful if agents know it exists — this file is what connects them.

**What goes here:**
- Project overview and tech stack
- Coding conventions and non-negotiables
- How to navigate the repo
- Pointers to agents, workflows, and knowledge resources
- Model routing rules (which tier handles what)

---

## `agents/`

**Purpose:** Defines *who* does the work. Each file describes a specialized sub-agent — its role, capabilities, model tier, input/output contract, and what it should never do.

**What goes here:**
```
agents/
├── orchestrator.md        ← master agent, delegates to others
├── backend-engineer.md    ← Go/API implementation tasks
├── reviewer.md            ← code review, standards enforcement
├── triage.md              ← classifies incoming tasks (Haiku-tier)
├── devops.md              ← K8s, IaC, infra tasks
└── routing.md             ← decision rules for model selection
```

Think of each file as a job description + operating manual for that agent.

---

## `commands/`

**Purpose:** Reusable slash commands that humans or agents can invoke. These are Claude Code's equivalent of macros or scripts — predefined prompts that trigger a specific, repeatable action.

**What goes here:**
```
commands/
├── review.md              ← /review — runs code review against standards
├── spec.md                ← /spec — generates task spec from a brief
├── debug.md               ← /debug — structured debugging flow
├── standup.md             ← /standup — generates daily update from git log
├── migrate.md             ← /migrate — DB migration checklist + execution
└── test-gen.md            ← /test-gen — generates unit tests for a file
```

Each file is a structured prompt with context about when to use it, what input it expects, and what output format it produces.

---

## `context/`

**Purpose:** Session and task-scoped information. Unlike `knowledge/` (which is stable and evergreen), context is *situational* — it describes the current state of things that agents need to be aware of right now.

**What goes here:**
```
context/
├── current-sprint.md      ← active tickets, goals, deadlines
├── open-prs.md            ← PRs in flight, blockers
├── team.md                ← who owns what, current availability
├── decisions-log.md       ← recent architectural/technical decisions
└── environment.md         ← env setup, local vs staging vs prod notes
```

These files get refreshed regularly — think weekly or per-sprint cadence.

---

## `knowledge/`

**Purpose:** Stable, domain-level reference material. This is the long-term memory of the project — things that don't change sprint to sprint. Agents pull from here when they need authoritative answers about how the system works.

**What goes here:**
```
knowledge/
├── architecture.md        ← system design, service boundaries, data flow
├── api-contracts.md       ← internal API specs, payload formats
├── database-schema.md     ← table definitions, relationships, ENUMs
├── glossary.md            ← domain terms, acronyms (important in fintech)
├── third-party-services.md← external dependencies, rate limits, quirks
└── error-catalog.md       ← known errors, root causes, standard fixes
```

The key distinction from `context/`: if something changes every week, it belongs in `context/`. If it describes how the system fundamentally works, it belongs in `knowledge/`.

---

## `templates/`

**Purpose:** Reusable document skeletons. When agents need to produce structured output — specs, PRDs, RCAs, ADRs — they pull from here so output is consistent across the team.

**What goes here:**
```
templates/
├── task-spec.md           ← feature/task specification template
├── adr.md                 ← Architecture Decision Record
├── rca.md                 ← Root Cause Analysis (you've used this before)
├── pr-description.md      ← pull request template
├── tech-debt.md           ← tech debt documentation format
└── onboarding.md          ← new engineer ramp-up checklist
```

---

## `workflows/`

**Purpose:** Defines *how* multi-step work gets done. While `agents/` describes the roles, `workflows/` describes the process — the sequence of steps, handoffs between agents, decision points, and exit conditions.

**What goes here:**
```
workflows/
├── feature-development.md ← spec → implement → review → test → deploy
├── bug-fix.md             ← triage → diagnose → fix → verify
├── code-review.md         ← review steps, checklist, approval criteria
├── incident-response.md   ← detection → RCA → fix → postmortem
├── db-migration.md        ← migration safety checklist + rollback plan
└── onboarding.md          ← new engineer workflow using agents
```

Each file should clearly define: trigger condition, steps in order, which agent handles each step, and what done looks like.

---

## `hooks/` (recommended addition)

**Purpose:** Automated guardrails that run before or after Claude Code tool executions. These catch problems without requiring agent reasoning cycles — saving tokens and preventing bad states.

**What goes here:**
```
hooks/
├── pre-write.sh           ← lint/format check before file is written
├── post-write.sh          ← run tests after file changes
├── pre-bash.sh            ← block dangerous commands (rm -rf, prod deploys)
└── post-bash.sh           ← log all bash executions for audit
```

---

## `thoughts/` (recommended addition)

**Purpose:** A scratchpad for agent reasoning traces. Useful when debugging complex multi-agent runs — instead of reasoning being lost in the session, agents write intermediate thinking here.

**What goes here:**
```
thoughts/
├── active/                ← in-progress reasoning files
│   └── 2026-04-28-migration-debug.md
└── archive/               ← completed traces for retrospective
```

---

## Quick Reference

| Folder | Scope | Changed by | Read by |
|---|---|---|---|
| `agents/` | Role definitions | Engineers | Orchestrator |
| `commands/` | Reusable actions | Engineers | Humans + Agents |
| `context/` | Current state | Weekly/per-sprint | All agents |
| `knowledge/` | Stable domain truth | As system evolves | All agents |
| `templates/` | Output formats | Engineers | Agents producing docs |
| `workflows/` | Process definitions | Engineers | Orchestrator |
| `hooks/` | Automated guardrails | Engineers | Claude Code runtime |
| `thoughts/` | Reasoning traces | Agents | Engineers debugging |

---

Want me to help draft the actual content for any of these — starting with `CLAUDE.md` and `agents/orchestrator.md` as the two most foundational ones?