# Phase 10: Session Compaction — Memory Flush + History Summarisation

**PRD:** `akb48-prd/PRD-10-session-compaction.md`
**Epic ticket:** KLW-037
**SDLC Class:** `class:medium`
**Depends on:** Phase 3 (GBrain bridge), Phase 5 (session JSONL + hooks). Brain backend is `mcp-server-memory` (KLW-035 / #59).

**Goal:** When a session exceeds `max_turns_before_compaction`, extract facts to the brain (via haiku) before old turns leave the context window, summarise them, and append a compaction record to JSONL. Append-only preserved; GBrain-down degrades safely.

**Definition of Done:**
- Compaction fires at `max_turns_before_compaction` (0 = disabled, the safe default)
- Facts extracted to brain **before** any turn is dropped from in-memory state
- Extraction + summary use `extraction_model` (haiku), never Sonnet
- GBrain unavailable → compaction skipped (summary still optional), session continues, no crash, no data loss
- JSONL compaction record appended; existing lines never rewritten
- Compacted session loads correctly on restart
- Compaction runs in a post-turn hook — never blocks the response
- `go test ./internal/session/...` covers trigger, graceful skip, record format, reload
- `go test ./...` green

**Tickets:** KLW-044 → KLW-045 → KLW-046

---

## KLW-044 — BrainWriter interface + haiku fact extraction

**Type:** Feature
**Effort:** 3 SP
**Labels:** `phase/10`, `type/feature`, `size/M`, `component/brain`, `component/session`
**Dependencies:** none
**Branch:** `feature/KLW-044-compaction-extract`

### User Story
> As the runtime, I need to turn a block of old turns into stored brain entities via a cheap model, so knowledge survives before turns scroll out of context.

### Implementation Plan
Per PRD-10 §5.3/§5.5:
1. Define `BrainWriter` interface (`CreateEntities(ctx, []Entity) error`, `Available() bool`) in `internal/session` (or `internal/brain` with a session-side alias). Confirm `GBrainBridge` satisfies it — it already wraps the `create_entities` MCP tool; add a thin `CreateEntities` method if absent.
2. Add an extractor that renders old turns into the PRD-10 §5.3 prompt and calls the haiku model (`cfg.LLM.ExtractionModel`). In `api` backend use the existing LLM caller; in `cli` backend use `claudecli.CLILLMClient` (single-shot). Parse the JSON entity array; tolerate malformed output (skip, log WARN).

**Flag at review:** touches brain MCP tool usage + introduces a session→brain dependency edge.

### Acceptance Criteria
- [ ] `BrainWriter` defined; `GBrainBridge` satisfies it
- [ ] extractor calls haiku (assert model name in a test via a stub caller)
- [ ] malformed extraction output → no panic, WARN logged, empty entity set
- [ ] `go test ./internal/session/... ./internal/brain/...` pass

---

## KLW-045 — Compaction flow + post-turn hook

**Type:** Feature
**Effort:** 5 SP
**Labels:** `phase/10`, `type/feature`, `size/M`, `component/session`
**Dependencies:** KLW-044
**Branch:** `feature/KLW-045-compaction-hook`

### User Story
> As the operator, I want long sessions to compact automatically without losing knowledge or blocking my replies.

### Implementation Plan
Per PRD-10 §5.1/§5.2:
1. **`internal/session/hooks.go`** — add `CompactionHook(mgr, brain BrainWriter, extractModel string) HookFunc`. Fires only when `cfg.MaxTurnsBeforeCompaction > 0 && len(sess.Turns) >= cfg.MaxTurnsBeforeCompaction`. Runs `runCompaction` in a goroutine (post-turn, non-blocking). Per-session atomic flag prevents concurrent compaction.
2. **`runCompaction`** — select old turns (`[0 .. N-MaxTurnsInContext]`); extract entities (KLW-044) → `brain.CreateEntities` (skip if `!brain.Available()`); summarise old turns via haiku; replace old turns in-memory with one synthetic `summary` turn; keep last `MaxTurnsInContext` turns.
3. **Wire** the hook in `runtime.New()` alongside `PersistSessionHook` / `LogMetricsHook`.

**Invariants:**
- extraction completes (or is intentionally skipped on brain-down) **before** in-memory turns are dropped
- hook panics are recovered (existing hook contract) — never break the main loop

### Acceptance Criteria
- [ ] hook fires at threshold; no-op when `MaxTurnsBeforeCompaction == 0`
- [ ] brain-down → entities skipped, session continues, in-memory still compacted (or fully skipped per §7 policy), no crash
- [ ] never blocks the response path (runs in goroutine)
- [ ] concurrent-compaction guard works (no double run)
- [ ] `go test ./internal/session/...` pass

---

## KLW-046 — JSONL compaction record + reload + tests

**Type:** Feature
**Effort:** 3 SP
**Labels:** `phase/10`, `type/feature`, `size/M`, `component/session`
**Dependencies:** KLW-045
**Branch:** `feature/KLW-046-compaction-jsonl`

### User Story
> As the operator, I want compaction events recorded append-only and reloaded correctly, so disk history is intact and restarts are safe.

### Implementation Plan
Per PRD-10 §6:
1. Append `{"type":"compaction","ts","turns_replaced","entity_count","summary"}` to the session JSONL (append-only; never rewrite). Extend the session line decoder to recognise `type:"compaction"` and reconstruct the synthetic summary turn on load.
2. On startup load, a compacted session yields: `[summary turn] + last N turns`.
3. Warn (existing 10MB threshold) still applies.

### Acceptance Criteria
- [ ] compaction record appended, never overwrites prior lines
- [ ] record matches PRD-10 §6 schema
- [ ] compacted session reloads correctly after restart (round-trip test)
- [ ] `INFO session: compaction triggered` with entity count + haiku token usage logged
- [ ] `go test ./internal/session/...` green; `go test ./...` green

---

## Risk Register

| Risk | Likelihood | Mitigation |
|---|---|---|
| Knowledge lost if extraction skipped on brain-down | Med | Policy in §7: prefer skip-entire-compaction on brain-down so turns stay until brain returns |
| Haiku extraction hallucinates entities | Med | Prompt scopes to durable facts; entities are additive, dedupe in brain |
| Compaction races with persistence hook | Low | Per-session atomic flag; both are post-turn goroutines under errgroup |
| Reload of mixed turn/compaction lines | Low | Decoder switch on `type`; round-trip test (KLW-046) |

## File Change Summary

| File | Change |
|---|---|
| `internal/session/hooks.go` | `CompactionHook` + `runCompaction` |
| `internal/session/manager.go` | compaction record append + reload decode |
| `internal/brain/bridge.go` | `CreateEntities` (if missing) to satisfy `BrainWriter` |
| `internal/runtime/runtime.go` | wire `CompactionHook` |
| `internal/session/*_test.go` | trigger, skip, record, reload tests |
