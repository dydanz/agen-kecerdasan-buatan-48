# Phase 2: Identity, Skills & Context Assembly

**Goal:** The agent has a defined personality, routes messages to skill files, and assembles prompts with 4-tier caching. Adding a new capability means writing a markdown file — no code changes.

**Definition of Done:**
- Agent responds with AKB48 personality (from SOUL.md) on "Who are you?"
- "remember that X" → `note-capture` skill injected in prompt
- "research Y" → `research` skill injected in prompt
- Prompt caching is active — Tier 1 (identity) and Tier 2 (session history) marked with `cache_control`
- First message of a session triggers cold opener brain search (when brain available)

**Tickets:** KLW-010 → KLW-011 → KLW-012 → KLW-013 → KLW-014 (KLW-012/013 can be parallel after KLW-011)

---

## KLW-010 — Skill Resolver

**Type:** Chore
**Owner:** Backend
**Effort:** 3 SP
**Labels:** `phase/2`, `type/chore`, `size/M`, `component/identity`
**Dependencies:** KLW-001 (config), `gopkg.in/yaml.v3`
**Branch:** `feat/skill-resolver`

### Description

Parse SKILL.md files' YAML frontmatter and match user intent to the most specific skill via keyword matching. Hot-reloads on every message call — no restart needed after editing a skill file.

### Implementation Plan

**Files to create:**
- `internal/skills/resolver.go`
- `internal/skills/resolver_test.go`

**Add dependency:** `go get gopkg.in/yaml.v3`

**Data structures:**

```go
package skills

import (
    "gopkg.in/yaml.v3"
    "os"
    "path/filepath"
    "strings"
)

type SkillFrontmatter struct {
    Name        string   `yaml:"name"`
    Description string   `yaml:"description"`
    Triggers    []string `yaml:"triggers"`
}

type Skill struct {
    Frontmatter SkillFrontmatter
    Body        string
    EstTokens   int // len(Body)/4
}

type SkillResolver struct {
    skillsDir string
}

func NewSkillResolver(dir string) *SkillResolver

// Resolve scans skillsDir for SKILL.md files on every call (hot-reload).
// Returns the skill with the longest matching trigger, or nil for general mode.
func (r *SkillResolver) Resolve(message string) (*Skill, error)
```

**Resolve implementation:**

```go
func (r *SkillResolver) Resolve(message string) (*Skill, error) {
    lower := strings.ToLower(message)

    entries, err := os.ReadDir(r.skillsDir)
    if err != nil {
        return nil, fmt.Errorf("read skills dir: %w", err)
    }

    var best *Skill
    bestLen := 0

    for _, entry := range entries {
        if !entry.IsDir() { continue }
        skillPath := filepath.Join(r.skillsDir, entry.Name(), "SKILL.md")
        data, err := os.ReadFile(skillPath)
        if err != nil { continue }

        skill, err := parseSkill(data)
        if err != nil {
            slog.Warn("Invalid SKILL.md", "path", skillPath, "error", err)
            continue
        }
        if skill.EstTokens > 8000 {
            slog.Warn("Skill body exceeds 2000 token estimate", "name", skill.Frontmatter.Name)
        }

        for _, trigger := range skill.Frontmatter.Triggers {
            if strings.Contains(lower, strings.ToLower(trigger)) && len(trigger) > bestLen {
                best = skill
                bestLen = len(trigger)
            }
        }
    }
    return best, nil
}
```

**parseSkill helper:**

```go
func parseSkill(data []byte) (*Skill, error) {
    // Split on "---" to separate frontmatter from body
    // First block between first and second "---" is YAML
    // Remainder is the body
    parts := strings.SplitN(string(data), "---", 3)
    if len(parts) < 3 {
        return nil, fmt.Errorf("missing frontmatter delimiters")
    }
    var fm SkillFrontmatter
    if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
        return nil, err
    }
    body := strings.TrimSpace(parts[2])
    return &Skill{
        Frontmatter: fm,
        Body:        body,
        EstTokens:   len(body) / 4,
    }, nil
}
```

### Acceptance Criteria

- [ ] "remember that X" → resolves to `note-capture` skill
- [ ] "research options for Y" → resolves to `research` skill
- [ ] "hello" → returns `nil` (general mode)
- [ ] Two overlapping triggers → longest trigger wins
- [ ] Corrupt YAML in one SKILL.md → that skill skipped with `slog.Warn`; others still load
- [ ] Edit SKILL.md while resolver is running → change takes effect on next `Resolve` call
- [ ] `go test ./internal/skills/...` passes

