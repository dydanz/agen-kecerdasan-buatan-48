# Phase 12: Agent Tool Reach & Visibility — Stop Hallucinated Tool Calls

**PRD:** `akb48-prd/PRD-12-agent-tool-visibility.md`
**Research:** `akb48-rsh/07-akb48-agent-cannot-learn-repo.md`
**Epic ticket:** KLW-039
**SDLC Class:** `class:high` — touches runtime/session tool contract; **`adr-required`**
**Paired with:** Phase 9. Phase 9 = capability (tools exist + allowlist). Phase 12 = visibility (surface what ran) + the ADR. **Sequencing: KLW-049/050 (visibility + logging) land first — zero behaviour change, immediately exposes the gap — then Phase 9 capability, then KLW-051 guardrail, then KLW-052 ADR.**

**Goal:** Make tool activity visible and guarantee the agent can never silently fabricate work. Parse `tool_use`/`tool_result` from the cli stream, surface a compact indicator, log the effective toolset each turn, and flag claimed-but-unexecuted action. Reconcile the two tool worlds via ADR.

**Definition of Done:**
- `tool_use`/`tool_result` parsed in `parseStreamLine`; operator sees a compact indicator (e.g. `⚙ gh repo view …`)
- tool failures are visible, not absorbed into narration
- one INFO line per turn names the effective toolset; ERROR if resolved toolset is empty
- post-turn guardrail logs WARN when a response claims action but zero tool events occurred
- ADR written deciding (a) registry→MCP unification vs (b) two documented surfaces, plus the cli write-scope gate
- `api` backend unchanged; `go test ./...` green

**Tickets:** KLW-049 → KLW-050 → KLW-051 → KLW-052

---

## KLW-049 — Parse + surface tool events (cli stream)

**Type:** Feature
**Effort:** 5 SP
**Labels:** `phase/12`, `type/feature`, `size/M`, `component/runtime`
**Dependencies:** none
**Branch:** `feature/KLW-049-tool-event-visibility`

### User Story
> As the operator, I want to see when the agent runs a tool (and when one fails), so narration can never be mistaken for real work.

### Implementation Plan
Per PRD-12 §5.2/§5.3:
1. **`internal/llm/claudecli/executor.go`** — extend `CLIEvent` with `ToolName string` and add event types `"tool_use"`, `"tool_result"`. In `parseStreamLine`, stop dropping non-text blocks: emit `tool_use` (tool name + short input summary) and `tool_result` (success/error) instead of the current `// silently ignored`.
2. **`internal/runtime/runtime.go` `handleCLI`** — on `tool_use`, push a compact line to `tokens` (`\n⚙ <ToolName>\n`); on `tool_result` error, surface it. Count tool events for KLW-051.

**Flag at review:** changes the cli stream event contract (`CLIEvent`) → ADR-relevant (KLW-052).

### Acceptance Criteria
- [ ] `tool_use`/`tool_result` blocks parsed (unit test on raw stream lines)
- [ ] operator sees a compact one-line indicator per tool call
- [ ] tool failure is visible (not swallowed)
- [ ] existing text-delta streaming unchanged (regression test)
- [ ] `go test ./internal/llm/claudecli/...` pass

---

## KLW-050 — Log effective toolset every turn

**Type:** Feature
**Effort:** 2 SP
**Labels:** `phase/12`, `type/feature`, `size/S`, `component/runtime`
**Dependencies:** none
**Branch:** `feature/KLW-050-toolset-log`

### User Story
> As the operator, I want one log line per turn naming exactly what the agent can call, so "no tool for this" is obvious without reading a 3-hour transcript.

### Implementation Plan
Per PRD-12 §5.5:
- At the start of `handleCLI` and `handleAPI`: `slog.Info("agent toolset", "backend", ..., "tools", names)`. cli: resolved allowlist + discovered `gbrain_*`. api: `registry.Definitions()` names.
- If the resolved cli toolset is empty → `slog.Error` (should be impossible after Phase 9 KLW-041; this catches regressions).

