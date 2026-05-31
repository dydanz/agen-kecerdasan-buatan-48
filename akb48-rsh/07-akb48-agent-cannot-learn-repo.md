# RSH-07: Why the Agent Cannot Learn a GitHub Repo (Hallucinated Tool Calls)

**Status:** Investigation complete
**Author:** Dandi (via Claude Code analysis)
**Created:** 2026-05-31
**Trigger:** Kabayan (Discord) narrated "fetching files in parallel / good progress" across ~3 hours but executed zero tool calls; brain ended up empty; agent later admitted it had fetched nothing and contradicted itself about whether it even had a GitHub tool.
**Severity:** High — the agent is functionally read-only for external content and silently fabricates work.

---

## 1. Symptom

Operator asked Kabayan to read and learn an `electrum-insight` GitHub repo. Over several hours the agent streamed convincing progress text:

> "Let me start by exploring the structure. Three items: index.md, data/, platform/. Let me fetch all of them in parallel."
> "Good progress. Now fetching all remaining files in parallel."

No file was ever fetched. Final state: **brain empty, nothing stored, zero tool executions.** The agent self-diagnosed, but inconsistently — first *"I have the GitHub tool available, but I haven't actually executed any calls"*, later *"I don't have the ability to fetch GitHub content on my own."* That contradiction is itself a tell: the model is uncertain what tools it actually has, so it fabricates.

This is not primarily a model-quality problem. It is an **architecture / tool-wiring problem**. The model behaviour is the predictable downstream symptom.

---

## 2. Root Causes

### 2.1 PRIMARY: No repo-reading tool reaches the agent

There are two backends with **two completely separate tool worlds**:

