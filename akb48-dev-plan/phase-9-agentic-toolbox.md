# Phase 9: Agentic Toolbox — Shell, HTTP, Dev Tools for the Agent

**PRD:** `akb48-prd/PRD-09-agentic-toolbox.md`
**Research:** `akb48-rsh/07-akb48-agent-cannot-learn-repo.md`
**Epic ticket:** KLW-036
**SDLC Class:** `class:medium`
**Paired with:** Phase 12 (Tool Reach & Visibility). Phase 9 = capability (tools in container + allowlist). Phase 12 = visibility (surface tool events). Land Phase 12 KLW-049/050 *before* Phase 9 KLW-041 so the new reach is observable on day one.

**Goal:** The cli-mode agent can execute shell commands, make HTTP calls, run `git`/`gh`/`curl`, and read/fetch content inside its container sandbox. Removes the "agent is read-only / hallucinates work" ceiling identified in RSH-07.

**Definition of Done:**
- `docker compose build akb48` succeeds with new apk packages
- `docker exec deploy-akb48-1 which curl git jq vim gh` all return paths
- cli-mode agent runs `curl -s https://api.github.com/zen` and returns the quote
- cli-mode agent lists `/app/skills/` via `Bash`/`Read`
- GBrain down → agent still has `Bash`/`WebFetch`/file tools (never zero tools)
- `allowed_tools` config overrides the compiled default; empty → default applies
- agent reads an env var's presence without echoing its value
- `OFFICE_GITHUB_TOKEN` is **not** mounted into the cli container (pending Phase 12 ADR write-scope decision)
- `go test ./...` passes; `api` backend unchanged
- CI green

**Tickets:** KLW-040 → KLW-041 → KLW-042 → KLW-043

---

## KLW-040 — Dockerfile: install dev tools

**Type:** Chore
**Effort:** 2 SP
**Labels:** `phase/9`, `type/chore`, `size/S`, `component/runtime`
**Dependencies:** none
**Branch:** `feature/KLW-040-toolbox-docker`

### User Story
> As the operator, I want `gh`, `git`, `curl`, `jq`, `vim`, `sed`, `gawk` present in the container so the agent's `Bash` calls have real tools to invoke.

### Implementation Plan
- **Modify** `Dockerfile` per PRD-09 §4.1. Add `curl git vim jq coreutils grep sed gawk openssh-client` to the `apk add` line. Add `github-cli` (Alpine package `github-cli`) so `gh` is available.
- Keep non-root `akb48` user; no `sudo`.
- Verify image size delta is acceptable (note in PR).

### Acceptance Criteria
- [ ] `docker compose build akb48` succeeds
- [ ] `docker exec deploy-akb48-1 which curl git jq vim gh sed awk` all return paths
- [ ] Container still runs as non-root `akb48`
- [ ] No new inbound ports

---

## KLW-041 — Allowlist mechanism: config + `defaultAllowedTools`

**Type:** Feature
**Effort:** 3 SP
**Labels:** `phase/9`, `type/feature`, `size/M`, `component/config`, `component/runtime`
**Dependencies:** KLW-040
**Branch:** `feature/KLW-041-toolbox-allowlist`
**⚠ Owns the `AllowedTools` surface — Phase 12 defers here. Do not edit the allowlist in Phase 12.**

### User Story
> As the operator, I want the cli agent's toolset to come from `allowed_tools` config with a safe compiled default, so I can tighten or widen it without a rebuild.

### Implementation Plan
Per PRD-09 §4.2/§4.3/§6:
1. **`internal/config/config.go`** — add `AllowedTools []string \`toml:"allowed_tools"\`` to `LLMConfig`.
2. **`internal/runtime/runtime.go`** — add `defaultAllowedTools(mcpPresent bool) []string` returning `["Bash","Read","Write","Glob","Grep","WebFetch"]` (+`"ToolSearch"` when `mcpPresent`). In `handleCLI`, resolve: `tools := cfg.LLM.AllowedTools; if len(tools)==0 { tools = defaultAllowedTools(mcpConfigPath != "") }`. Pass to `CLIRequest.AllowedTools`.
3. **`config.toml` + `deploy/config.docker.toml`** — add the `allowed_tools` line (commented default) per PRD-09 §4.3.

