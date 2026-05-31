# PRD-09: Agentic Toolbox — Shell, HTTP, and Dev Tools for the Agent

**Status:** Draft v1.0  
**Parent:** PRD-00 (AKB48 Master PRD), PRD-08 (Claude CLI Backend)  
**Author:** Dandi  
**Created:** 2026-05-30  
**Dependencies:** PRD-08 (Claude CLI backend must be running)  
**Paired with:** PRD-12 (Agent Tool Reach & Visibility) — see §9 for the boundary  
**Estimated Effort:** 2–3 days  
**SDLC Class:** `class:medium`  
**Ticket:** KLW-036 (to be filed)

---

## 1. Problem

The agent currently runs with `AllowedTools: ["ToolSearch"]` — the Claude subprocess can only load MCP tool schemas. It cannot execute shell commands, make HTTP calls, read files, or interact with external services like GitHub.

This means the agent cannot:
- Call GitHub API to create issues, read PRs, or check CI status
- Run `curl` to fetch external data
- Execute `git` commands (clone, diff, log)
- Read or write files inside the container
- Perform any agentic action beyond answering questions

The agent is effectively read-only. For a software development assistant, this is a hard ceiling.

---

## 2. Goals

- Enable the agent to execute shell commands inside the container sandbox
- Install essential dev tools in the Docker image (`curl`, `git`, `vim`, `jq`, `awk`, `sed`)
- Expose `Bash` tool to the Claude subprocess alongside `ToolSearch`
- Keep execution sandboxed to the container — no host filesystem access
- Remain secure: operator-only access, no arbitrary code from untrusted users

---

## 3. Non-Goals

- Code execution sandbox with resource limits (Phase 3)
- Multi-user sandboxing — this is a solo operator runtime
- Installing language runtimes (Python, Node beyond what Claude Code needs) — out of scope for this PRD
- Persistent tool state across sessions

---

## 4. Proposed Changes

### 4.1 Dockerfile — add dev tools

```dockerfile
RUN apk --no-cache add \
    ca-certificates tzdata nodejs npm \
    curl git vim jq \
    coreutils grep sed gawk \
    openssh-client && \
    npm install -g @anthropic-ai/claude-code @modelcontextprotocol/server-memory && \
    addgroup -S akb48 && adduser -S akb48 -G akb48
```

| Tool | Purpose |
|------|---------|
| `curl` | HTTP API calls (GitHub, Slack, webhooks) |
| `git` | Clone repos, diff, log — read-only ops |
| `vim` | In-container file editing (debug sessions) |
| `jq` | Parse JSON responses from API calls |
| `sed` / `gawk` | Text processing in shell pipelines |
| `openssh-client` | `ssh-keyscan`, key handling for git over SSH |

### 4.2 runtime.go — expand AllowedTools

> **PRD-09 owns the allowlist** (this section + §4.3 + §6). PRD-12 consumes the result and never redefines it. The canonical default below is the single source — keep PRD-12 §5.1 pointing here.

```go
// defaultAllowedTools — canonical cli-mode toolset. ToolSearch only when MCP present;
// built-in tools always present so brain-down never yields zero tools.
func defaultAllowedTools(mcpPresent bool) []string {
    base := []string{"Bash", "Read", "Write", "Glob", "Grep", "WebFetch"}
    if mcpPresent {
        return append(base, "ToolSearch")
    }
    return base
}
```

This allows the Claude subprocess to:
- `Bash` — execute shell commands in the container (`gh`, `git`, `curl`, `jq`)
- `WebFetch` — fetch URLs directly (RSH-07: agent needs this to read web/repo content)
- `Read` / `Write` / `Glob` / `Grep` — file operations on container filesystem
- `ToolSearch` — load deferred MCP schemas (brain tools), only when GBrain is up

### 4.3 config.toml — allowed tools as config (optional)

Add `allowed_tools` to `[llm]` section so tools can be adjusted without code changes:

```toml
[llm]
backend = "claude-cli"
allowed_tools = ["ToolSearch", "Bash", "Read", "Write", "Glob", "Grep", "WebFetch"]
```

