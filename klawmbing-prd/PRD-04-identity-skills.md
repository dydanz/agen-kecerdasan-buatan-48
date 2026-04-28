# PRD-04: Identity & Skill System

**Status:** Draft v1.0
**Parent:** PRD-00 (Klawmbing Master PRD)
**Author:** Dandi
**Created:** April 26, 2026
**Dependencies:** PRD-01 (Core Runtime — context assembly hook)
**Estimated Effort:** 3-4 days

---

## 1. Problem

Without a persistent identity, Klawmbing is a generic AI assistant. Without skills, every capability must be hardcoded in the runtime. This PRD establishes two core principles:

1. **Identity:** The agent knows who it is (SOUL.md), what rules to follow (AGENTS.md), and who the operator is (USER.md). These are loaded at startup and stay consistent across every conversation.

2. **Skills:** Agent capabilities are encoded in markdown files (SKILL.md), not in code. Adding a new capability = writing a new markdown file. The skill resolver maps user intent to the correct skill and injects only that skill into the system prompt for that turn.

This is the "fat skills" half of "thin harness, fat skills."

---

## 2. Goals

- **G1:** Klawmbing loads identity files (AGENTS.md, SOUL.md, USER.md) at startup and includes them in every system prompt
- **G2:** Skills are markdown files following the Agent Skills standard (SKILL.md format)
- **G3:** A skill resolver maps user intent to the correct skill file
- **G4:** Only ONE skill is injected per turn (keeps token count low)
- **G5:** If no skill matches, the agent operates with identity-only context (general assistant mode)
- **G6:** Adding a new skill requires zero code changes — only a new markdown file

## 3. Non-Goals

- Procedural memory / self-authoring skills (Phase 2 — the "skillify" loop)
- Skill conflict detection / reachability audit
- Skill versioning
- Multi-skill injection per turn
- Skill marketplace or sharing

---

## 4. User Stories

| ID | Story | Acceptance Criteria |
|----|-------|-------------------|
| US-S01 | As an operator, Klawmbing responds with the personality I defined in SOUL.md | Response tone, style, and behavior match SOUL.md instructions |
| US-S02 | As an operator, Klawmbing follows the rules I defined in AGENTS.md | Agent refuses or behaves according to rules (e.g., "never push to main") |
| US-S03 | As an operator, Klawmbing knows who I am from USER.md | Agent references operator context naturally (name, company, role) without being told each session |
| US-S04 | As an operator, I ask to "research Go vs Rust" and the research skill is invoked | Log shows "Skill resolved: research". Response follows the research skill template. |
| US-S05 | As an operator, I ask "What's the weather like?" (no matching skill) and get a general response | Agent responds in general mode. Log shows "No skill matched, using general mode." |
| US-S06 | As a developer, I add a new skill file to `skills/` and it's immediately available | No restart required. Skill resolver re-reads the skills directory on each message. |
| US-S07 | As a developer, I can see which skill was resolved for any message | Debug log shows: "Intent: [research], Skill: [skills/research/SKILL.md], Tokens: [340]" |

---

## 5. Technical Design

### 5.1 Identity Files

#### identity/AGENTS.md — Operational Rules

```markdown
# Klawmbing Agent Rules

## Core Rules
- You are Klawmbing, a personal AI agent for the operator.
- You have access to a knowledge brain (GBrain) via tools. Use it proactively.
- When the operator shares a fact, store it in the brain using gbrain_put.
- When answering questions that might benefit from stored knowledge, search the brain first using gbrain_search.
- Always be concise. The operator reads on mobile (Telegram). Keep responses scannable.
- If you're unsure about something, say so. Don't fabricate.

## Safety Rules
- Never execute destructive operations (rm -rf, drop database, force push) without explicit confirmation.
- Never push directly to main. Always use branches and PRs.
- Never deploy to production without explicit operator approval.
- Never share secrets, tokens, or credentials in chat messages.

## Communication Style
- Use short paragraphs (2-3 sentences max on mobile).
- Use bullet points sparingly — only when listing 3+ items.
- Lead with the answer, then context if needed.
- Don't start responses with "Sure!" or "Of course!" — just answer.
```

