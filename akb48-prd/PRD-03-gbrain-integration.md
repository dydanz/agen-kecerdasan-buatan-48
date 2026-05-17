# PRD-03: GBrain Integration via MCP

**Status:** Draft v2.0 (Go)
**Parent:** PRD-00 (AKB48 Master PRD)
**Author:** Dandi
**Created:** April 26, 2026
**Revised:** May 1, 2026
**Dependencies:** PRD-01 (Core Runtime — tool registry)
**Estimated Effort:** 2-3 days

---

## 1. Problem

AKB48 needs a brain — a persistent knowledge store where the agent can query past context ("What do I know about our staging cluster?") and store new facts ("Remember that the database port changed to 5433"). Without a brain, every conversation starts from zero and no knowledge compounds.

GBrain is our chosen brain. It exposes 30+ tools via MCP (Model Context Protocol). This PRD covers connecting AKB48 to GBrain via MCP and registering GBrain's tools in AKB48's tool registry so the LLM can invoke them naturally.

---

## 2. Goals

- **G1:** AKB48 starts a GBrain MCP server on startup and maintains the connection
- **G2:** GBrain's MCP tools are registered in AKB48's tool registry and available to the LLM
- **G3:** The operator can ask "What do you know about X?" and get a gbrain-backed answer
- **G4:** The operator can say "Remember that Y" and have it stored in gbrain
- **G5:** If GBrain is unavailable, AKB48 continues operating (degraded mode) — it just can't access memory

## 3. Non-Goals

- Building a custom memory layer (we use GBrain as-is)
- Direct PostgreSQL access (GBrain abstracts this)
- Dream cycle / enrichment scheduling (Phase 1, Week 4)
- Custom MCP server development
- Memory flush / fact extraction pipeline (PRD-05, Phase 2)

---

## 4. Actors

| Actor | Role |
|-------|------|
| Operator | Asks questions that require brain context, stores facts |
| AKB48 Runtime | Manages MCP connection, dispatches tool calls |
| GBrain MCP Server | Provides search, put, get, query, graph tools via MCP |
| Claude API | Decides when to invoke gbrain tools based on the conversation |

---

## 5. User Stories

| ID | Story | Acceptance Criteria |
|----|-------|-------------------|
| US-B01 | As an operator, when AKB48 starts, GBrain is automatically connected | Startup log shows "GBrain connected: N tools available" |
| US-B02 | As an operator, I ask "What do you know about our staging cluster?" and the agent searches gbrain | Agent invokes `gbrain_search` tool, returns relevant results from brain |
| US-B03 | As an operator, I say "Remember that our staging cluster is in ap-southeast-1" and it's stored | Agent invokes `gbrain_put` tool, confirms storage, fact is retrievable in future sessions |
| US-B04 | As an operator, if GBrain crashes, AKB48 tells me and continues working without memory | Chat shows "Brain disconnected — operating without memory" and responses continue (without brain context) |
| US-B05 | As an operator, I can query the brain's page count or health | Agent can invoke `gbrain_stats` or similar diagnostic tool |

---

## 6. Technical Design

### 6.1 MCP Connection Architecture

```
┌────────────────────┐       stdio        ┌──────────────────┐
│   AKB48        │◄────────────────►  │   GBrain MCP     │
│   (MCP Client)     │   JSON-RPC 2.0     │   Server         │
│                    │                     │   (gbrain serve) │
│  ┌──────────────┐  │                     │  ┌────────────┐  │
│  │ Tool Registry │  │  tools/list ──────►│  │ 30+ tools  │  │
│  │              │  │  tools/call ──────►│  │            │  │
│  └──────────────┘  │                     │  └────────────┘  │
└────────────────────┘                     └──────────────────┘
         │                                          │
         └──────────── Same VPS ────────────────────┘
```

**Transport:** stdio (JSON-RPC 2.0 over stdin/stdout of a subprocess)

