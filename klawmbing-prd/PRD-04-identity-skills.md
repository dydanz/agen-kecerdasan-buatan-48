# PRD-04: Identity & Skill System

**Status:** Draft v2.0 (Go)
**Parent:** PRD-00 (Klawmbing Master PRD)
**Author:** Dandi
**Created:** April 26, 2026
**Revised:** May 1, 2026
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
| US-S04 | As an operator, I ask to "research Go vs Rust" and the research skill is invoked | Log shows "skill resolved: research". Response follows the research skill template. |
| US-S05 | As an operator, I ask "What's the weather like?" (no matching skill) and get a general response | Agent responds in general mode. Log shows "no skill matched, using general mode". |
| US-S06 | As a developer, I add a new skill file to `skills/` and it's immediately available | No restart required. Skill resolver re-reads SKILL.md from disk on each message. |
| US-S07 | As a developer, I can see which skill was resolved for any message | Debug log shows: `skill resolved: research path=skills/research/SKILL.md est_tokens=340` |

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

### 5.2 Skill File Format

Following the Agent Skills standard (agentskills.io). Directory layout:

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

Each SKILL.md has a YAML frontmatter block followed by a markdown body:

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

### 5.3 Skill Resolver

Package: `internal/skills`

```go
// SkillFrontmatter holds the parsed YAML header from a SKILL.md file.
// Fields map directly to YAML keys; unknown keys are silently ignored.
type SkillFrontmatter struct {
    Name        string   `yaml:"name"`
    Description string   `yaml:"description"`
    Triggers    []string `yaml:"triggers"`
}

// Skill is a fully-loaded skill ready for injection into the system prompt.
type Skill struct {
    Frontmatter SkillFrontmatter
    Body        string // markdown body after the closing --- of the frontmatter block
    EstTokens   int    // len(Body) / 4 — rough approximation
    FilePath    string // absolute path; used in debug logs
}

// SkillResolver resolves a user message to the best-matching Skill.
// It re-reads SKILL.md from disk on every call to Resolve — hot-reload
// with no file watcher required. The performance cost is negligible
// (a few KB of disk I/O per message).
type SkillResolver struct {
    skillsDir string // absolute path to the skills/ directory
}

// NewSkillResolver returns a SkillResolver rooted at skillsDir.
// It does not scan the directory at construction time — scanning happens
// on every Resolve call.
func NewSkillResolver(skillsDir string) *SkillResolver

// Resolve scans skillsDir for SKILL.md files, parses their frontmatter,
// matches triggers against the lowercased message, and returns the skill
// with the most specific (longest) matching trigger.
//
// Algorithm:
//  1. Walk skillsDir one level deep, collect paths matching */SKILL.md.
//  2. For each SKILL.md, call parseFrontmatter; skip the file on error (log warning).
//  3. Lowercase message. For each skill, iterate triggers; record
//     (skill, len(trigger)) for the first trigger that is a substring of message.
//  4. Among all recorded matches, return the skill with the longest trigger.
//     Ties broken alphabetically by skill name.
//  5. If the chosen skill's body is empty after parsing, log a warning and skip it.
//  6. Return nil if no skill matched (general mode).
//
// On every successful match, re-read the SKILL.md bytes and parse again so
// that edits made between messages are picked up automatically.
func (r *SkillResolver) Resolve(message string) (*Skill, error)

// List returns a summary of every parseable skill in skillsDir.
// Used by status commands and debug tooling.
func (r *SkillResolver) List() ([]SkillFrontmatter, error)
```

**parseFrontmatter (unexported helper):**

```go
// parseFrontmatter splits a SKILL.md file into its YAML frontmatter and
// markdown body. It expects the file to begin with "---\n", end the block
// with a second "---\n" or "---" at end-of-file, and treats everything
// after the closing delimiter as the body.
//
// Returns an error if:
//   - The opening "---" delimiter is missing
//   - The YAML block cannot be unmarshalled into SkillFrontmatter
//   - name or triggers fields are empty after parsing
func parseFrontmatter(data []byte) (SkillFrontmatter, string, error)
```

**Trigger matching — Go implementation:**

```go
func (r *SkillResolver) Resolve(message string) (*Skill, error) {
    entries, err := os.ReadDir(r.skillsDir)
    if err != nil {
        return nil, fmt.Errorf("reading skills dir: %w", err)
    }

    type candidate struct {
        skill       *Skill
        matchLength int
    }
    var best candidate

    lower := strings.ToLower(message)

    for _, entry := range entries {
        if !entry.IsDir() {
            continue
        }
        path := filepath.Join(r.skillsDir, entry.Name(), "SKILL.md")
        data, err := os.ReadFile(path)
        if err != nil {
            continue // file doesn't exist or unreadable; skip silently
        }

        fm, body, err := parseFrontmatter(data)
        if err != nil {
            slog.Warn("skipping skill: invalid frontmatter", "path", path, "err", err)
            continue
        }

        for _, trigger := range fm.Triggers {
            if strings.Contains(lower, strings.ToLower(trigger)) {
                if len(trigger) > best.matchLength ||
                    (len(trigger) == best.matchLength && fm.Name < best.skill.Frontmatter.Name) {
                    est := len(body) / 4
                    if est > 2000 {
                        slog.Warn("skill body exceeds recommended token limit",
                            "skill", fm.Name, "est_tokens", est)
                    }
                    best = candidate{
                        skill: &Skill{
                            Frontmatter: fm,
                            Body:        body,
                            EstTokens:   est,
                            FilePath:    path,
                        },
                        matchLength: len(trigger),
                    }
                }
                break // one trigger match per skill is sufficient
            }
        }
    }

    if best.skill == nil {
        slog.Debug("no skill matched, using general mode")
        return nil, nil
    }

    slog.Debug("skill resolved",
        "skill", best.skill.Frontmatter.Name,
        "path", best.skill.FilePath,
        "est_tokens", best.skill.EstTokens)
    return best.skill, nil
}
```