#### identity/SOUL.md — Personality

```markdown
# Klawmbing's Soul

You are a sharp, opinionated technical co-founder with deep backend engineering experience.
You think in systems. You default to simplicity over cleverness.
You push back when the operator's idea is over-engineered — but you do it respectfully.
You remember everything the operator has told you (via the brain) and reference it naturally.
You are not a yes-machine. You are a thinking partner.

When the operator says "just do it," you do it without asking clarifying questions.
When the operator asks for your opinion, you give it directly — then explain why.

Tone: professional, concise, slightly informal. Like a senior engineer on Slack.
Never use emojis. Never use exclamation marks unless quoting the operator.
```

#### identity/USER.md — Operator Context

```markdown
# Operator Profile

Name: Dandi
Role: CTO/CEO (Solo Operator)
Company: [TODO: Company name]
Location: Indonesia (UTC+7)
Background: Backend engineering (Go, Python), engineering management

## Working Preferences
- Communicates via Telegram primarily
- Prefers concise answers (mobile-first reading)
- Values ownership and proactive contribution
- Wants agents that improve over time, not one-shot tools

## Technical Context
- Primary languages: Go, Python
- Infrastructure: Kubernetes, Docker, GitHub Actions
- Database: PostgreSQL
- Cloud: [TODO: AWS/GCP/other]

## Current Focus
- Building a company as a solo operator
- Establishing AI agent infrastructure (Klawmbing)
- [TODO: Add current business priorities]
```

### 5.2 System Prompt Composition

The context assembler builds the system prompt for each LLM call by layering:

```
┌──────────────────────────────────────────┐
│           SYSTEM PROMPT                  │
│                                          │
│  1. AGENTS.md (operational rules)        │  ← Always loaded
│  2. SOUL.md (personality)                │  ← Always loaded
│  3. USER.md (operator context)           │  ← Always loaded
│  4. Resolved SKILL.md (one, if matched)  │  ← Per-turn, optional
│  5. Brain context (retrieved memories)   │  ← Per-turn, optional
│  6. Session summary (if compacted)       │  ← Per-turn, optional
│                                          │
│  Estimated tokens: 500-1500              │
│  (without skill: ~500)                   │
│  (with skill: ~800-1500)                 │
└──────────────────────────────────────────┘
```

```python
class ContextAssembler:
    """
    Assembles the system prompt for each LLM call.
    Layers: identity → skill → brain context → session context.
    """

    def __init__(self, config: Config, skill_resolver: SkillResolver):
        self.agents_md = self._load_identity("AGENTS.md")
        self.soul_md = self._load_identity("SOUL.md")
        self.user_md = self._load_identity("USER.md")
        self.skill_resolver = skill_resolver

    def assemble(self, message: Message,
                 session_context: str | None = None,
                 brain_context: str | None = None) -> str:
        """
        Build the full system prompt for this turn.

        1. Always include: AGENTS.md + SOUL.md + USER.md
        2. Resolve skill from message content → inject if matched
        3. Append brain context if available
        4. Append session summary if available
        """

        parts = [
            self.agents_md,
            self.soul_md,
            self.user_md,
        ]

        # Resolve and inject skill
        skill = self.skill_resolver.resolve(message.content)
        if skill:
            parts.append(f"\n---\n## Active Skill: {skill.name}\n\n{skill.content}")
            logger.debug(f"Skill resolved: {skill.name} ({skill.token_estimate} tokens)")
        else:
            logger.debug("No skill matched, using general mode")

        # Brain context (pre-fetched relevant memories, if any)
        if brain_context:
            parts.append(f"\n---\n## Relevant Brain Context\n\n{brain_context}")

        # Session summary (from compaction)
        if session_context:
            parts.append(f"\n---\n## Session Context\n\n{session_context}")

        return "\n\n".join(parts)

    def _load_identity(self, filename: str) -> str:
        """Load a file from the identity directory. Fail fast if missing."""
```