### Testing Plan

```go
func TestResolve_NoteCapture(t *testing.T) {
    // Create temp dir with note-capture/SKILL.md
    // triggers: ["remember", "note that"]
    r := NewSkillResolver(tmpDir)
    skill, _ := r.Resolve("remember that X")
    assert skill.Frontmatter.Name == "note-capture"
}

func TestResolve_LongestTriggerWins(t *testing.T) {
    // Skill A: triggers: ["look"]
    // Skill B: triggers: ["look into"]
    skill, _ := r.Resolve("look into this")
    assert skill == SkillB
}

func TestResolve_NoMatch(t *testing.T) {
    skill, err := r.Resolve("hello world")
    assert skill == nil && err == nil
}

func TestResolve_CorruptYAML_OtherSkillsLoad(t *testing.T) {
    // Two skills, one with bad YAML
    // Assert valid skill resolves, corrupt one produces slog.Warn
}

func TestResolve_HotReload(t *testing.T) {
    // Resolve once, modify trigger in SKILL.md, resolve again
    // Assert new trigger matches
}
```

---

## KLW-011 — Context Assembler (4-Tier Caching)

**Type:** Chore
**Owner:** Backend
**Effort:** 8 SP
**Labels:** `phase/2`, `type/chore`, `size/L`, `component/identity`
**Dependencies:** KLW-005 (session manager), KLW-010 (skill resolver)
**Branch:** `feat/context-assembler`

### Description

Assembles the full system prompt and message history for each LLM call using the 4-tier cache-first architecture. This replaces the Phase 1 stub assembler in the runtime.

**4-Tier Architecture:**
```
Tier 1 [CACHED] — Identity: AGENTS.md + SOUL.md + USER.md + Skill body
Tier 2 [CACHED] — Session history: all turns except last 2
Tier 3 [NO CACHE] — Cold opener context (injected once per session)
Tier 4 [NO CACHE] — Live turn: last 2 turns + current user message
```

**Cache savings at 30 turns:** ~85–90% reduction on Tier 1+2 tokens vs. uncached.

### Implementation Plan

**Files to create:**
- `internal/identity/assembler.go`
- `internal/identity/assembler_test.go`

**Struct and interface:**

```go
package identity

type ContextAssembler struct {
    agentsContent string
    soulContent   string
    userContent   string
    resolver      *skills.SkillResolver
    manager       *session.SessionManager
    cfg           config.IdentityConfig
}

func NewContextAssembler(cfg config.IdentityConfig, resolver *skills.SkillResolver, manager *session.SessionManager) *ContextAssembler

// Load reads identity files from disk. Called once at startup.
// Returns error if AGENTS.md or SOUL.md is missing.
func (a *ContextAssembler) Load() error

// Build assembles system prompt + messages for one LLM call.
// coldContext is injected as Tier 3 if non-empty.
func (a *ContextAssembler) Build(
    sess *session.Session,
    userMessage string,
    coldContext string,
) (systemPrompt string, messages []anthropic.MessageParam, err error)
```

**Load implementation:**

```go
func (a *ContextAssembler) Load() error {
    required := []struct{ path, field *string }{
        {filepath.Join(a.cfg.Dir, "AGENTS.md"), &a.agentsContent},
        {filepath.Join(a.cfg.Dir, "SOUL.md"),   &a.soulContent},
    }
    for _, r := range required {
        data, err := os.ReadFile(*r.path)
        if err != nil {
            return fmt.Errorf("required identity file missing: %w", err)
        }
        *r.field = string(data)
    }

    // USER.md is optional
    userData, err := os.ReadFile(filepath.Join(a.cfg.Dir, "USER.md"))
    if err != nil {
        slog.Warn("USER.md not found — operator context not loaded")
    } else {
        a.userContent = string(userData)
    }
    return nil
}
```

**Build implementation:**

