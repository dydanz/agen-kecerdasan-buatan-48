# ADR-001: Two Tool Surfaces — api registry vs cli subprocess

**Date:** 2026-06-01
**Status:** Accepted
**Linked PR:** #82
**Linked KLW:** KLW-052
**Linked PRD:** akb48-prd/PRD-12-agent-tool-visibility.md §5.6

---

## Context

AKB48 supports two LLM backends:

- **`api`** — Anthropic SDK in-process. Tools come from the Go `tools.Registry` (registered handlers: `github_api`, `web_search`, `gbrain_*`).
- **`claude-cli`** — Claude subprocess per turn. Tools are Claude Code built-ins (`Bash`, `Read`, `WebFetch`, etc.) + MCP tools (`mcp__gbrain__*`) passed via `--tools` and `--mcp-config` flags.

The two surfaces are separate by construction. A tool added to the Go registry (e.g. `github_api`) is invisible to the cli subprocess; a subprocess built-in (`Bash`) is invisible to the api backend's tool loop. This divergence caused the RSH-07 incident (agent narrated hours of repo-reading it could not perform).

## Decision

**We keep two distinct tool surfaces** and make the divergence explicit rather than unifying them.

- **api backend tools:** Go registry handlers. Add new api tools by registering in `runtime.New()`.
- **cli backend tools:** Claude Code built-ins + MCP. Configured via `defaultAllowedTools()` in `runtime.go` and `allowed_tools` in config. Extend by adding to the default list or config.
- Both surfaces log their effective toolset at turn start (`slog.Info("agent toolset", ...)`), making the divergence visible in logs without code changes.
- `NarrationGuardHook` catches turns where the response claims action but zero tools ran.

Unification (generating an MCP server from the Go registry so both backends share one surface) was evaluated but deferred: the added complexity isn't justified until the registry grows significantly (>10 tools) or the backends are consolidated.

## Cli Write-Scope Gate (open item resolved)

**Issue:** `github_api` in the api backend enforces personal=write / office=read-only. In cli mode the agent uses `Bash`/`gh` with `GITHUB_TOKEN` and has no equivalent gate.

**Decision:** Accept personal-write in cli mode; document the boundary explicitly.

- `OFFICE_GITHUB_TOKEN` is **not mounted** into the cli container. The cli agent can reach personal GitHub repos (`GITHUB_TOKEN`) with full write via `Bash`/`gh`.
- Office repos are read-only in the api backend via `github_api`. In cli mode, office access requires operator to explicitly provide `OFFICE_GITHUB_TOKEN` at the session level — this is intentional friction.
- A policy shim wrapping `gh` (option ii from PRD-12) adds complexity without proportional security benefit for a solo-operator runtime.

## Alternatives Considered

| Option | Why rejected |
|---|---|
| Generate MCP server from Go registry — single surface | Too much wiring for current tool count. Revisit if registry grows > 10 tools. |
| `bash_exec` Go handler in api registry | Adds shell access to api backend, increases attack surface. Use cli backend for shell operations. |
| `gh` policy shim for office write-scope gate | Fragile; easy to bypass; adds maintenance burden. Prefer token-scope enforcement at the infra level. |

## Consequences

- Operators must know which backend they're using when diagnosing "tool not found" errors.
- Adding a new tool requires deciding which surface it belongs to (registry vs cli built-in).
- Docs: `CLAUDE.md` "Memory Brain MCP Connection" section and dev plans reflect the boundary.
- Revisit unification if: cli backend is retired, registry tools exceed 10, or a third backend is added.