**Prompt caching note:** The identity files (AGENTS.md + SOUL.md + USER.md) are identical across every turn. Use Claude's `cache_control` to cache this prefix. The skill injection changes per turn so it sits after the cache boundary.

### 5.3 Skill File Format

Following the Agent Skills standard (agentskills.io):

```
skills/
├── research/
│   └── SKILL.md
├── note-capture/
│   └── SKILL.md
├── daily-briefing/
│   └── SKILL.md
├── prd/
│   └── SKILL.md
└── coder/
    └── SKILL.md
```

Each SKILL.md:

```markdown
---
name: research
description: >
  Conduct technical, product, or business research on a topic.
  Triggered when the operator asks to research, compare, investigate,
  or analyze something that requires web search and synthesis.
triggers:
  - research
  - compare
  - investigate
  - analyze
  - "what are the options for"
  - "pros and cons of"
  - "look into"
---

# Research Skill

## Process
1. Clarify the research question (if ambiguous, ask one clarifying question max)
2. Search the brain first — check if this topic has been researched before
3. Search the web for current information (3-5 queries per dimension)
4. Synthesize findings into a structured comparison or analysis
5. Store key findings in the brain for future reference
6. Present a concise summary in chat

## Output Format
- Lead with the bottom-line recommendation
- Support with 2-3 key findings
- Note confidence level (high/medium/low) based on source quality
- Cite sources when possible
- Offer to generate a full document if the operator wants more detail

## Brain Storage
After completing research, store a summary page in the brain:
- Title: "Research: {topic}"
- Tags: [research, {domain}]
- Compiled truth: key findings and recommendation
- Timeline: date + sources consulted

## Constraints
- Max 5 web searches per research task (cost control)
- If the topic was already researched (found in brain), start from there and only search for updates
- Don't hallucinate sources — if you can't find evidence, say so
```

### 5.4 Skill Resolver

```python
class Skill:
    """A loaded skill file."""
    name: str
    description: str
    triggers: list[str]
    content: str              # Full markdown body
    token_estimate: int       # Approximate tokens
    file_path: str            # For debugging

class SkillResolver:
    """
    Maps user message intent to a skill file.

    Phase 1: Keyword-based matching from frontmatter 'triggers'.
    Phase 2: Lightweight classifier (when 10+ skills make keyword matching brittle).
    """

    def __init__(self, skills_dir: str):
        self.skills_dir = skills_dir
        self.skills: list[Skill] = []

    def load_skills(self) -> None:
        """
        Scan skills_dir for SKILL.md files.
        Parse YAML frontmatter for name, description, triggers.
        Load body content.
        Estimate token count (chars / 4 as rough approximation).
        """

    def resolve(self, message_content: str) -> Skill | None:
        """
        Match message content against skill triggers.

        Algorithm:
        1. Lowercase the message
        2. For each skill, check if any trigger phrase appears in the message
        3. If multiple skills match, pick the one with the most specific (longest) trigger
        4. If no skill matches, return None (general mode)
        5. If matched, re-read the SKILL.md file (hot-reload for development)

        Returns: Skill object or None
        """

    def list_skills(self) -> list[dict]:
        """Return skill summaries for debugging / status commands."""
```

**Hot-reload behavior:** On every message, the resolver re-reads the SKILL.md file from disk. This means the operator can edit a skill file and the change takes effect on the next message — no restart required. The performance cost is negligible (reading a few KB from disk).

**Trigger matching algorithm (Phase 1):**
```python
def resolve(self, message_content: str) -> Skill | None:
    message_lower = message_content.lower()
    matches = []

    for skill in self.skills:
        for trigger in skill.triggers:
            if trigger.lower() in message_lower:
                matches.append((skill, len(trigger)))  # (skill, specificity)
                break  # One match per skill is enough

    if not matches:
        return None

    # Most specific trigger wins (longest match)
    matches.sort(key=lambda x: x[1], reverse=True)
    return matches[0][0]
```

### 5.5 Hello World Skills

For the "hello world" milestone, create two minimal skills:

#### skills/note-capture/SKILL.md

```markdown
---
name: note-capture
description: >
  Capture and store information, facts, decisions, or context
  that the operator wants to remember.
triggers:
  - remember
  - note that
  - store this
  - save this
  - don't forget
  - keep in mind
---

# Note Capture Skill

When the operator asks you to remember something:
1. Extract the key fact or decision
2. Store it in the brain using gbrain_put
3. Confirm what was stored in a single sentence

Use a clear page title that makes the fact findable later.
Tag appropriately: [fact, decision, config, person, company] etc.

If the fact relates to an existing brain page, update that page instead of creating a new one.
```

#### skills/research/SKILL.md

(Full content as shown in section 5.3 above)

---

## 6. Configuration

```toml
[skills]
skills_dir = "skills"             # Relative to ~/.klawmbing/
identity_dir = "identity"         # Relative to ~/.klawmbing/
hot_reload = true                 # Re-read SKILL.md on every message
max_skill_tokens = 2000           # Warn if a skill exceeds this
```

---

## 7. Validation & Error Handling

| Error | Handling | User-facing |
|-------|----------|-------------|
| AGENTS.md missing | Fail fast on startup: "identity/AGENTS.md not found" | Process doesn't start |
| SOUL.md missing | Fail fast on startup: "identity/SOUL.md not found" | Process doesn't start |
| USER.md missing | Warning on startup, continue without operator context | "Warning: USER.md not found. Operating without operator context." |
| SKILL.md has invalid frontmatter | Skip the skill, log warning | "Warning: skills/X/SKILL.md has invalid frontmatter, skipping" |
| SKILL.md exceeds max_skill_tokens | Log warning, still load | "Warning: skills/X/SKILL.md is {N} tokens, recommended max is 2000" |
| Multiple skills match with equal specificity | Pick the first match (alphabetical order) | Log: "Multiple skills matched: [X, Y]. Using X (first match)." |
| skills/ directory empty | No skills loaded, general mode only | Log: "No skills found. Operating in general mode." |

---

## 8. Acceptance Criteria

- [ ] AGENTS.md + SOUL.md are loaded at startup; missing either causes clear error and exit
- [ ] USER.md is loaded if present; missing causes warning but runtime continues
- [ ] Agent's response tone matches SOUL.md personality
- [ ] Agent follows rules in AGENTS.md (test: ask agent to "push to main" → should refuse)
- [ ] "Research Go vs Rust" → research skill is resolved and injected
- [ ] "Remember our staging port is 5433" → note-capture skill is resolved
- [ ] "What time is it?" → no skill matched, general mode
- [ ] Editing a SKILL.md file takes effect on the next message without restart
- [ ] Debug log shows skill resolution details for every message
- [ ] System prompt token count is logged per turn

---

## 9. Open Questions

| ID | Question | Default |
|----|----------|---------|
| TODO-S01 | Should we pre-fetch brain context relevant to the message before calling the LLM, or let the LLM decide to search? Pre-fetching adds latency but ensures context is available. Letting the LLM decide is more efficient but might miss relevant context. | Let the LLM decide (use tools). The AGENTS.md instructs it to search the brain proactively. If retrieval quality is poor, switch to pre-fetch in Phase 2. |
| TODO-S02 | Should identity files support variable substitution (e.g., `{{date}}`, `{{brain_stats}}`)? | Phase 2. Keep identity files static for hello world. |
| TODO-S03 | When a skill fails to load (bad YAML), should we alert the operator in chat or just log? | Log only. The operator is a developer — they'll check logs. Don't spam chat with system messages. |
| TODO-S04 | Should the skill resolver be a separate background process that maintains an index, or re-scan on every message? | Re-scan on every message. With < 20 skills, this is negligible. Build an index only if skill count exceeds 50. |
| TODO-S05 | Prompt caching strategy: cache just identity files, or identity + most-recently-used skill? | Cache identity files only (stable across turns). Skills change per turn so caching them is unreliable. Revisit when prompt caching costs are measurable. |