### 5.4 Context Assembler

Package: `internal/context` (or `internal/assembly`)

```go
// ContextAssembler builds the full input to the LLM for each turn.
// It layers: identity files (loaded once at startup) → resolved skill →
// session history → user message.
//
// Identity files are loaded via Load() and held in memory for the process
// lifetime. Skills are resolved fresh on every call to Build().
type ContextAssembler struct {
    agentsContent string        // contents of identity/AGENTS.md
    soulContent   string        // contents of identity/SOUL.md
    userContent   string        // contents of identity/USER.md (may be empty)
    resolver      *SkillResolver
}

// NewContextAssembler returns an unloaded assembler. Call Load() before Build().
func NewContextAssembler(identityDir string, resolver *SkillResolver) *ContextAssembler

// Load reads identity files from identityDir. Must be called once at startup.
//
// Rules:
//   - AGENTS.md: required. Returns error if missing or empty.
//   - SOUL.md:   required. Returns error if missing or empty.
//   - USER.md:   optional. Logs a warning if missing; sets userContent = "".
//
// Startup fails fast if Load() returns an error.
func (a *ContextAssembler) Load() error

// Build assembles the full message list for a single LLM call.
//
// Parameters:
//   - session: the current Session (provides turn history).
//   - userMessage: the raw text the operator just sent.
//   - coldContext: optional pre-fetched context string (brain summary,
//     session compaction summary, etc.). Pass "" if not applicable.
//
// Returns:
//   - systemPrompt: a single assembled string for anthropic.SystemParam.
//   - messages:     []anthropic.MessageParam representing the conversation
//                   (session history + the current user message).
//   - error: non-nil only if identity files were not loaded or the resolver fails.
//
// Prompt caching strategy (two tiers):
//   Tier 1 — System prompt (stable identity block):
//     The identity string (AGENTS.md + SOUL.md + USER.md) is identical
//     every turn. Mark the last TextBlockParam in Tier 1 with
//     CacheControl: &anthropic.CacheControlEphemeralParam{}
//     so Claude caches the prefix. If a skill was resolved, the skill body
//     is the last Tier-1 block and receives the cache marker; if no skill
//     matched, USER.md is the last block and receives it.
//
//   Tier 2 — Session history (stable recent history):
//     The assembler marks the last message of the stable history slice
//     (all turns except the current one) with cache_control so Claude
//     can cache the conversation context. Details of which message field
//     receives the marker are specified in PRD-05.
func (a *ContextAssembler) Build(
    session *Session,
    userMessage string,
    coldContext string,
) (systemPrompt string, messages []anthropic.MessageParam, err error)
```

**System prompt assembly — internal layout:**

The system prompt is a single `string` passed as `anthropic.SystemParam`. Sections are separated by `\n\n---\n\n`. Assembled in this order:

```
1. AGENTS.md content          — always
2. SOUL.md content            — always
3. USER.md content            — always (empty string if USER.md missing)
4. Active Skill section       — only if Resolve returned a match:
       "## Active Skill: {name}\n\n{body}"
5. Cold context section       — only if coldContext != "":
       "## Context\n\n{coldContext}"
```

The cache marker is placed on the last TextBlockParam of Tier 1:
- Skill matched: skill body block gets `CacheControl`.
- No skill: USER.md block gets `CacheControl`.

**Build — sketch:**

```go
func (a *ContextAssembler) Build(
    session *Session,
    userMessage string,
    coldContext string,
) (string, []anthropic.MessageParam, error) {
    if a.agentsContent == "" {
        return "", nil, errors.New("identity files not loaded; call Load() first")
    }

    // --- Tier 1: system prompt ---
    parts := []string{a.agentsContent, a.soulContent, a.userContent}
    cacheAnchor := 2 // index of the last required block (USER.md)

    skill, err := a.resolver.Resolve(userMessage)
    if err != nil {
        return "", nil, fmt.Errorf("skill resolver: %w", err)
    }
    if skill != nil {
        parts = append(parts, fmt.Sprintf("## Active Skill: %s\n\n%s",
            skill.Frontmatter.Name, skill.Body))
        cacheAnchor = len(parts) - 1
    }

    if coldContext != "" {
        parts = append(parts, "## Context\n\n"+coldContext)
    }

    // Build system prompt string; cache marker is tracked but applied at the
    // API call layer using the index of cacheAnchor in the TextBlockParam slice.
    _ = cacheAnchor // consumed by the LLM caller when constructing SystemParam
    systemPrompt := strings.Join(parts, "\n\n---\n\n")

    // --- Tier 2: message history + current turn ---
    messages := buildMessages(session, userMessage) // see PRD-05

    return systemPrompt, messages, nil
}
```