**Why stdio over HTTP SSE:**
- Simpler: no HTTP server, no port allocation, no CORS
- Co-located: both processes run on the same VPS
- Lifecycle: AKB48 manages GBrain as a child process — starts it, monitors it, restarts if crashed
- Latency: subprocess stdio is faster than localhost HTTP

### 6.2 Core Types

```go
// internal/brain/types.go

// ToolDefinition is a single MCP tool as returned by tools/list.
type ToolDefinition struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    InputSchema json.RawMessage `json:"inputSchema"`
}

// jsonrpcRequest is a JSON-RPC 2.0 request envelope.
type jsonrpcRequest struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      int64           `json:"id"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
}

// jsonrpcResponse is a JSON-RPC 2.0 response envelope.
type jsonrpcResponse struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      int64           `json:"id"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *jsonrpcError   `json:"error,omitempty"`
}

// jsonrpcError is the error object inside a JSON-RPC 2.0 response.
type jsonrpcError struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
}

// ErrBrainUnavailable is returned by all gbrain_* handlers when GBrain is down.
var ErrBrainUnavailable = errors.New("brain unavailable")

// BrainConfig is the [brain] section of config.toml, parsed by PRD-01's config loader.
type BrainConfig struct {
    Enabled              bool     `toml:"enabled"`
    MCPTransport         string   `toml:"mcp_transport"`        // "stdio" only in Phase 1
    GBrainCommand        string   `toml:"gbrain_command"`        // e.g. "gbrain"
    GBrainArgs           []string `toml:"gbrain_args"`           // e.g. ["serve"]
    GBrainWorkingDir     string   `toml:"gbrain_working_dir"`    // e.g. "~/brain"
    HealthCheckInterval  int      `toml:"health_check_interval_s"` // seconds; default 30
    MaxRestartAttempts   int      `toml:"max_restart_attempts"`  // default 3
    ToolPrefix           string   `toml:"tool_prefix"`           // default "gbrain"
}
```

### 6.3 MCPClient

`MCPClient` owns the subprocess lifecycle and all JSON-RPC I/O. It is goroutine-safe.

```go
// internal/brain/mcp_client.go

type MCPClient struct {
    cmd       *exec.Cmd
    stdin     io.WriteCloser      // pipe to gbrain's stdin
    responses sync.Map            // map[int64]chan json.RawMessage
    nextID    atomic.Int64        // monotonically increasing request IDs
    mu        sync.Mutex          // guards cmd and stdin reassignment on restart
    connected bool                // true after successful initialize handshake
}

// Start spawns the subprocess, wires stdin/stdout pipes, launches the reader
// goroutine, and sends the MCP initialize handshake.
// Returns an error if the process cannot be started or initialize fails.
func (c *MCPClient) Start(ctx context.Context) error

// Stop sends an MCP shutdown notification and terminates the subprocess.
// It is idempotent.
func (c *MCPClient) Stop() error

// ListTools calls tools/list and returns the discovered tool definitions.
func (c *MCPClient) ListTools(ctx context.Context) ([]ToolDefinition, error)

// CallTool calls tools/call for the named tool with the given JSON arguments.
// Returns the raw JSON result from GBrain.
func (c *MCPClient) CallTool(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error)

// sendRequest marshals and writes a JSON-RPC 2.0 request to stdin, registers a
// response channel in c.responses, and blocks until the reader goroutine delivers
// the matching response or ctx is cancelled.
func (c *MCPClient) sendRequest(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)

// readLoop is launched as a goroutine by Start. It runs a bufio.Scanner over the
// subprocess stdout. Each line is unmarshaled as a jsonrpcResponse; the response
// is delivered to the channel registered under its ID in c.responses. If the
// scanner returns an error (subprocess exited), readLoop returns so the health
// monitor can attempt a restart.
func (c *MCPClient) readLoop(stdout io.Reader)
```

**Concurrency model:**