**Invariant:** GBrain down (`mcpConfigPath == ""`) must still yield the 6 built-in tools — never the empty `--tools ""` that caused RSH-07.

**Flag at review:** this changes runtime tool wiring → `adr-required` (ADR authored in Phase 12 KLW-052).

### Acceptance Criteria
- [ ] `defaultAllowedTools(true)` includes `ToolSearch`; `defaultAllowedTools(false)` does not, but still has all 6 built-ins
- [ ] `allowed_tools` in config overrides the default
- [ ] empty `allowed_tools` → default applies
- [ ] cli agent can run a `Bash` command end-to-end against the built image
- [ ] `go test ./internal/config/... ./internal/runtime/...` pass

---

## KLW-042 — AGENTS.md: shell-tool guidance

**Type:** Docs
**Effort:** 1 SP
**Labels:** `phase/9`, `type/docs`, `size/XS`, `component/identity`
**Dependencies:** KLW-041
**Branch:** `feature/KLW-042-toolbox-agents-md`

### User Story
> As the agent, I need explicit guidance that I have shell tools, when to use them, and the secret-handling rule, so I act instead of narrating.

### Implementation Plan
- **Modify** `identity/AGENTS.md` per PRD-09 §4.4: add a `## Shell Tools` section — Bash available for curl/git/jq/file inspection; env vars for credentials, never hardcode; prefer read-only, confirm destructive ops; working dir + writable paths.
- Add one line tying back to RSH-07: "If you cannot perform an action, say so plainly — never narrate work you did not do."

### Acceptance Criteria
- [ ] `## Shell Tools` section present
- [ ] secret-handling rule explicit (never echo `OFFICE_*` / token values)
- [ ] anti-hallucination line present
- [ ] identity files still load (`go test ./internal/identity/...`)

---

## KLW-043 — Tests + manual smoke

**Type:** Testing
**Effort:** 2 SP
**Labels:** `phase/9`, `type/test`, `size/S`, `component/runtime`
**Dependencies:** KLW-041
**Branch:** `feature/KLW-043-toolbox-tests`

### Implementation Plan
- Unit: `defaultAllowedTools` composition (brain up/down); config override precedence; resolved toolset is never empty.
- Manual smoke (post-build, documented in PR):
  - `@Kabayan run curl -s https://api.github.com/zen` → zen quote
  - `@Kabayan what files are in /app/skills/` → list
  - `@Kabayan does OFFICE_GITHUB_TOKEN exist?` → confirms presence, does NOT print value
  - GBrain stopped → agent still answers using `Bash`

### Acceptance Criteria
- [ ] allowlist-composition tests pass (this is Phase 9's ownership, not Phase 12)
- [ ] manual smoke checklist documented in PR with results
- [ ] `go test ./...` green; `go vet ./...` clean

---

## Risk Register

| Risk | Likelihood | Mitigation |
|---|---|---|
| `Bash` access widens blast radius | Med | Container sandbox, non-root, operator-only adapter gate, `OFFICE_*` not mounted until Phase 12 ADR |
| Agent echoes a secret | Med | AGENTS.md rule (KLW-042) + Phase 12 tool-event visibility surfaces it |
| `github-cli` Alpine package name drift | Low | Pin in Dockerfile; verify in KLW-040 AC |
| Image size growth | Low | Note delta in PR; acceptable for dev assistant |

## File Change Summary

| File | Change |
|---|---|
| `Dockerfile` | add dev tools + `gh` |
| `internal/config/config.go` | `AllowedTools` field |
| `internal/runtime/runtime.go` | `defaultAllowedTools()` + resolve in `handleCLI` |
| `config.toml`, `deploy/config.docker.toml` | `allowed_tools` line |
| `identity/AGENTS.md` | `## Shell Tools` section |
| `internal/runtime/runtime_test.go` | allowlist composition tests |
