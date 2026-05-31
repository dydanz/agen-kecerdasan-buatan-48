# PRD-10: Session Compaction — Memory Flush + History Summarisation

**Status:** Draft v1.0
**Parent:** PRD-00 (AKB48 Master PRD), PRD-05 (Session Management), PRD-03 (GBrain Integration)
**Author:** Dandi
**Created:** 2026-05-31
**Dependencies:** PRD-05 (Session JSONL, hooks), PRD-03 (GBrain MCP — entity storage)
**Estimated Effort:** 3–4 days
**SDLC Class:** `class:medium`
**Ticket:** KLW-037

---

## 1. Problem

Sessions grow unboundedly. `MaxTurnsInContext = 50` silently drops old turns from the LLM window — knowledge in those turns is permanently lost. The config already has `max_turns_before_compaction = 30` but no implementation exists.

Two consequences:
1. **Context loss:** once turns scroll out of the 50-turn window, the LLM has no memory of them
2. **File bloat:** JSONL files grow forever; sessions with thousands of turns risk hitting the 10 MB warn threshold

The hard rule in CLAUDE.md: **flush facts to GBrain before compacting**. Skipping this permanently destroys knowledge.

---

## 2. Goals

- **G1:** When a session reaches `max_turns_before_compaction` turns, extract facts to GBrain then replace old turns with a summary
- **G2:** Fact extraction runs via `claude-haiku` (12× cheaper than Sonnet) — extraction model already configured
- **G3:** Compaction runs as a post-turn hook — never blocks the response path
- **G4:** JSONL file records compaction events; existing turns are never deleted from disk (append-only preserved)
- **G5:** If GBrain is unavailable, compaction is skipped with a warning — no data loss
- **G6:** Compacted sessions load correctly on next startup

---

## 3. Non-Goals

- Full context summarisation with re-injection as a message (LangChain-style) — overkill for solo operator
- Compaction policy per session type (main vs group) — single policy for now
- Compaction during the response turn — always post-turn
- Deleting JSONL entries on disk — append-only is non-negotiable

---

## 4. User Stories

| ID | Story | Acceptance Criteria |
|----|-------|---------------------|
| US-K01 | After 30+ turns, old turns are summarised and facts stored in GBrain | Compaction hook fires; haiku extracts entities; GBrain confirms storage |
| US-K02 | Compaction failure (GBrain down) does not crash or corrupt the session | Hook logs warning; session continues in degraded mode; no JSONL corruption |
| US-K03 | Compacted session resumes correctly after restart | Session loads summary turn; LLM receives it as context; prior facts retrievable via GBrain tools |
| US-K04 | Compaction is visible in logs | `INFO session: compaction triggered`, entity count, haiku token usage logged |
| US-K05 | JSONL file records the compaction event | `{"type":"compaction","summary":"...","entity_count":N}` appended |

---

## 5. Technical Design

### 5.1 Compaction Trigger

In `session/hooks.go`, add `CompactionHook` — runs after every turn if `len(session.Turns) >= MaxTurnsBeforeCompaction`.

```go
func CompactionHook(mgr *SessionManager, brain BrainWriter, extractModel string) HookFunc {
    return func(sess *Session, turn SessionTurn) {
        cfg := mgr.cfg
        if cfg.MaxTurnsBeforeCompaction == 0 {
            return
        }
        if len(sess.Turns) < cfg.MaxTurnsBeforeCompaction {
            return
        }
        go runCompaction(sess, brain, extractModel, mgr)
    }
}
```

### 5.2 Compaction Flow

```
1. Take turns [0 .. N-MaxTurnsInContext] (the "old" turns outside the context window)
2. Build extraction prompt from old turns
3. Call claude-haiku → extract entities (person/project/decision/product)
4. Store entities in GBrain via gbrain_create_entities
5. Summarise old turns into one paragraph via claude-haiku
6. Append compaction record to JSONL:
   {"type":"compaction","ts":"...","summary":"...","entity_count":N,"turns_replaced":M}
7. Replace old turns in session.Turns with a single synthetic "summary" turn
8. Write updated in-memory session (JSONL stays append-only)
```

### 5.3 Extraction Prompt (haiku)

```
You are extracting facts from a conversation for long-term memory storage.
From the turns below, extract entities (people, projects, decisions, products, policies, infra).
For each entity: name, type, one-line observation. Return JSON array.
Only include facts that will be useful weeks from now. Skip small talk.

<turns>
{{old turns rendered as USER/ASSISTANT pairs}}
</turns>
```

### 5.4 Summary Prompt (haiku)

```
Summarise these conversation turns in 2–3 sentences for a technical operator.
Focus on decisions made, tasks completed, and open questions. Be specific.

<turns>{{old turns}}</turns>
```

### 5.5 BrainWriter Interface

```go
type BrainWriter interface {
    CreateEntities(ctx context.Context, entities []Entity) error
    Available() bool
}
```

`GBrainBridge` already satisfies this (wraps the MCP `gbrain_create_entities` tool).

### 5.6 Config

No new config keys. Existing:
```toml
[session]
max_turns_before_compaction = 30   # 0 = disabled
max_turns_in_context = 50
```

`max_turns_before_compaction = 0` disables compaction entirely (safe default for testing).

---

## 6. Data Format

### JSONL compaction record
```json
{
  "type": "compaction",
  "ts": "2026-05-31T10:00:00Z",
  "turns_replaced": 20,
  "entity_count": 7,
  "summary": "Operator debugged Discord adapter, added ChannelTyping loop fix, created github_api tool. Open: session compaction not yet implemented."
}
```

### In-memory after compaction
```
session.Turns = [
  {role:"summary", text:"<summary paragraph>", ts:"..."},  // synthetic
  <last MaxTurnsInContext turns as normal>
]
```

---

## 7. Error Handling

| Error | Behaviour |
|-------|-----------|
| GBrain unavailable | Skip entity extraction, log WARN, still summarise + replace turns |
| Haiku call fails | Skip compaction entirely, log WARN, session continues unchanged |
| JSONL write fails | Log ERROR, continue — in-memory compaction still applied |
| Compaction while compaction running | Skip (atomic flag per session) |

---

## 8. Acceptance Criteria

- [ ] Compaction fires after `max_turns_before_compaction` turns (configurable, 0 = disabled)
- [ ] Facts extracted to GBrain before any turn is removed from in-memory state
- [ ] GBrain unavailable → compaction skipped, session continues, no crash
- [ ] JSONL compaction record appended (never overwrites existing lines)
- [ ] Compacted session loads correctly on restart
- [ ] `go test ./internal/session/...` covers: trigger threshold, GBrain-down graceful skip, JSONL record format
- [ ] Haiku used for extraction and summary (not Sonnet)