```go
func (a *ContextAssembler) Build(sess *session.Session, userMessage, coldContext string) (string, []anthropic.MessageParam, error) {
    // --- Tier 1: System prompt (cached) ---
    parts := []string{a.agentsContent, a.soulContent}
    if a.userContent != "" {
        parts = append(parts, a.userContent)
    }
    skill, err := a.resolver.Resolve(userMessage)
    if err != nil {
        slog.Warn("Skill resolution error", "error", err)
    }
    if skill != nil {
        parts = append(parts, skill.Body)
    }
    systemPrompt := strings.Join(parts, "\n\n---\n\n")

    // --- Tier 2: Session history (cached) ---
    contextTurns := a.manager.GetContextTurns(sess)
    var allMsgs []anthropic.MessageParam

    // All turns except last 2 → Tier 2 (mark last one with cache_control)
    tier2Turns := contextTurns
    var tier4Turns []session.SessionTurn
    if len(contextTurns) > 2 {
        tier2Turns = contextTurns[:len(contextTurns)-2]
        tier4Turns = contextTurns[len(contextTurns)-2:]
    } else {
        tier2Turns = nil
        tier4Turns = contextTurns
    }

    // Convert Tier 2 turns to messages
    tier2Msgs := turnsToMessages(tier2Turns)
    // Mark last Tier 2 message with cache_control
    if len(tier2Msgs) > 0 {
        markCached(&tier2Msgs[len(tier2Msgs)-1])
    }
    allMsgs = append(allMsgs, tier2Msgs...)

    // --- Tier 3: Cold context (no cache) ---
    if coldContext != "" {
        allMsgs = append(allMsgs, anthropic.NewUserMessage(
            anthropic.NewTextBlock("[What I recall that may be relevant]\n"+coldContext),
        ))
        allMsgs = append(allMsgs, anthropic.NewAssistantMessage(
            anthropic.NewTextBlock("Understood. I have that context."),
        ))
    }

    // --- Tier 4: Recent turns + current message (no cache) ---
    allMsgs = append(allMsgs, turnsToMessages(tier4Turns)...)
    allMsgs = append(allMsgs, anthropic.NewUserMessage(anthropic.NewTextBlock(userMessage)))

    return systemPrompt, allMsgs, nil
}
```

**turnsToMessages helper** — converts `[]SessionTurn` to `[]anthropic.MessageParam`, preserving tool_use/tool_result blocks:

```go
func turnsToMessages(turns []session.SessionTurn) []anthropic.MessageParam {
    var msgs []anthropic.MessageParam
    for _, t := range turns {
        msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock(t.UserMessage)))
        if len(t.ToolCalls) > 0 {
            msgs = append(msgs, buildToolUseAssistantMsg(t)...)
        } else {
            msgs = append(msgs, anthropic.NewAssistantMessage(anthropic.NewTextBlock(t.AssistantResponse)))
        }
    }
    return msgs
}
```

**Wire into runtime** — in `NewRuntime`, replace `stubAssembler` with `*identity.ContextAssembler` after `Load()` succeeds.

### Acceptance Criteria

- [ ] `Load()` returns error if `AGENTS.md` missing; logs warning if `USER.md` missing
- [ ] `Build()` on 10-turn session: Tier 2 = 8 messages with cache_control marker on last; Tier 4 = last 4 messages (2 turns) + current
- [ ] Skill matched → skill body concatenated into system prompt
- [ ] Cold context provided → appears as first message pair before Tier 4
- [ ] System prompt contains AGENTS.md + SOUL.md content verbatim
- [ ] `go test ./internal/identity/...` passes

### Testing Plan

```go
func TestBuild_TierStructure(t *testing.T) {
    // 10-turn session, no cold context
    // Assert:
    // - messages[:16] are Tier 2 (8 turns × 2 msgs), last marked cache_control
    // - messages[16:20] are Tier 4 (last 2 turns × 2 msgs)
    // - messages[20] is current user message
}

func TestBuild_WithColdContext(t *testing.T) {
    // 0-turn session, cold context = "staging is ap-southeast-1"
    // Assert first 2 messages are cold context pair
    // Assert last message is current user message
}

func TestBuild_SkillInjected(t *testing.T) {
    // Message "remember that X"
    // Assert systemPrompt contains note-capture skill body
}

func TestLoad_MissingAgentsMD(t *testing.T) {
    // Don't create AGENTS.md
    // Assert Load() returns error
}

func TestLoad_MissingUserMD(t *testing.T) {
    // Don't create USER.md, create AGENTS.md and SOUL.md
    // Assert Load() returns nil (warning only)
}
```

---

## KLW-012 — Identity Files (AGENTS.md, SOUL.md, USER.md)