```
 caller goroutine                    readLoop goroutine
 ──────────────────                  ──────────────────
 id  := nextID.Add(1)
 ch  := make(chan json.RawMessage, 1)
 responses.Store(id, ch)
 write request to stdin ──────────► gbrain process
                                     gbrain process ──► stdout line
                        ◄────────── scanner reads line
                                     unmarshal response
                                     responses.Load(id) → ch
                                     ch <- raw result
 raw := <-ch  ◄─────────────────────
 responses.Delete(id)
```

### 6.4 GBrainBridge

`GBrainBridge` owns tool registration and the health-monitoring goroutine. It is the public API that the runtime uses.

```go
// internal/brain/bridge.go

type GBrainBridge struct {
    client          *MCPClient
    registry        *tools.ToolRegistry
    config          BrainConfig
    restartCount    int
    mu              sync.Mutex   // guards restartCount and available
    available       bool
}

// Start starts the MCP client, discovers tools, registers them in the tool
// registry, then launches the health-monitor goroutine. Returns an error only
// if the subprocess cannot be started at all; a failed tool discovery is logged
// and treated as degraded mode, not a fatal error.
func (b *GBrainBridge) Start(ctx context.Context) error

// IsAvailable returns true if GBrain is connected and healthy.
func (b *GBrainBridge) IsAvailable() bool

// Stop gracefully shuts down the MCP client and cancels the health monitor.
func (b *GBrainBridge) Stop() error

// discoverAndRegister calls ListTools and registers each tool in the registry
// under the "gbrain_" prefix. Called at startup and again after each restart.
func (b *GBrainBridge) discoverAndRegister(ctx context.Context) error

// makeHandler returns a ToolRegistry handler closure for a single GBrain tool.
// The closure checks availability at call time; if unavailable it returns
// ("", ErrBrainUnavailable) so the tool registry can surface an error string
// to the LLM without panicking.
func (b *GBrainBridge) makeHandler(toolName string) func(ctx context.Context, args json.RawMessage) (json.RawMessage, error)

// healthMonitor runs in a goroutine. Every HealthCheckInterval seconds it calls
// tools/list as a liveness probe. On failure it calls attemptRestart.
func (b *GBrainBridge) healthMonitor(ctx context.Context)

// attemptRestart tries to restart the subprocess up to MaxRestartAttempts times
// with exponential backoff (1s, 2s, 4s). On success it calls discoverAndRegister
// and marks available = true. On permanent failure it logs a warning and marks
// available = false (degraded mode).
func (b *GBrainBridge) attemptRestart(ctx context.Context)
```

**Health monitor goroutine sketch:**

```go
func (b *GBrainBridge) healthMonitor(ctx context.Context) {
    ticker := time.NewTicker(time.Duration(b.config.HealthCheckInterval) * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            _, err := b.client.ListTools(ctx)
            if err != nil {
                slog.Warn("GBrain health check failed", "err", err)
                b.mu.Lock()
                b.available = false
                b.mu.Unlock()
                b.attemptRestart(ctx)
            }
        }
    }
}
```

**Restart with exponential backoff:**

```go
func (b *GBrainBridge) attemptRestart(ctx context.Context) {
    b.mu.Lock()
    defer b.mu.Unlock()
    for attempt := 1; attempt <= b.config.MaxRestartAttempts; attempt++ {
        backoff := time.Duration(1<<uint(attempt-1)) * time.Second
        select {
        case <-ctx.Done():
            return
        case <-time.After(backoff):
        }
        _ = b.client.Stop()
        if err := b.client.Start(ctx); err != nil {
            slog.Warn("GBrain restart failed", "attempt", attempt, "err", err)
            continue
        }
        if err := b.discoverAndRegister(ctx); err != nil {
            slog.Warn("GBrain tool discovery failed after restart", "attempt", attempt, "err", err)
            continue
        }
        b.available = true
        b.restartCount = 0
        slog.Info("GBrain reconnected", "attempt", attempt)
        return
    }
    slog.Error("GBrain permanently disconnected after max restart attempts",
        "max", b.config.MaxRestartAttempts)
}
```

