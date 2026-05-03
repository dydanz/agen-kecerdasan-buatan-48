# Phase 3: GBrain Integration

**Goal:** The agent can store and retrieve knowledge from GBrain via MCP. Brain unavailability degrades gracefully — the runtime continues and the LLM handles missing brain results naturally.

**Definition of Done:**
- "Remember that staging cluster is ap-southeast-1" → `gbrain_put` called, entity stored
- "What do you know about our staging cluster?" → `gbrain_search` called, stored fact returned
- Kill GBrain process → agent continues responding, `slog.Warn` every 60s
- GBrain restarts → auto-reconnected within 30s

**Tickets:** KLW-015 → KLW-016 → KLW-017

---

## KLW-015 — MCP Client (stdio subprocess)

**Type:** Chore
**Owner:** Backend
**Effort:** 8 SP
**Labels:** `phase/3`, `type/chore`, `size/L`, `component/brain`
**Dependencies:** KLW-001
**Branch:** `feat/mcp-client`

### Description

A generic JSON-RPC 2.0 MCP client over stdio. Manages the GBrain subprocess lifecycle and demuxes concurrent requests to their callers via per-request response channels. This is the transport layer — it knows nothing about GBrain's specific tools.

### Implementation Plan

**Files to create:**
- `internal/brain/mcp_client.go`
- `internal/brain/mcp_client_test.go`

**Data structures:**

```go
package brain

import (
    "bufio"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "log/slog"
    "os/exec"
    "sync"
    "sync/atomic"
)

type MCPClient struct {
    cmd       *exec.Cmd
    stdin     io.WriteCloser
    responses sync.Map      // map[int64]chan json.RawMessage
    nextID    atomic.Int64
    mu        sync.Mutex    // guards connected flag and stdin writes
    connected bool
}

func NewMCPClient(command string, args []string, workDir string) *MCPClient
```

**Lifecycle:**

```go
func (c *MCPClient) Start(ctx context.Context) error {
    c.cmd = exec.CommandContext(ctx, c.command, c.args...)
    if c.workDir != "" {
        c.cmd.Dir = c.workDir
    }

    stdin, err := c.cmd.StdinPipe()
    if err != nil { return err }
    stdout, err := c.cmd.StdoutPipe()
    if err != nil { return err }

    c.stdin = stdin
    if err := c.cmd.Start(); err != nil { return err }

    go c.readLoop(stdout)

    c.mu.Lock()
    c.connected = true
    c.mu.Unlock()
    return nil
}

func (c *MCPClient) Stop() error {
    c.mu.Lock()
    c.connected = false
    c.mu.Unlock()
    if c.stdin != nil {
        c.stdin.Close()
    }
    return c.cmd.Wait()
}
```

**readLoop — routes responses to waiting callers:**

```go
func (c *MCPClient) readLoop(r io.Reader) {
    scanner := bufio.NewScanner(r)
    for scanner.Scan() {
        line := scanner.Bytes()
        var resp struct {
            ID     int64           `json:"id"`
            Result json.RawMessage `json:"result"`
            Error  *struct {
                Message string `json:"message"`
            } `json:"error"`
        }
        if err := json.Unmarshal(line, &resp); err != nil {
            slog.Warn("MCP: non-JSON line on stdout", "line", string(line))
            continue
        }
        if ch, ok := c.responses.LoadAndDelete(resp.ID); ok {
            ch.(chan json.RawMessage) <- resp.Result
        }
    }
}
```

**call — makes a JSON-RPC request and waits for response:**

```go
func (c *MCPClient) call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
    id := c.nextID.Add(1)
    ch := make(chan json.RawMessage, 1)
    c.responses.Store(id, ch)
    defer c.responses.Delete(id)

    req, err := json.Marshal(map[string]interface{}{
        "jsonrpc": "2.0",
        "id":      id,
        "method":  method,
        "params":  params,
    })
    if err != nil { return nil, err }

    c.mu.Lock()
    if !c.connected {
        c.mu.Unlock()
        return nil, ErrDisconnected
    }
    _, writeErr := fmt.Fprintf(c.stdin, "%s\n", req)
    c.mu.Unlock()
    if writeErr != nil { return nil, writeErr }

    select {
    case result := <-ch:
        return result, nil
    case <-ctx.Done():
        return nil, ctx.Err()
    }
}
```