**Type:** Chore
**Owner:** Backend
**Effort:** 2 SP
**Labels:** `phase/2`, `type/chore`, `size/S`, `component/identity`
**Dependencies:** KLW-011
**Branch:** `feat/identity-files`

### Description

Write the actual content of the three identity files. These define the agent's operational rules, personality, and operator context. They are loaded once at startup by the `ContextAssembler`.

### Implementation Plan

**Files to create:**
- `identity/AGENTS.md`
- `identity/SOUL.md`
- `identity/USER.md`

**AGENTS.md — Operational Rules:**

```markdown
# Operational Rules

## Brain Usage
- Always search the brain before answering questions about people, projects, or decisions.
- When storing a fact, extract: type (person/project/decision/product/policy), title, body (2–3 sentences with context and rationale), tags, scope (org).
- Proactively suggest storing facts when the operator shares important information.

## Communication
- Be concise. The operator reads on mobile.
- Lead with the answer. Context follows.
- Short paragraphs over long ones. Avoid bullet lists unless comparing options.
- Never start a message with "Sure!", "Of course!", "Certainly!", or similar filler.
- Never use emojis.

## Safety
- Never perform destructive operations (delete, drop, rm -rf) without explicit operator confirmation.
- Never push to main directly — always open a PR.
- Never deploy to production without explicit approval in chat.
- Never include secrets, API keys, or credentials in chat responses.
- Always confirm before executing any irreversible action.

## Accuracy
- Never fabricate facts, names, dates, or citations.
- If you don't know something, say so and search the brain or web.
- Always cite sources when presenting research.
```

**SOUL.md — Personality:**

```markdown
# Personality

You are a sharp, opinionated technical co-founder.

**Thinking style:** Systems-first. You see the architecture behind every problem. You default to simplicity and fight against unnecessary complexity. You push back on over-engineering respectfully but directly.

**Voice:** Professional, concise, informal — like a senior engineer on Slack. Short sentences. Direct statements. No hedging. You are a thinking partner, not a yes-machine.

**Memory:** You reference what you know naturally, without announcing that you're "recalling from memory." You treat the brain as your own knowledge, not an external database.

**Opinions:** You have them. When asked to compare options, you give a recommendation with clear reasoning, not just a list of trade-offs. You say "I'd go with X because Y" not "both have merit."

**What you are not:** A corporate assistant. Not a customer service bot. Not deferential. You respect the operator's autonomy and decisions but you engage as a peer.
```

**USER.md — Operator Context:**

```markdown
# Operator Profile

**Name:** Dandi
**Role:** Solo CTO/CEO
**Location:** UTC+7
**Background:** Backend engineer (Go, Python), 10+ years experience

## Working Style
- Primary interface: Telegram (mobile-first)
- Preference: concise, direct answers. No padding.
- Values: ownership, compounding systems, minimal operational overhead
- Decision style: data-informed, bias toward action

## Technical Context
- Primary languages: Go, Python
- Infra: VPS (Hetzner), Docker, Tailscale
- Database: PostgreSQL + pgvector (via GBrain)
- Cloud: self-hosted first, cloud when unavoidable

## Current Focus
- Building AKB48 as personal AI infrastructure
- Goal: reduce cognitive overhead of solo operator role
- Key projects: AKB48 runtime, GBrain integration
```

### Acceptance Criteria

- [ ] `ContextAssembler.Load()` succeeds with these files
- [ ] Agent asked "Who are you?" → personality consistent with SOUL.md (no emojis, direct, concise)
- [ ] Agent asked "What's my API key?" → refuses per AGENTS.md safety rule
- [ ] Agent asked a question about a person → searches brain before answering (AGENTS.md brain rule)
- [ ] USER.md contains accurate operator context

### Testing Plan

```bash
# Manual test after Phase 2 wiring
./akb48
> Who are you?
# Expect: direct intro referencing role as technical co-founder, no emojis
> What is my Anthropic API key?
# Expect: refusal ("I don't share secrets in chat")
```

---

## KLW-013 — Skill Files (note-capture, research)

**Type:** User Story
**Owner:** Backend
**Effort:** 2 SP
**Labels:** `phase/2`, `type/user-story`, `size/S`, `component/identity`
**Dependencies:** KLW-010
**Branch:** `feat/skill-files`

### User Stories

> As an operator, I want to say "remember that X" and have the agent store it as a structured entity in GBrain, so knowledge persists across sessions.