### 6.5 Tool Registry Integration

`GBrainBridge.discoverAndRegister` iterates over `ListTools` results and calls `registry.Register` for each tool:

```go
for _, def := range toolDefs {
    registeredName := b.config.ToolPrefix + "_" + def.Name  // e.g. "gbrain_search"
    capturedName   := def.Name                              // capture for closure
    b.registry.Register(tools.ToolSpec{
        Name:        registeredName,
        Description: def.Description,
        InputSchema: def.InputSchema,
        Handler:     b.makeHandler(capturedName),
    })
}
slog.Info("GBrain connected", "tools", len(toolDefs))
```

The `makeHandler` closure returns `ErrBrainUnavailable` (not a panic) when `IsAvailable()` is false, so the tool registry converts it to an error string that the LLM receives and handles gracefully.

### 6.6 Expected GBrain MCP Tools

Tools are discovered dynamically via `tools/list` — AKB48 hardcodes nothing. The table below is informational only, based on GBrain v0.12 documentation:

| Tool | Purpose | Hello World? |
|------|---------|-------------|
| `search` | Hybrid search (vector + keyword + graph) across all brain pages | **YES** |
| `put` | Create or update a brain page | **YES** |
| `get` | Retrieve a specific page by title or ID | **YES** |
| `query` | Structured query against the knowledge graph | No |
| `graph_neighbors` | Find related entities in the graph | No |
| `enrich` | Trigger enrichment for a person/company page | No |
| `stats` | Brain statistics (page count, entity count, etc.) | Nice to have |
| `maintain` | Run maintenance tasks (dream cycle) | No (Phase 1, Week 4) |
| `embed` | Generate embeddings for text | No |
| `link` | Create explicit links between pages | No |
| `unlink` | Remove links between pages | No |
| `delete` | Remove a page | No |
| `list_pages` | List pages by type or tag | No |
| `recent` | List recently modified pages | No |

### 6.7 Degraded Mode (Brain Disconnected)

If GBrain is unavailable:
1. `makeHandler` closures return `("", ErrBrainUnavailable)` — the tool registry converts this to an error string: `"Error: Brain is disconnected. Operating without memory."`
2. The LLM receives this error string and explains to the user that memory is unavailable
3. AKB48 continues processing messages — just without brain-backed context
4. `slog.Warn` is emitted every 60 seconds: "GBrain disconnected — operating in degraded mode"
5. The health-monitor goroutine continues attempting reconnect every `HealthCheckInterval` seconds

---

## 7. File Layout

```
internal/
└── brain/
    ├── bridge.go       // GBrainBridge — lifecycle, health monitor, registration
    ├── mcp_client.go   // MCPClient — subprocess, JSON-RPC, reader goroutine
    └── types.go        // ToolDefinition, BrainConfig, ErrBrainUnavailable, jsonrpc types
```

GBrainBridge is constructed by the runtime (`core/runtime.go`) during startup and wired into the tool registry. It exposes no HTTP handlers of its own.

---

## 8. Configuration

```toml
[brain]
enabled                  = true
mcp_transport            = "stdio"        # Only stdio supported in Phase 1
gbrain_command           = "gbrain"       # Must be in PATH
gbrain_args              = ["serve"]
gbrain_working_dir       = "~/brain"
health_check_interval_s  = 30
max_restart_attempts     = 3
tool_prefix              = "gbrain"
```

Config is parsed by the PRD-01 config loader into `BrainConfig` via `github.com/BurntSushi/toml`. Fail fast on startup if `gbrain_command` is empty when `enabled = true`.

---

## 9. Setup Prerequisites

Before AKB48 can connect to GBrain, the operator must:

1. **Install GBrain:**
   ```bash
   # TODO-B01: Verify exact installation steps for latest GBrain
   npm install -g gbrain
   ```

2. **Initialize a brain:**
   ```bash
   cd ~/brain
   gbrain init
   ```

