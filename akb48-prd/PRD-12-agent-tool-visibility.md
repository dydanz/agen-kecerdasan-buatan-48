# PRD-12: Agent Tool Reach & Visibility — Stop Hallucinated Tool Calls

**Status:** Draft v1.0
**Parent:** PRD-00 (AKB48 Master PRD), PRD-08 (Claude CLI Backend), PRD-09 (Agentic Toolbox)
**Author:** Dandi
**Created:** 2026-05-31
**Dependencies:** PRD-08 (cli executor, stream parser), PRD-09 (Bash/dev tools in container)
**Research:** `akb48-rsh/07-akb48-agent-cannot-learn-repo.md`
**Estimated Effort:** 3–4 days
**SDLC Class:** `class:high` — touches runtime/session tool contract; `adr-required`
**Ticket:** KLW-039

---

## 1. Problem

The agent narrated ~3 hours of "fetching files in parallel / good progress" while reading a GitHub repo, executed **zero** tool calls, and ended with an empty brain. It then contradicted itself about whether it even had a GitHub tool.

RSH-07 isolated three compounding defects:

1. **No repo-reading tool reaches the agent in `claude-cli` mode.** The cli subprocess gets `--tools ToolSearch` (+ GBrain MCP) and nothing else — no `Bash`, no `WebFetch`. The `github_api` tool (#64) lives only in the Go registry, which the cli backend never consults. If GBrain is down, the agent gets `--tools ""` — zero tools.
2. **Tool activity is invisible.** `executor.go parseStreamLine()` streams only `text` blocks; `tool_use` / `tool_result` are dropped. Real work and fabricated narration look identical on the wire.
3. **No guardrail** catches "claimed work, executed nothing", and **no log line** ever states the agent's effective toolset.

Net effect: a model told by AGENTS.md to "be agentic", handed a near-empty toolbelt, predictably hallucinates plausible tool-call narration — and nothing in the system reveals it.

---

## 2. Goals

- **G1:** (delivered by PRD-09; verified here) In `claude-cli` mode the agent has real action tools — `Bash`, `WebFetch`, file ops — plus `ToolSearch` and GBrain MCP
- **G2:** (delivered by PRD-09; verified here) GBrain being unavailable never reduces the agent to zero tools; built-in tools remain
- **G3:** `tool_use` and `tool_result` events are parsed and surfaced — operator sees what ran
- **G4:** A post-turn guardrail flags responses that claim action but executed no tools
- **G5:** Every turn logs the agent's effective toolset (one INFO line)
- **G6:** The two tool worlds (Go registry vs cli subprocess) are documented and reconciled via ADR
- **G7:** No regression to `api` backend behaviour

---

## 3. Non-Goals

- Installing dev tools in the Docker image — that is PRD-09 (this PRD assumes `gh`/`git`/`curl` present)
- Exposing the full Go registry to the subprocess via a generated MCP server — evaluated in the ADR, deferred unless chosen
- Rich tool-call UI in Discord (collapsible blocks etc.) — a compact one-line indicator is enough
- Changing GBrain MCP tool surface

---

## 4. User Stories

| ID | Story | Acceptance Criteria |
|----|-------|---------------------|
| US-T01 | As operator I ask the agent to read a GitHub repo and it actually fetches it | Agent runs `gh`/`git`/`curl` via `Bash` or fetches via `WebFetch`; content appears in the response |
| US-T02 | As operator I see when the agent runs a tool | Discord shows a compact indicator, e.g. `⚙ running: gh repo view org/repo`; failures shown, not hidden |
| US-T03 | As operator, GBrain is down but the agent can still act | Agent retains `Bash`/`WebFetch`/`ToolSearch`; logs note GBrain unavailable |
| US-T04 | As operator, the agent cannot silently fake work | If response claims action but zero tools ran, a WARN logs and the turn is flagged |
| US-T05 | As operator I can see the agent's toolset in logs | One INFO line per turn: `cli toolset: ToolSearch, Bash, WebFetch, gbrain_*` |
| US-T06 | As operator on `api` backend, nothing changes | All existing api-backend tests pass; behaviour identical |

---

## 5. Technical Design

### 5.1 cli allowlist — owned by PRD-09, consumed here

> **PRD-09 §4.2/§4.3/§6 owns the `AllowedTools` mechanism** (`allowed_tools` config + `defaultAllowedTools(mcpPresent)`). PRD-12 does **not** redefine the list — it relies on PRD-09's canonical toolset, which already includes `Bash`, `WebFetch`, `Read/Write/Glob/Grep`, and `ToolSearch` (when MCP present), and already keeps built-in tools when GBrain is down (no zero-tools cliff). G1/G2 are satisfied by PRD-09.

PRD-12's only stake in the allowlist: assert and **log** the effective toolset each turn (§5.5) so a missing/empty toolset is visible immediately instead of after a 3-hour transcript. If the resolved list is empty, log ERROR — that should never happen given PRD-09's default.

> Security note: `bypassPermissions` is already set (`executor.go:134`) and the subprocess runs with `cmd.Dir = os.TempDir()` and `ANTHROPIC_API_KEY` stripped. `Bash` access is operator-scoped (allowed-user gate at the adapter). Container sandboxing (PRD-09 / Phase 3) bounds blast radius. Document this in the ADR.

### 5.2 Parse and surface tool events (`executor.go`)

Extend `CLIEvent`:

```go
type CLIEvent struct {
    Type    string // "text" | "tool_use" | "tool_result" | "done" | "error"
    Content string
    ToolName string // for tool_use/tool_result
    CostUSD float64
    Err     error
}
```

In `parseStreamLine`, handle `tool_use` and `tool_result` content blocks (currently dropped at the `// silently ignored` line). Emit `tool_use` with the tool name and a short input summary; emit `tool_result` with success/error.

### 5.3 Render tool activity (`runtime.go handleCLI` + adapters)

In the cli event loop, on `tool_use` push a compact line to `tokens`:

```go
case "tool_use":
    tokens <- fmt.Sprintf("\n⚙ %s\n", evt.ToolName)
```

Keep it minimal — one line per call. Count tool events for the guardrail (§5.4).

### 5.4 Claimed-but-didn't-execute guardrail (post-turn hook)

```go
var actionVerbs = regexp.MustCompile(`(?i)\b(fetch(ing)?|running|executing|reading the repo|cloning|downloading)\b`)

func NarrationGuardHook() HookFunc {
    return func(sess *Session, turn SessionTurn) {
        if turn.ToolEventCount == 0 && actionVerbs.MatchString(turn.AssistantResponse) {
            slog.Warn("agent claimed action but executed no tools",
                "turn_id", turn.TurnID, "session", sess.ID)
        }
    }
}
```

Requires threading a `ToolEventCount` into `SessionTurn` (incremented in the cli/api loops). Phase 2: optionally append a self-correction note to the response.

### 5.5 Log effective toolset every turn

At the start of both `handleCLI` and `handleAPI`:

```go
slog.Info("agent toolset", "backend", r.cfg.LLM.Backend, "tools", effectiveToolNames)
```

cli: the `allowedTools` slice + discovered `gbrain_*` names. api: `registry.Definitions()` names.

### 5.6 Two-tool-worlds reconciliation (ADR)

Write `akb48-adr/ADR-00X-tool-backends.md` deciding between:
- **(a)** Generate an MCP server from the Go registry so cli and api share one tool surface (single source of truth, more wiring)
- **(b)** Keep two surfaces but assert+log them at startup, and document which tools exist per backend (less work, dual maintenance)

Recommendation in the ADR: **(b)** now, **(a)** if registry tools proliferate.

The same ADR resolves the **cli-mode write-scope gap** (PRD-09 open question): the `github_api` tool (#62/#64) enforces personal=write / office=read-only at the Go layer, but that gate exists only on the `api` backend. In cli-mode the agent reaches GitHub via `Bash`/`gh`/`curl` with `GITHUB_TOKEN` and has **no equivalent guard**. Decide: (i) inject only a scoped token in cli-mode, (ii) wrap `gh` with a policy shim, or (iii) accept full personal write in cli and document it. Until decided, do not mount `OFFICE_GITHUB_TOKEN` into the cli container.

---

## 6. Error Handling

| Condition | Behaviour |
|-----------|-----------|
| `Bash`/`WebFetch` tool errors in subprocess | `tool_result` event with error surfaced to operator; logged |
| GBrain down | `ToolSearch` omitted; `Bash`/`WebFetch` retained; INFO note |
| Guardrail trips | WARN log with turn_id; turn still persists (no block) |
| Stream parse of unknown block type | Ignored as today (forward-compatible) |

---

## 7. Acceptance Criteria

- [ ] cli-mode agent can fetch a GitHub repo via `Bash`/`WebFetch` end-to-end (US-T01) — requires PRD-09 allowlist + image
- [ ] `tool_use`/`tool_result` parsed in `parseStreamLine`; operator sees a compact indicator
- [ ] Tool failures are visible (not absorbed into narration)
- [ ] Post-turn guardrail logs WARN when response claims action with zero tool events
- [ ] One INFO line per turn names the effective toolset; ERROR if the resolved toolset is empty
- [ ] ADR written and linked deciding the tool-world reconciliation + cli write-scope gate
- [ ] `go test ./...` green; api backend unchanged
- [ ] New tests: `tool_use` parsing, guardrail trip/no-trip, empty-toolset ERROR log

> Allowlist-composition tests (brain up/down) belong to **PRD-09** (it owns `defaultAllowedTools`). PRD-12 only tests that the effective toolset is logged and non-empty.

---

## 8. Rollout

1. Land §5.2 + §5.5 first (visibility + logging) — zero behaviour change, immediately exposes the gap
2. Land PRD-09 (image + `defaultAllowedTools` allowlist) so the agent gains reach; PRD-12 §5.5 logging confirms it
3. Land §5.4 guardrail
4. ADR alongside PRD-09's allowlist change (interface-affecting → `adr-required`)

Visibility before capability: even before the agent can act, operators stop being lied to.