> As an operator, I want to say "research Y" and have the agent run a structured research process, so I get actionable findings not just a web summary.

### Implementation Plan

**Files to create:**
- `skills/note-capture/SKILL.md`
- `skills/research/SKILL.md`

**skills/note-capture/SKILL.md:**

```markdown
---
name: note-capture
description: >
  Capture and store facts, decisions, and context into the knowledge brain.
  Use when the operator explicitly asks to remember, note, or save something.
triggers:
  - remember
  - note that
  - store this
  - save this
  - don't forget
  - keep in mind
  - add to brain
---

## Note Capture Process

When the operator asks you to remember something:

1. **Identify the entity type** — classify as one of:
   - `person` — a team member, contact, or individual
   - `project` — an active or past project with status/stack
   - `decision` — an architectural, product, or business decision with rationale
   - `product` — a product you build or operate
   - `policy` — a standing rule or constraint

2. **Extract structured entity:**
   - `title`: one sentence, factual
   - `body`: 2–3 sentences with full context and rationale
   - `tags`: 1–3 relevant domain tags (e.g., `["infra", "klawmbing"]`)
   - `scope`: `org` (default for all operator notes)

3. **Call `gbrain_put`** with the structured entity.

4. **Confirm** with a brief response: "Stored. [one-line summary of what was saved]."

**Example:**
Operator: "Remember that we chose PostgreSQL + pgvector over Pinecone because we own the data."
→ Entity: type=decision, title="Chose PostgreSQL + pgvector over Pinecone", body="Selected for data ownership and cost. GBrain handles all indexing. Hybrid search: vector + keyword + graph.", tags=["infra", "database"]
→ Confirm: "Stored. Decision: PostgreSQL + pgvector for brain storage (data ownership, GBrain integration)."
```

**skills/research/SKILL.md:**

```markdown
---
name: research
description: >
  Research a technical, product, or business topic. Use when the operator
  asks to compare options, investigate alternatives, or analyze a subject.
triggers:
  - research
  - compare
  - investigate
  - analyze
  - what are the options
  - pros and cons
  - look into
  - evaluate
---

## Research Process

1. **Search brain first** — call `gbrain_search` to surface relevant prior context, decisions, and notes. Reference what's already known.

2. **Identify gaps** — what does the brain not cover? What's missing for a good recommendation?

3. **Enumerate 2–4 options** — for each option:
   - What it is (1 sentence)
   - Key trade-offs (2–3 bullets)
   - Best suited for (1 sentence)

4. **Recommend** — give a clear recommendation with rationale. "I'd go with X because Y." Not just "both have merit."

5. **Store findings** — call `gbrain_put` to store the research summary as a `decision` entity so future sessions benefit.

## Output Format

- Lead with the recommendation (bottom-line-up-front)
- Options table or short section per option
- Close with: "Stored research summary in brain."

Keep total response under 300 words unless the operator asks for more detail.
```

### Acceptance Criteria

- [ ] `SkillResolver.Resolve("remember that X")` → `note-capture` skill
- [ ] `SkillResolver.Resolve("research options for message queuing")` → `research` skill
- [ ] Both SKILL.md files parse without YAML errors
- [ ] Both skill bodies are under 2000 tokens (8000 chars)
- [ ] `note-capture` body instructs structured entity extraction before `gbrain_put`
- [ ] `research` body instructs brain search first, recommendation, and storage

### Testing Plan

```go
func TestSkillFiles_Parse(t *testing.T) {
    for _, name := range []string{"note-capture", "research"} {
        data, _ := os.ReadFile(fmt.Sprintf("../../skills/%s/SKILL.md", name))
        skill, err := parseSkill(data)
        assert err == nil
        assert len(skill.Frontmatter.Triggers) > 0
        assert skill.EstTokens < 2000
    }
}
```

---

## KLW-014 — Cold Session Opener

**Type:** User Story
**Owner:** Backend
**Effort:** 3 SP
**Labels:** `phase/2`, `type/user-story`, `size/M`, `component/identity`
**Dependencies:** KLW-007 (runtime), KLW-011 (assembler), KLW-016 (brain bridge — can stub for unit tests)
**Branch:** `feat/cold-opener`

### User Story

> As an operator, I want the agent to automatically recall relevant context when I start or resume a conversation, so it doesn't start cold and I don't have to re-explain background.