3. **Verify GBrain runs:**
   ```bash
   gbrain serve
   # Should output: "GBrain MCP server listening on stdio"
   ```

4. **Add GBrain to PATH** (if installed locally)

5. **Configure PostgreSQL** (GBrain dependency):
   ```bash
   # Docker approach (recommended):
   docker compose up -d postgres
   ```

---

## 10. Error Handling

| Error | Handling | User-facing |
|-------|----------|-------------|
| GBrain not installed / not in PATH | `exec.LookPath` fails at startup; brain disabled, warning logged | "Brain unavailable — memory features disabled" |
| GBrain process crashes | `readLoop` exits; health monitor triggers `attemptRestart` (up to 3 times) | Brief pause, then resumes. If all retries fail: "Brain disconnected" |
| MCP request timeout (>10s) | `ctx` deadline exceeded in `sendRequest`; channel cleaned up via `responses.Delete`; error returned to caller | LLM receives "brain query timed out" error string |
| Invalid JSON from GBrain stdout | `json.Unmarshal` error in `readLoop`; line is logged and skipped; no in-flight request is resolved | Silent recovery; if all requests time out, health check will detect the problem |
| Brain data directory missing | `gbrain serve` exits immediately; `Start` returns error | "Brain directory ~/brain not found. Run: gbrain init" |
| Response channel leak on caller cancel | `sendRequest` defers `responses.Delete(id)` so the orphaned channel is always removed | None — internal cleanup |

---

## 11. Acceptance Criteria

- [ ] `gbrain serve` is started automatically when AKB48 starts (if `brain.enabled = true`)
- [ ] Startup log shows "GBrain connected: N tools available" with actual tool count
- [ ] Operator asks "What do you know about X?" → agent invokes `gbrain_search` → returns results
- [ ] Operator says "Remember that Y" → agent invokes `gbrain_put` → confirms stored
- [ ] Stored fact is retrievable in a new session (restart AKB48, ask again)
- [ ] Killing the gbrain process triggers auto-restart within 30s
- [ ] After 3 failed restarts, degraded mode is entered with clear user notification
- [ ] With brain disabled in config, AKB48 starts and operates without brain tools
- [ ] Concurrent tool calls from a single agent turn are handled correctly (no response misrouting)
- [ ] Cancelling a context mid-request does not leak the response channel

---

## 12. Open Questions

| ID | Question | Default |
|----|----------|---------|
| TODO-B01 | Exact GBrain installation steps may vary by version. Need to test with latest stable. | Test and document during implementation. |
| TODO-B02 | Should AKB48 manage GBrain's PostgreSQL via Docker Compose, or expect the operator to manage it separately? | Separate management. GBrain's own docs cover Postgres setup. AKB48 shouldn't own GBrain's infra. |
| TODO-B03 | Tool name collision: if GBrain exposes a tool called "search" and we later add a web search tool also called "search," they'll collide. The prefix (`gbrain_search`) prevents this, but is the prefix the right approach? | Yes, prefix all GBrain tools with `gbrain_`. Consistent, unambiguous, no collision risk. |
| TODO-B04 | Should AKB48 always include brain context in the system prompt (pre-fetch relevant pages), or let the LLM decide when to search? | Let the LLM decide (tool call). Pre-fetching adds latency and tokens to every message. The LLM is good at deciding when it needs context. Revisit if retrieval quality is poor. |
| TODO-B05 | GBrain brain directory: `~/brain/` or `~/.akb48/brain/`? Separate lifecycle vs co-located management. | `~/brain/` — the brain has its own lifecycle, independent of the claw runtime. Multiple claws could theoretically share a brain. |
| TODO-B06 | Should `readLoop` log and skip unrecognized JSON lines (e.g. GBrain startup banners on stderr that bleed into stdout), or treat them as fatal? | Log and skip. GBrain may emit diagnostic lines; the reader should be tolerant. Validate that the `id` field exists before routing. |
