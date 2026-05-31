# Phase 11: Web Search Tool — Real-Time Information Access

**PRD:** `akb48-prd/PRD-11-web-search-tool.md`
**Epic ticket:** KLW-038
**SDLC Class:** `class:low`
**Depends on:** Phase 1 (tool registry), env.Reader gate (#61, merged).

**Goal:** Register a `web_search` tool (Brave Search API) so the agent can fetch current information on demand. Key gated via `env.Reader`. Mirrors the `github_api` tool pattern (#62/#64).

**Definition of Done:**
- `web_search` registered in `runtime.New()` (api backend)
- key read via `env.Reader`; missing key → descriptive error, no panic
- results formatted as numbered list (title, URL, snippet)
- response truncated at `max_tool_result_tokens`
- `go test ./internal/tools/search/...` covers success, missing key, HTTP error, truncation
- `go test ./...` green
- agent answers "latest Go release?" with current data (manual smoke)

**Tickets:** KLW-047 → KLW-048

> Note: in `claude-cli` mode the agent already gets `WebFetch` (Phase 9). `web_search` is the structured-results tool for the **api** backend. Both can coexist; document that cli-mode prefers `WebFetch`.

---

## KLW-047 — `web_search` tool + wiring

**Type:** Feature
**Effort:** 3 SP
**Labels:** `phase/11`, `type/feature`, `size/M`, `component/runtime`
**Dependencies:** none
**Branch:** `feature/KLW-047-web-search`

### User Story
> As the agent, I want a `web_search` tool that returns titled snippets with URLs, so I can answer questions about current events and recent docs.

### Implementation Plan
Per PRD-11 §6/§7, mirroring `internal/tools/github`:
1. **New** `internal/tools/search/search.go` — `NewHandler(er *env.Reader, maxResultTokens int) (tools.ToolDefinition, tools.ToolHandler)`. Reads `BRAVE_SEARCH_API_KEY` via `er.Get`; `GET https://api.search.brave.com/res/v1/web/search?q=&count=` with `X-Subscription-Token`. Parse `web.results[]` → numbered `[n] title / URL / snippet`. Truncate at `maxResultTokens*4` chars. 10s timeout. Inject HTTP client for tests (same pattern as github tool's `newHandlerWithClient`).
2. **Wire** in `runtime.New()` after `github_api`:
   ```go
   sDef, sHandler := searchtools.NewHandler(envReader, cfg.LLM.MaxToolResultTokens)
   registry.Register(sDef, sHandler)
   ```
3. Add `BRAVE_SEARCH_API_KEY` to `.env` (operator supplies value).

### Acceptance Criteria
- [ ] tool registered; appears in `registry.Definitions()`
- [ ] key via `env.Reader` only — no raw `os.Getenv` in handler
- [ ] missing key → descriptive error string, no panic
- [ ] results numbered with title/URL/snippet
- [ ] truncation at `max_tool_result_tokens`
- [ ] `go build ./...` clean

---

## KLW-048 — Tests + smoke

**Type:** Testing
**Effort:** 2 SP
**Labels:** `phase/11`, `type/test`, `size/S`, `component/runtime`
**Dependencies:** KLW-047
**Branch:** `feature/KLW-048-web-search-tests`

### Implementation Plan
- `httptest` mock server (rewriteTransport pattern from github tool): success response parse, missing key, HTTP 4xx/5xx → error, no-results message, truncation.
- Manual smoke: `@Kabayan what is the latest Go release?` → current version with source URL.

### Acceptance Criteria
- [ ] success, missing-key, HTTP-error, no-results, truncation tests pass
- [ ] `go test ./internal/tools/search/...` green
- [ ] `go test ./...` green; `go vet ./...` clean
- [ ] manual smoke documented in PR

---

## Risk Register

| Risk | Likelihood | Mitigation |
|---|---|---|
| Brave API quota/cost | Low | Pay-per-use; key operator-controlled; truncation caps payload |
| Snippet quality varies | Low | `count` param; agent can refine query |
| Provider lock-in | Low | Handler isolates provider; Tavily swap is a localized change |

## File Change Summary

| File | Change |
|---|---|
| `internal/tools/search/search.go` | new tool |
| `internal/tools/search/search_test.go` | tests |
| `internal/runtime/runtime.go` | register `web_search` |
| `.env` | `BRAVE_SEARCH_API_KEY` (value by operator) |