The LLM caller (PRD-01) is responsible for splitting `systemPrompt` into `[]anthropic.TextBlockParam` and applying `CacheControl` to the block at `cacheAnchor`. The assembler provides the index via a companion struct if needed, or the caller may re-split on `---` boundaries.

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

(Full content as shown in section 5.2 above)

---

## 6. Configuration

```toml
[skills]
skills_dir   = "skills"    # Relative to the runtime root (~/.klawmbing/)
identity_dir = "identity"  # Relative to the runtime root
```

`hot_reload` is not a config knob — the resolver always re-reads from disk on every message. This is the correct behavior for a solo-operator runtime with < 20 skills.

`max_skill_tokens = 2000` is a warning threshold, not a hard limit. Skills exceeding ~8000 chars (~2000 tokens) log a warning at load time but are still injected.

---

## 7. Validation & Error Handling

| Error | Handling | Logged message |
|-------|----------|----------------|
| AGENTS.md missing or empty | `Load()` returns error → startup fails fast | `"identity/AGENTS.md missing or empty"` |
| SOUL.md missing or empty | `Load()` returns error → startup fails fast | `"identity/SOUL.md missing or empty"` |
| USER.md missing | Warning, `userContent = ""`, runtime continues | `"identity/USER.md not found; operating without operator context"` |
| SKILL.md has invalid YAML frontmatter | Skip skill, log warning, continue | `"skipping skill: invalid frontmatter path=skills/X/SKILL.md err=..."` |
| SKILL.md body exceeds ~2000 tokens | Warn, still inject | `"skill body exceeds recommended token limit skill=X est_tokens=N"` |
| Multiple skills match with equal trigger length | Alphabetically first skill name wins | `"tie broken alphabetically skill=X over skill=Y"` |
| skills/ directory missing or empty | No skills loaded, general mode only | `"no skills found; operating in general mode"` |
| Skill file unreadable (permissions) | Skip silently during scan | `"skill file unreadable, skipping path=..."` |

---

## 8. Acceptance Criteria

- [ ] `ContextAssembler.Load()` fails fast with a clear error if AGENTS.md or SOUL.md is missing or empty
- [ ] USER.md missing produces a startup warning but does not prevent the runtime from starting
- [ ] Agent response tone matches SOUL.md (test: ask a casual question; verify no emojis, no exclamation marks, no "Sure!")
- [ ] Agent refuses to push to main when asked (AGENTS.md rule enforced by LLM)
- [ ] "Research Go vs Rust" → `skill resolved: research` in debug log; response follows research skill template
- [ ] "Remember our staging port is 5433" → `skill resolved: note-capture` in debug log
- [ ] "What time is it?" → `no skill matched, using general mode` in debug log
- [ ] Editing a SKILL.md and sending the next message picks up the edit without a process restart
- [ ] Debug log shows `skill`, `path`, and `est_tokens` for every resolved skill
- [ ] System prompt token estimate is logged per turn
- [ ] Prompt cache marker (`CacheControl`) is applied to the correct Tier-1 block (skill body if matched, USER.md if not)

---

## 9. Open Questions

| ID | Question | Default |
|----|----------|---------|
| TODO-S01 | Should we pre-fetch brain context relevant to the message before calling the LLM, or let the LLM decide to search? Pre-fetching adds latency but ensures context is available. Letting the LLM decide is more efficient but might miss relevant context. | Let the LLM decide (use tools). AGENTS.md instructs it to search the brain proactively. If retrieval quality is poor, switch to pre-fetch in Phase 2. |
| TODO-S02 | Should identity files support variable substitution (e.g., `{{date}}`, `{{brain_stats}}`)? | Phase 2. Keep identity files static for hello world. |
| TODO-S03 | When a skill fails to load (bad YAML), should we alert the operator in chat or just log? | Log only. The operator is a developer — they will check logs. Don't spam chat with system messages. |
| TODO-S04 | Should the skill resolver maintain an in-memory index, or re-scan on every message? | Re-scan every message. With < 20 skills this is negligible. Build an index only if skill count exceeds 50. |
| TODO-S05 | Prompt caching strategy: cache just identity files, or identity + most-recently-used skill? | Cache identity files only (stable across turns). Skills change per turn so caching them per-skill is unreliable. Revisit when prompt caching costs are measurable. |
| TODO-S06 | Should `ContextAssembler.Build` return a structured object (with `cacheAnchor` index) rather than a plain string, to give the LLM caller precise control over where to insert the cache marker? | Return a structured `AssembledContext` with `SystemBlocks []string` and `CacheAnchor int`. Decide at PRD-01 integration time. |
