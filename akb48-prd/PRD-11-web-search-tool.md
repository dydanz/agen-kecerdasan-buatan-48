# PRD-11: Web Search Tool — Real-Time Information Access for the Agent

**Status:** Draft v1.0
**Parent:** PRD-00 (AKB48 Master PRD), PRD-01 (Core Runtime — tool registry)
**Author:** Dandi
**Created:** 2026-05-31
**Dependencies:** PRD-01 (Tool registry), #61 (env.Reader gate)
**Estimated Effort:** 1–2 days
**SDLC Class:** `class:low`
**Ticket:** KLW-038

---

## 1. Problem

The agent's knowledge is frozen at training cutoff (August 2025). It cannot:
- Look up current GitHub repo status, CI results, or latest releases
- Check current exchange rates, prices, or real-time data
- Verify facts that may have changed since training
- Research libraries, docs, or APIs that were released or updated recently

Without a web search tool the agent must either hallucinate or admit ignorance. For a technical assistant used daily, this is a hard ceiling.

---

## 2. Goals

- **G1:** Register `web_search` tool in the tool registry — agent calls it on demand
- **G2:** Returns top N results with title, URL, and snippet per result
- **G3:** API key read via `env.Reader` (#61) — key must be declared in `.env`
- **G4:** Response truncated at `max_tool_result_tokens`
- **G5:** Works for both `api` and `claude-cli` backends
- **G6:** Missing API key → descriptive error, no crash

---

## 3. Non-Goals

- Full page content fetching (follow links and scrape) — out of scope; snippet only
- Image or video search
- Search result caching across sessions
- Multiple search provider fallback — one provider, one key

---

## 4. Search Provider

**Brave Search API** — chosen for:
- Pay-per-use ($5 per 1000 queries; generous free tier)
- No Google/Bing dependency
- Clean JSON response format
- Privacy-respecting (no user tracking)
- Env var: `BRAVE_SEARCH_API_KEY`

Alternative: **Tavily** (`TAVILY_API_KEY`) — designed for LLM tool use, returns cleaner snippets. Either works; Brave preferred for cost.

---

## 5. Tool Definition

```json
{
  "name": "web_search",
  "description": "Search the web for current information. Use when you need real-time data, recent events, docs, or anything that may have changed since training cutoff (Aug 2025).",
  "parameters": {
    "query": {
      "type": "string",
      "description": "Search query. Be specific. E.g. 'golang 1.23 release notes' not 'go new features'"
    },
    "count": {
      "type": "integer",
      "description": "Number of results to return (1–10, default 5)",
      "default": 5
    }
  },
  "required": ["query"]
}
```

---

## 6. Handler Behaviour

1. Read `BRAVE_SEARCH_API_KEY` via `env.Reader` — error if missing
2. `GET https://api.search.brave.com/res/v1/web/search?q={query}&count={count}`
3. Header: `X-Subscription-Token: {key}`, `Accept: application/json`
4. Parse response → extract `web.results[]` → format each as:
   ```
   [1] Title
   URL: https://...
   Snippet: ...
   ```
5. Join results, truncate to `max_tool_result_tokens * 4` chars
6. Return formatted string

### Response format to LLM

```
[1] Go 1.23 Release Notes - The Go Programming Language
URL: https://go.dev/doc/go1.23
Snippet: Go 1.23 adds range-over-func iterators, improved timer behaviour, and toolchain management...

[2] ...
```

---

## 7. Technical Design

### 7.1 File

`internal/tools/search/search.go` — mirrors the github_api tool pattern.

```go
func NewHandler(er *env.Reader, maxResultTokens int) (tools.ToolDefinition, tools.ToolHandler)
```

### 7.2 Brave API Response (relevant fields)

```json
{
  "web": {
    "results": [
      {
        "title": "...",
        "url": "...",
        "description": "..."
      }
    ]
  }
}
```

### 7.3 Wiring

In `runtime.New()`, after `github_api`:

```go
searchDef, searchHandler := searchtools.NewHandler(envReader, cfg.LLM.MaxToolResultTokens)
registry.Register(searchDef, searchHandler)
```

### 7.4 Config

No new config keys. Add to `.env`:
```
BRAVE_SEARCH_API_KEY=BSA...
```

---

## 8. Error Handling

| Error | Behaviour |
|-------|-----------|
| API key missing / not in .env | Return error string to LLM: "web_search: BRAVE_SEARCH_API_KEY not set or not declared in .env" |
| HTTP 429 rate limit | Return error string: "web_search: rate limited — try again in a moment" |
| HTTP 4xx/5xx | Return error string with status code |
| No results | Return "web_search: no results found for query: {query}" |
| Network timeout | 10s timeout; return error string |

---

## 9. Acceptance Criteria

- [ ] `web_search` tool registered in `runtime.New()`
- [ ] Key read via `env.Reader` — raw `os.Getenv` not used in handler
- [ ] Missing key returns descriptive error, no panic
- [ ] Results formatted as numbered list with title, URL, snippet
- [ ] Response truncated at `max_tool_result_tokens`
- [ ] `go test ./internal/tools/search/...` covers: success response, missing key, HTTP error, truncation (httptest mock server)
- [ ] Agent can answer "what is the latest Go release?" with current data