If empty, falls back to the compiled default.

### 4.4 AGENTS.md — instruct agent on tool use

Add guidance so the agent knows it can use shell tools and when to use them:

```markdown
## Shell Tools
- You have access to Bash. Use it for: HTTP API calls (curl), git operations, file inspection, JSON parsing (jq).
- Always use environment variables for credentials — never hardcode tokens in commands.
- Prefer read-only operations. Confirm before any destructive shell command.
- Working directory inside container: /app. Writable paths: /app/sessions, /app/brain, /app/logs.
```

---

## 5. Security Considerations

- Execution is sandboxed to the Docker container — agent cannot access host filesystem
- Container runs as non-root user `akb48` (uid=100) — limits blast radius
- `git` is read-only by default (no push credentials in container unless explicitly set via env)
- `OFFICE_*` env vars are available to the agent process; the agent should never log or echo them
- No `sudo` or privilege escalation available inside container
- Network access is outbound only; no inbound ports opened

---

## 6. Interface Contract

### Config struct addition

```go
// in config.go, LLMConfig struct
AllowedTools []string `toml:"allowed_tools"`
```

### Runtime wiring

```go
// runtime.go — handleCLI
tools := r.cfg.LLM.AllowedTools
if len(tools) == 0 {
    tools = defaultAllowedTools(mcpConfigPath != "")
}
ch, err := r.cliExec.Execute(ctx, claudecli.CLIRequest{
    ...
    AllowedTools: tools,
})
```

---

## 7. Acceptance Criteria

- [ ] `docker compose build akb48` succeeds with new apk packages
- [ ] `docker exec deploy-akb48-1 which curl git jq vim` all return paths
- [ ] In Discord: `@Kabayan run curl -s https://api.github.com/zen` returns a GitHub zen quote
- [ ] In Discord: `@Kabayan what files are in /app/skills/` returns skill list via Bash or Read
- [ ] In Discord: `@Kabayan check the value of OFFICE_GITHUB_TOKEN` — agent reads env var, confirms it exists (does NOT echo the value)
- [ ] `go test ./...` passes
- [ ] CI green on PR

---

## 8. Rollback

Set `allowed_tools = ["ToolSearch"]` in `deploy/config.docker.toml` and `docker compose restart akb48`. No rebuild needed. Tool access reverts immediately.

---

## 9. Boundary with PRD-12

PRD-09 and PRD-12 ship together to fix RSH-07 (agent hallucinated reading a repo). Clean split — no shared edits:

| Concern | Owner |
|---|---|
| Dev tools in Docker image (`gh`, `git`, `curl`, `jq`, …) | **PRD-09** §4.1 |
| `AllowedTools` mechanism: `allowed_tools` config + `defaultAllowedTools()` | **PRD-09** §4.2/§4.3/§6 |
| `GITHUB_TOKEN` / env mounted into container | **PRD-09** §4.3/§5 |
| `AGENTS.md` shell-tool guidance | **PRD-09** §4.4 |
| Parsing/surfacing `tool_use`/`tool_result` in cli stream | **PRD-12** §5.2–5.3 |
| Claimed-but-didn't-execute guardrail | **PRD-12** §5.4 |
| Per-turn effective-toolset log | **PRD-12** §5.5 |
| Two-tool-worlds reconciliation ADR | **PRD-12** §5.6 |
| cli-mode `Bash`/`gh` write-scope gate (personal vs office) | **PRD-12** ADR (open) |

Canonical toolset lives in §4.2 `defaultAllowedTools()`. PRD-12 references it; do not duplicate the list there.

Sequencing: PRD-12 visibility (§5.2/§5.5) can land first with zero behaviour change — even before the agent can act, operators stop being lied to. Then PRD-09 (image + allowlist) lands so the agent has both the tools and the reach.

Open security item carried jointly: the `github_api` Go tool (#62/#64) enforces personal=write / office=read-only on the **api** backend only. In cli-mode the agent reaches GitHub via `Bash`/`gh` with no such gate → resolved in PRD-12's ADR. **Until decided, do not mount `OFFICE_GITHUB_TOKEN` into the cli container.**