**Public MCP methods:**

```go
// ListTools calls tools/list and returns all available tool definitions.
func (c *MCPClient) ListTools(ctx context.Context) ([]ToolSpec, error)

// CallTool calls tools/call with the given tool name and JSON arguments.
func (c *MCPClient) CallTool(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error)

type ToolSpec struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    InputSchema json.RawMessage `json:"inputSchema"`
}

var ErrDisconnected = errors.New("MCP client is not connected")
```

### Acceptance Criteria

- [ ] `Start` launches the GBrain subprocess successfully
- [ ] `ListTools` returns at least 3 tools: `search`, `put`, `get`
- [ ] `CallTool("search", ...)` returns a non-empty JSON result
- [ ] 10 concurrent `CallTool` calls → all receive correct responses (no cross-routing)
- [ ] Non-JSON line on stdout → `slog.Warn` logged, client stays connected
- [ ] `Stop` terminates subprocess cleanly, no goroutine leak
- [ ] `call` after `Stop` → returns `ErrDisconnected` immediately

### Testing Plan

```go
// Unit test with mock subprocess
func TestMCPClient_ConcurrentCalls(t *testing.T) {
    // Fake subprocess echoes JSON-RPC responses with matching IDs
    // Launch 10 concurrent calls via errgroup
    // Assert each receives its own response, no cross-routing
}

func TestMCPClient_NonJSONLine(t *testing.T) {
    // Fake subprocess writes "not json\n" then a valid response
    // Assert client survives, valid response received
}

func TestMCPClient_CallAfterStop(t *testing.T) {
    client.Stop()
    _, err := client.call(ctx, "test", nil)
    assert errors.Is(err, ErrDisconnected)
}

// Integration test (requires gbrain installed)
func TestMCPClient_Integration(t *testing.T) {
    if testing.Short() { t.Skip("requires gbrain") }
    client := NewMCPClient("gbrain", []string{"serve"}, "~/brain")
    client.Start(ctx)
    defer client.Stop()
    tools, _ := client.ListTools(ctx)
    assert len(tools) >= 3
}
```

---

## KLW-016 — GBrain Bridge & Tool Registration

**Type:** Chore
**Owner:** Backend
**Effort:** 5 SP
**Labels:** `phase/3`, `type/chore`, `size/M`, `component/brain`
**Dependencies:** KLW-015 (MCP client), KLW-002 (tool registry)
**Branch:** `feat/gbrain-bridge`

### Description

Wraps `MCPClient` to discover GBrain's tools dynamically at startup, register them in the `ToolRegistry` with the `gbrain_` prefix, monitor health, and auto-restart on failure. Also provides the `SearchEntities` convenience method for the cold opener.

### Implementation Plan

**Files to create:**
- `internal/brain/bridge.go`
- `internal/brain/bridge_test.go`

**Struct:**

```go
type GBrainBridge struct {
    client    *MCPClient
    registry  *tools.Registry
    cfg       config.BrainConfig
    available atomic.Bool
    restarts  atomic.Int32
}

func NewGBrainBridge(cfg config.BrainConfig, registry *tools.Registry) *GBrainBridge
```

**Start — discover and register tools:**

```go
func (b *GBrainBridge) Start(ctx context.Context) error {
    if err := b.client.Start(ctx); err != nil {
        return fmt.Errorf("start MCP client: %w", err)
    }

    specs, err := b.client.ListTools(ctx)
    if err != nil {
        return fmt.Errorf("list MCP tools: %w", err)
    }

    for _, spec := range specs {
        toolName := b.cfg.ToolPrefix + "_" + spec.Name // e.g. "gbrain_search"
        b.registry.Register(
            tools.ToolDefinition{
                Name:        toolName,
                Description: spec.Description,
                InputSchema: spec.InputSchema,
            },
            b.makeHandler(spec.Name),
        )
    }

    b.available.Store(true)
    slog.Info("GBrain connected", "tools", len(specs))
    go b.healthMonitor(ctx)
    return nil
}
```