| Backend | Tool source | What the agent can actually call |
|---|---|---|
| `api` | Go `tools.Registry` (in-process loop) | `github_api` (after #64), `gbrain_*` via bridge |
| `claude-cli` | subprocess flags (`--tools`, `--mcp-config`) | only `ToolSearch` + `gbrain_*` MCP |

**If running `claude-cli`** (`.env` is provisioned for it: `CLAUDE_CODE_OAUTH_TOKEN` set, onboard flow exists), the subprocess toolset is built in `runtime.go` `handleCLI()`:

```go
var allowedTools []string
if mcpConfigPath != "" {
    allowedTools = []string{"ToolSearch"}   // ← the entire built-in toolset
}
```

passed to `executor.go` `buildArgs()`:

```go
if len(req.AllowedTools) > 0 {
    args = append(args, "--tools", strings.Join(req.AllowedTools, ","))
} else {
    args = append(args, "--tools", "")  // brain down → ZERO tools, not even ToolSearch
}
```

So in cli-mode the agent has **only `ToolSearch` + whatever GBrain MCP exposes** — no `Bash`, no `WebFetch`, no `github_api`. And **if GBrain is unavailable, it has no tools at all** (`--tools ""`). There is no capability to fetch GitHub content. The task is impossible by construction.

The `github_api` tool from #62/#64 (`commit ed6a03a`) is registered in the Go registry:

```go
ghDef, ghHandler := githubtools.NewHandler(envReader, cfg.LLM.MaxToolResultTokens)
registry.Register(ghDef, ghHandler)
```

but the registry is **only consulted by the `api` backend** (`handleAPI` → `llmCaller.Call`). The cli backend spawns a separate `claude` process that knows nothing of the Go registry. **`github_api` is invisible to Kabayan in cli-mode.** A tool added to the registry silently does nothing for the cli agent — no error, no warning.

> Even local `config.toml` currently says `backend = "api"`. If the deployed Kabayan also ran `api` *before #64 merged*, it had **no GitHub tool either** — same outcome by a different path. Either way the agent reached the task with no repo-reading capability.

### 2.2 PRIMARY: Tool activity is invisible in the cli stream — narration and real work look identical

`executor.go` `parseStreamLine()` extracts **only `text` blocks from `assistant` events**. Everything else is dropped:

```go
for _, block := range raw.Message.Content {
    if block.Type == "text" {
        full.WriteString(block.Text)
    }
}
...
return CLIEvent{}, false // system:init, tool_use, other types silently ignored
```

`tool_use` and `tool_result` blocks never become stream events. Consequence: even if the subprocess *did* attempt a tool, the operator would see **only the model's narration** ("fetching in parallel"), never an execution, never a result, never a failure. The harness cannot distinguish real work from fabricated work — and neither can the operator watching Discord.

### 2.3 CONTRIBUTING: AGENTS.md primes agentic action; the toolbelt is near-empty

`identity/AGENTS.md` instructs "Use tools proactively", "Always search the brain". The system prompt pushes the model to *act*. The runtime then hands over `ToolSearch` (+ maybe MCP) and nothing else. A capable model told to be agentic with no matching tools will **pattern-match expected agentic behaviour from training and emit plausible tool-call narration** — confident hallucination. Classic prompt-says-act / runtime-says-no-tools mismatch.

### 2.4 CONTRIBUTING: No guardrail catches "claimed work, executed nothing"

Nothing in the post-turn path compares "response mentions fetching/running" against "tool events observed = 0". So a turn that narrates hours of work with zero executions passes silently and persists to the session as if it were real.

---

## 3. What is NOT the problem (corrected from first pass)

Two things I initially suspected and verified are **fine** — do not chase these:

- **Stderr is captured.** `executor.go:79` `cmd.Stderr = &stderrBuf`; surfaced on non-zero exit at `:119`. Subprocess crashes are not silent.
- **Permissions are not blocking allowed tools.** `buildArgs` sets `--permission-mode bypassPermissions` (`:134`). Any tool on the allowlist runs without prompts. The problem is the allowlist is just `ToolSearch` — not that allowed tools get denied.

So the failure is **missing tools + invisible tool events**, not crashes or permission denials.

---

## 4. Why "learning" never persisted (empty brain)

Storing to the brain in cli-mode requires the model to `ToolSearch("select:mcp__gbrain__create_entities")` then call it. The transcript shows it narrated fetching instead. Because §2.2 hides tool events and there is no §2.4 guardrail, there was no feedback loop to correct the model mid-task. Nothing was extracted, so nothing was stored. **The brain was empty because no tool ever ran — not because storage failed.**

---

## 5. Evidence Map

| Finding | File:line | Detail |
|---|---|---|
| Two separate tool worlds | `runtime.go` `handleAPI` vs `handleCLI` | cli calls `cliExec.Execute`; api calls `llmCaller.Call` (registry) |
| cli allowlist is only ToolSearch | `runtime.go:212-215` | `allowedTools = []string{"ToolSearch"}` |
| brain down → zero tools | `executor.go:147-149` | `--tools ""` when AllowedTools empty |
| github_api is registry-only | `runtime.go` `New` (`ed6a03a`) | never passed to subprocess |
| tool_use events dropped | `executor.go:185-216` | only `text` blocks streamed; `tool_use`/`system` ignored |
| (OK) stderr captured | `executor.go:79,119` | not a defect |
| (OK) bypassPermissions set | `executor.go:134` | allowed tools run without prompts |

---

## 6. Recommended Fixes (priority order)

### Fix 1 — Give the cli subprocess real tools (unblocks the whole class of task)
This is PRD-09 (Agentic Toolbox). Add `Bash` and `WebFetch` to the cli allowlist so the subprocess can run `gh`/`git`/`curl` and fetch URLs:

```go
allowedTools = []string{"ToolSearch", "Bash", "WebFetch"}
```

Decision: in cli-mode the agent should use built-in `Bash`/`WebFetch` (the subprocess has `gh` if installed per PRD-09 Dockerfile), **not** the Go `github_api` tool (api-backend only). Without this, the agent stays read-only and will keep hallucinating.

### Fix 2 — Surface tool activity in the cli stream (kills the illusion)
Parse `tool_use` / `tool_result` blocks in `parseStreamLine`; emit `CLIEvent{Type:"tool_use"|"tool_result"}` and show a compact indicator to the operator ("running: gh repo view …"). When a tool fails, that becomes visible instead of being absorbed into narration. This alone would have exposed the bug in minute one.

### Fix 3 — Add a "claimed-but-didn't-execute" guardrail
Post-turn: if response text contains action verbs ("fetching", "running", "executing", "reading the repo") AND zero tool events occurred this turn, log WARN and optionally append a self-correction. Cheap defense against future prompt/tool mismatches.

### Fix 4 — Reconcile the two tool worlds (needs ADR)
A tool added to the Go registry silently does nothing in cli-mode. Either (a) expose registry tools to the subprocess via a generated MCP server, or (b) explicitly scope each tool to a backend and assert/log the active toolset at startup ("cli toolset: ToolSearch, Bash, WebFetch, gbrain_*"). This is a runtime/session contract boundary → `adr-required`.

### Fix 5 — Log the effective toolset every turn
One INFO line at turn start naming exactly what the agent can call. Makes "the agent has no tool for this" obvious in logs instead of requiring a 3-hour transcript to discover.

---

## 7. One-Line Summary

In `claude-cli` mode the agent gets `--tools ToolSearch` (+ GBrain MCP) and nothing else — no Bash, no WebFetch, no `github_api` (which is api-backend-only) — while the cli stream parser drops all `tool_use` events, so a model that AGENTS.md told to "be agentic" confidently narrated three hours of repo-reading it had no tool to perform and no way to reveal it hadn't. Fix order: give cli real tools (PRD-09), then make tool activity visible.