### Acceptance Criteria
- [ ] one INFO line per turn with backend + tool names
- [ ] empty cli toolset → ERROR log
- [ ] `go test ./internal/runtime/...` pass

---

## KLW-051 — Claimed-but-didn't-execute guardrail

**Type:** Feature
**Effort:** 3 SP
**Labels:** `phase/12`, `type/feature`, `size/M`, `component/session`, `component/runtime`
**Dependencies:** KLW-049
**Branch:** `feature/KLW-051-narration-guard`

### User Story
> As the operator, I want a warning when the agent's reply claims action but ran no tools, so silent fabrication is caught.

### Implementation Plan
Per PRD-12 §5.4:
1. Thread a `ToolEventCount int` into `SessionTurn` (incremented in cli/api loops as tool events occur).
2. **`internal/session/hooks.go`** — `NarrationGuardHook()`: if `turn.ToolEventCount == 0` and the response matches the action-verb regex (`fetch(ing)?|running|executing|reading the repo|cloning|downloading`), log WARN with `turn_id` + session. Turn still persists (no block).
3. Wire in `runtime.New()`.

### Acceptance Criteria
- [ ] guardrail trips: action verbs + zero tool events → WARN
- [ ] no trip: tool events present, OR no action verbs
- [ ] turn still persists when guardrail trips (non-blocking)
- [ ] `go test ./internal/session/...` pass (trip / no-trip cases)

---

## KLW-052 — ADR: two-tool-worlds + cli write-scope gate

**Type:** Docs (ADR)
**Effort:** 2 SP
**Labels:** `phase/12`, `type/docs`, `size/S`, `adr-required`, `component/runtime`
**Dependencies:** KLW-049, KLW-041 (Phase 9 allowlist)
**Branch:** `feature/KLW-052-tool-backends-adr`

### User Story
> As the operator, I want a recorded decision on how cli and api tool surfaces relate, and how cli-mode GitHub writes are scoped, so the design is intentional not accidental.

### Implementation Plan
Per PRD-12 §5.6:
1. **New** `akb48-adr/ADR-00X-tool-backends.md` from `0000-template.md`. Decide: (a) generate an MCP server from the Go registry (single surface) vs (b) keep two surfaces, assert+log per backend. Recommendation: (b) now, (a) if registry tools proliferate.
2. Same ADR resolves the **cli write-scope gap**: `github_api` enforces personal=write/office=read-only on api backend only; cli-mode `Bash`/`gh` has no gate. Decide: (i) scoped token in cli, (ii) `gh` policy shim, or (iii) accept personal-write + document. **Until decided, `OFFICE_GITHUB_TOKEN` stays unmounted in the cli container** (Phase 9 DoD).
3. Link the ADR from PRD-09 §9 and PRD-12 §5.6.

### Acceptance Criteria
- [ ] ADR created, status Accepted, linked from both PRDs
- [ ] tool-world decision recorded with rationale
- [ ] cli write-scope decision recorded; `.env` mount policy stated
- [ ] ADR bot satisfied (`adr-required` cleared within 48h of the interface-changing merge)

---

## Risk Register

| Risk | Likelihood | Mitigation |
|---|---|---|
| Stream-contract change breaks text streaming | Med | Regression test on text-delta path (KLW-049); api backend untouched |
| Guardrail false positives | Med | Narrow verb regex; WARN-only (non-blocking); tune in follow-up |
| Indicator spam on multi-tool turns | Low | One compact line per call; collapse if noisy |
| ADR stalls capability rollout | Low | Visibility (KLW-049/050) ships independently first |

## File Change Summary

| File | Change |
|---|---|
| `internal/llm/claudecli/executor.go` | `CLIEvent.ToolName`; parse `tool_use`/`tool_result` |
| `internal/runtime/runtime.go` | surface tool events; toolset log; wire guardrail; `ToolEventCount` |
| `internal/session/hooks.go` | `NarrationGuardHook` |
| `internal/types` / session turn | `ToolEventCount` field |
| `akb48-adr/ADR-00X-tool-backends.md` | new ADR |
| `*_test.go` | tool-event parsing, toolset log, guardrail tests |