**makeHandler — creates a tool handler closure:**

```go
func (b *GBrainBridge) makeHandler(toolName string) tools.ToolHandler {
    return func(ctx context.Context, input json.RawMessage) (string, error) {
        if !b.available.Load() {
            return "", ErrBrainUnavailable
        }
        result, err := b.client.CallTool(ctx, toolName, input)
        if err != nil {
            return "", err
        }
        return string(result), nil
    }
}

var ErrBrainUnavailable = errors.New("Brain is disconnected")
```

**healthMonitor — detects failures and auto-restarts:**

```go
func (b *GBrainBridge) healthMonitor(ctx context.Context) {
    ticker := time.NewTicker(time.Duration(b.cfg.HealthCheckIntervalS) * time.Second)
    defer ticker.Stop()
    warnTicker := time.NewTicker(60 * time.Second)
    defer warnTicker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            if _, err := b.client.ListTools(ctx); err != nil {
                slog.Warn("GBrain health check failed", "error", err)
                b.available.Store(false)
                b.attemptRestart(ctx)
            }
        case <-warnTicker.C:
            if !b.available.Load() {
                slog.Warn("GBrain still unavailable — brain features degraded")
            }
        }
    }
}
```

**attemptRestart — exponential backoff:**

```go
func (b *GBrainBridge) attemptRestart(ctx context.Context) {
    for attempt := 1; attempt <= b.cfg.MaxRestartAttempts; attempt++ {
        backoff := time.Duration(1<<uint(attempt-1)) * time.Second // 1s, 2s, 4s
        slog.Info("GBrain restart attempt", "attempt", attempt, "backoff", backoff)
        time.Sleep(backoff)

        b.client.Stop()
        if err := b.client.Start(ctx); err != nil {
            slog.Error("GBrain restart failed", "attempt", attempt, "error", err)
            continue
        }
        b.restarts.Add(1)
        b.available.Store(true)
        slog.Info("GBrain reconnected", "total_restarts", b.restarts.Load())
        return
    }
    slog.Error("GBrain max restart attempts exhausted — brain permanently degraded",
        "max_attempts", b.cfg.MaxRestartAttempts)
}
```

**SearchEntities — for cold opener:**

```go
// SearchEntities returns a formatted string of top-K brain results for the query.
// Returns "" if unavailable or no results.
func (b *GBrainBridge) SearchEntities(ctx context.Context, query string, limit int) (string, error) {
    if !b.available.Load() {
        return "", nil
    }
    args, _ := json.Marshal(map[string]interface{}{
        "query":        query,
        "limit":        limit,
        "entity_types": []string{"person", "project", "decision", "product", "policy"},
    })
    result, err := b.client.CallTool(ctx, "search", args)
    if err != nil {
        return "", err
    }
    return formatSearchResults(result), nil
}
```

### Acceptance Criteria

- [ ] After `Start`, all `gbrain_search`, `gbrain_put`, `gbrain_get` registered in registry
- [ ] Tool names are prefixed with `gbrain_` (or configured `tool_prefix`)
- [ ] Kill GBrain process → health check detects failure → `available.Store(false)`
- [ ] Auto-restart within 3 attempts → `available.Store(true)`, tools work again
- [ ] After 3 failed restarts → `available` stays false, `slog.Error` logged
- [ ] Tool call when unavailable → `ToolResult{IsError: true, Output: "Brain is disconnected"}`
- [ ] `SearchEntities` when unavailable → returns `"", nil` (no error)

### Testing Plan