### Implementation Plan

**Files to create:**
- `internal/session/cold.go`
- `internal/session/cold_test.go`

**ColdOpener:**

```go
package session

type BrainSearcher interface {
    SearchEntities(ctx context.Context, query string, limit int) (string, error)
}

type ColdOpener struct {
    searcher  BrainSearcher
    threshold time.Duration
}

func NewColdOpener(searcher BrainSearcher, thresholdMinutes int) *ColdOpener

// BuildColdContext returns formatted recall context for a cold session.
// Returns "" if session is not cold or brain is unavailable.
func (c *ColdOpener) BuildColdContext(ctx context.Context, sess *Session, message string) string
```

**BuildColdContext logic:**

```go
func (c *ColdOpener) BuildColdContext(ctx context.Context, sess *Session, message string) string {
    if !sess.IsCold(c.threshold) {
        return ""
    }

    query := extractQuery(message) // first 10–15 words, stop words stripped
    results, err := c.searcher.SearchEntities(ctx, query, 3)
    if err != nil || results == "" {
        slog.Debug("Cold opener: brain search returned nothing", "query", query)
        return ""
    }
    return results
}
```

**extractQuery — strip stop words:**

```go
var stopWords = map[string]bool{
    "the": true, "a": true, "an": true, "is": true, "are": true,
    "what": true, "how": true, "can": true, "i": true, "do": true,
    "my": true, "me": true, "we": true, "our": true, "it": true,
    "in": true, "on": true, "at": true, "for": true, "to": true,
}

func extractQuery(message string) string {
    words := strings.Fields(strings.ToLower(message))
    var filtered []string
    for _, w := range words {
        if !stopWords[w] && len(filtered) < 12 {
            filtered = append(filtered, w)
        }
    }
    return strings.Join(filtered, " ")
}
```

**Wire into AKB48Runtime.HandleMessage:**

```go
// In HandleMessage, before Build():
coldContext := ""
if r.coldOpener != nil {
    coldContext = r.coldOpener.BuildColdContext(ctx, sess, msg.Text)
}
systemPrompt, messages, err := r.assembler.Build(sess, msg.Text, coldContext)
```

**BrainSearcher implementation** — `GBrainBridge.SearchEntities` (Phase 3). For Phase 2 testing, use a stub.

**Output format** (returned by `SearchEntities`):
```
- Dandi decided to use PostgreSQL + pgvector for brain storage (decision, 2025-03-15)
- GBrain serve must run before klawmbing starts (project:klawmbing)
- Startup: gbrain serve → ./akb48 (project:klawmbing)
```

### Acceptance Criteria

- [ ] New session → `IsCold(30min) == true` → `BuildColdContext` calls `SearchEntities`
- [ ] Resume after 31-minute gap → `BuildColdContext` calls `SearchEntities`
- [ ] Second message within 5 minutes → `IsCold == false` → `BuildColdContext` returns ""
- [ ] Brain unavailable (error from searcher) → returns "" silently; no error to user
- [ ] `extractQuery("What do you know about our staging cluster?")` → `"know staging cluster"` (stop words removed)
- [ ] `go test ./internal/session/... -run TestCold` passes

### Testing Plan

```go
func TestBuildColdContext_ColdSession(t *testing.T) {
    mockSearcher := &mockBrainSearcher{result: "- staging is ap-southeast-1 (decision)"}
    opener := NewColdOpener(mockSearcher, 30)
    sess := NewSession("test")
    result := opener.BuildColdContext(ctx, sess, "what do you know about staging?")
    assert result == "- staging is ap-southeast-1 (decision)"
}

func TestBuildColdContext_WarmSession(t *testing.T) {
    // Session with recent turn (< 30 min)
    // Assert BuildColdContext returns ""
    // Assert mockSearcher.SearchEntities NOT called
}

func TestBuildColdContext_BrainError(t *testing.T) {
    mockSearcher := &mockBrainSearcher{err: errors.New("disconnected")}
    result := opener.BuildColdContext(ctx, sess, "hello")
    assert result == "" // no panic, no error propagated
}

func TestExtractQuery(t *testing.T) {
    cases := []struct{ input, want string }{
        {"What do you know about our staging cluster?", "know staging cluster"},
        {"remember that we chose postgres", "remember chose postgres"},
    }
    for _, c := range cases {
        assert extractQuery(c.input) == c.want
    }
}
```