```go
func TestGBrainBridge_ToolRegistration(t *testing.T) {
    // Mock MCPClient returning 3 tools: search, put, get
    // After Start, assert registry has gbrain_search, gbrain_put, gbrain_get
}

func TestGBrainBridge_HandlerWhenUnavailable(t *testing.T) {
    b.available.Store(false)
    result := registry.Execute(ctx, types.ToolCall{Name: "gbrain_search"}, "")
    assert result.IsError == true
    assert result.Output == "Brain is disconnected"
}

func TestGBrainBridge_AutoRestart(t *testing.T) {
    // Mock client that fails first Start, succeeds on second
    // Simulate health check failure
    // Assert available becomes true after restart
}

func TestSearchEntities_Unavailable(t *testing.T) {
    b.available.Store(false)
    result, err := b.SearchEntities(ctx, "staging", 3)
    assert result == "" && err == nil
}
```

---

## KLW-017 — GBrain Degraded Mode & Runtime Integration

**Type:** Chore
**Owner:** Backend
**Effort:** 3 SP
**Labels:** `phase/3`, `type/chore`, `size/M`, `component/brain`
**Dependencies:** KLW-007 (runtime), KLW-016 (bridge)
**Branch:** `feat/gbrain-degraded`

### Description

Wire `GBrainBridge` into `KlawmbingRuntime`'s startup sequence. Handle all failure modes (brain disabled, not installed, unavailable) so the agent always works in some form.

### Implementation Plan

**Modify:** `internal/runtime/runtime.go`

**NewRuntime brain wiring:**

```go
func NewRuntime(cfg *config.Config) (*KlawmbingRuntime, error) {
    registry := tools.NewRegistry()
    llmCaller := llm.NewCaller(cfg, registry)
    sessionManager := session.NewSessionManager(cfg.Session)

    var bridge *brain.GBrainBridge
    brainStatus := "disabled"

    if cfg.Brain.Enabled {
        bridge = brain.NewGBrainBridge(cfg.Brain, registry)
        if err := bridge.Start(context.Background()); err != nil {
            slog.Warn("GBrain failed to start — running in degraded mode", "error", err)
            brainStatus = "degraded (unavailable)"
        } else {
            brainStatus = fmt.Sprintf("connected (%d tools)", len(registry.Definitions()))
        }
    }

    // ... assemble cold opener, assembler, etc.
    slog.Info("Klawmbing initialised",
        "brain", brainStatus,
        "skills_dir", cfg.Skills.Dir,
    )

    return &KlawmbingRuntime{...}, nil
}
```

**Startup banner format:**
```
INFO Klawmbing started adapters=CLI brain="connected (32 tools)" skills=2
```

**Degraded mode rules:**
- `brain.enabled = false` → `GBrainBridge` never created; tools not registered; LLM operates without brain
- Brain starts but crashes mid-session → `available.Store(false)` → `gbrain_*` handlers return error string → LLM tells user brain is unavailable
- Brain auto-restarts → `available.Store(true)` → tools work again without restarting Klawmbing

**LLM behavior in degraded mode** — the system prompt (AGENTS.md) must include:

> If a gbrain tool returns "Brain is disconnected", tell the operator that the brain is currently unavailable and answer from your own knowledge where possible.

### Acceptance Criteria

- [ ] `brain.enabled = false` → startup succeeds, banner: `"Brain: disabled"`
- [ ] `gbrain` not in PATH → startup succeeds with `slog.Warn`, banner: `"Brain: degraded (unavailable)"`
- [ ] Brain goes down mid-session → next message returns response (without brain), no crash
- [ ] Brain comes back (auto-restart) → `gbrain_*` tools work again, no Klawmbing restart needed
- [ ] Degraded warning logged at most once per 60s (not on every tool call)
- [ ] `go test ./internal/runtime/... -run TestDegraded` passes

### Testing Plan

```go
func TestRuntime_BrainDisabled(t *testing.T) {
    cfg := &config.Config{Brain: config.BrainConfig{Enabled: false}}
    rt, err := runtime.NewRuntime(cfg)
    assert err == nil
    // Send message, assert response received
}

func TestRuntime_BrainUnavailableMidSession(t *testing.T) {
    // Start runtime with mock bridge (initially available)
    // bridge.available.Store(false)
    // Send message → agent responds (gracefully degrades)
    // Assert no panic
}
```
