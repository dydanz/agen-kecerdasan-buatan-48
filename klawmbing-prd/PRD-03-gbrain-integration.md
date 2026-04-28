# PRD-03: GBrain Integration via MCP

**Status:** Draft v1.0
**Parent:** PRD-00 (Klawmbing Master PRD)
**Author:** Dandi
**Created:** April 26, 2026
**Dependencies:** PRD-01 (Core Runtime — tool registry)
**Estimated Effort:** 2-3 days

---

## 1. Problem

Klawmbing needs a brain — a persistent knowledge store where the agent can query past context ("What do I know about our staging cluster?") and store new facts ("Remember that the database port changed to 5433"). Without a brain, every conversation starts from zero and no knowledge compounds.

GBrain is our chosen brain. It exposes 30+ tools via MCP (Model Context Protocol). This PRD covers connecting Klawmbing to GBrain via MCP and registering GBrain's tools in Klawmbing's tool registry so the LLM can invoke them naturally.

---

## 2. Goals

- **G1:** Klawmbing starts a GBrain MCP server on startup and maintains the connection
- **G2:** GBrain's MCP tools are registered in Klawmbing's tool registry and available to the LLM
- **G3:** The operator can ask "What do you know about X?" and get a gbrain-backed answer
- **G4:** The operator can say "Remember that Y" and have it stored in gbrain
- **G5:** If GBrain is unavailable, Klawmbing continues operating (degraded mode) — it just can't access memory

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
| Klawmbing Runtime | Manages MCP connection, dispatches tool calls |
| GBrain MCP Server | Provides search, put, get, query, graph tools via MCP |
| Claude API | Decides when to invoke gbrain tools based on the conversation |

---

## 5. User Stories

| ID | Story | Acceptance Criteria |
|----|-------|-------------------|
| US-B01 | As an operator, when Klawmbing starts, GBrain is automatically connected | Startup log shows "GBrain connected: N tools available" |
| US-B02 | As an operator, I ask "What do you know about our staging cluster?" and the agent searches gbrain | Agent invokes `gbrain_search` tool, returns relevant results from brain |
| US-B03 | As an operator, I say "Remember that our staging cluster is in ap-southeast-1" and it's stored | Agent invokes `gbrain_put` tool, confirms storage, fact is retrievable in future sessions |
| US-B04 | As an operator, if GBrain crashes, Klawmbing tells me and continues working without memory | Chat shows "Brain disconnected — operating without memory" and responses continue (without brain context) |
| US-B05 | As an operator, I can query the brain's page count or health | Agent can invoke `gbrain_stats` or similar diagnostic tool |

---

## 6. Technical Design

### 6.1 MCP Connection Architecture

```
┌────────────────────┐       stdio        ┌──────────────────┐
│   Klawmbing        │◄────────────────►  │   GBrain MCP     │
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
- Lifecycle: Klawmbing manages GBrain as a child process — starts it, monitors it, restarts if crashed
- Latency: subprocess stdio is faster than localhost HTTP

### 6.2 MCP Client Implementation

```python
class MCPClient:
    """
    Generic MCP client over stdio transport.
    Manages the subprocess, sends JSON-RPC requests, handles responses.
    """

    def __init__(self, command: str, args: list[str] = None):
        self.command = command
        self.args = args or []
        self.process: asyncio.subprocess.Process | None = None
        self._request_id = 0
        self._pending: dict[int, asyncio.Future] = {}

    async def start(self) -> None:
        """
        Start the MCP server as a subprocess.
        1. Spawn process with stdin/stdout pipes
        2. Start background reader for stdout
        3. Send 'initialize' request
        4. Verify capabilities
        """

    async def stop(self) -> None:
        """Send shutdown notification, terminate process."""

    async def list_tools(self) -> list[dict]:
        """
        Call tools/list to discover available tools.
        Returns list of tool definitions with name, description, inputSchema.
        """

    async def call_tool(self, name: str, arguments: dict) -> str:
        """
        Call tools/call with the given tool name and arguments.
        Returns the tool result as a string.
        """

    async def _send_request(self, method: str, params: dict = None) -> dict:
        """Send a JSON-RPC 2.0 request and await the response."""

    async def _read_loop(self) -> None:
        """
        Background task: read lines from subprocess stdout,
        parse JSON-RPC responses, resolve pending futures.
        """

    async def _health_check(self) -> bool:
        """Ping the server. Return False if unresponsive."""

    async def _restart(self) -> None:
        """Kill and restart the subprocess. Re-discover tools."""
```

### 6.3 GBrain Bridge

```python
class GBrainBridge:
    """
    Bridges GBrain MCP tools into Klawmbing's tool registry.
    Handles tool discovery, registration, and health monitoring.
    """

    def __init__(self, config: BrainConfig, tool_registry: ToolRegistry):
        self.mcp = MCPClient(
            command=config.gbrain_command,  # "gbrain"
            args=["serve"]
        )
        self.registry = tool_registry
        self.connected = False

    async def connect(self) -> None:
        """
        1. Start MCP client (spawns gbrain serve)
        2. Discover tools via tools/list
        3. Register each tool in Klawmbing's ToolRegistry
        4. Set self.connected = True
        5. Log: "GBrain connected: {N} tools available"
        """
        await self.mcp.start()
        tools = await self.mcp.list_tools()

        for tool in tools:
            self.registry.register(
                name=f"gbrain_{tool['name']}",  # Prefix to namespace
                description=tool['description'],
                input_schema=tool['inputSchema'],
                handler=self._make_handler(tool['name'])
            )

        self.connected = True
        logger.info(f"GBrain connected: {len(tools)} tools available")

    def _make_handler(self, tool_name: str) -> Callable:
        """Create a handler closure that calls MCP for a specific tool."""
        async def handler(input: dict) -> str:
            if not self.connected:
                return "Error: Brain is disconnected"
            return await self.mcp.call_tool(tool_name, input)
        return handler

    async def disconnect(self) -> None:
        """Stop MCP client, mark as disconnected."""

    async def monitor(self) -> None:
        """
        Background task: health-check every 30s.
        If unresponsive, attempt restart.
        If restart fails 3 times, mark as permanently disconnected.
        """
```

### 6.4 Expected GBrain MCP Tools

Based on GBrain's documentation, the following tools will be available after connection. Klawmbing registers all of them but the most critical for "hello world" are marked:

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

**Note:** The actual tool names and schemas will be discovered dynamically via `tools/list`. The table above is based on GBrain v0.12 documentation and may differ. The dynamic discovery pattern means Klawmbing doesn't hardcode any GBrain-specific knowledge — it adapts to whatever tools GBrain exposes.

### 6.5 Degraded Mode (Brain Disconnected)

If GBrain is unavailable:
1. All `gbrain_*` tool handlers return `"Error: Brain is disconnected. Operating without memory."`
2. The LLM receives this error and can explain to the user that memory is unavailable
3. Klawmbing continues processing messages normally — just without brain-backed context
4. A warning is logged every 60 seconds: "GBrain disconnected — operating in degraded mode"
5. Background monitor attempts to reconnect every 30 seconds

---

## 7. Configuration

```toml
[brain]
enabled = true
mcp_transport = "stdio"                   # Only stdio supported in Phase 1
gbrain_command = "gbrain"                 # Command to run (must be in PATH)
gbrain_args = ["serve"]                   # Arguments
gbrain_working_dir = "~/brain"            # Working directory for gbrain
health_check_interval_s = 30              # How often to check health
max_restart_attempts = 3                  # Before giving up
tool_prefix = "gbrain"                    # Prefix for registered tool names
```

---

## 8. Setup Prerequisites

Before Klawmbing can connect to GBrain, the operator must:

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

## 9. Error Handling

| Error | Handling | User-facing |
|-------|----------|-------------|
| GBrain not installed | Startup warning, brain disabled | "Brain unavailable — memory features disabled" |
| GBrain process crashes | Auto-restart (up to 3 attempts) | Brief pause, then resumes. If all retries fail: "Brain disconnected" |
| MCP request timeout (>10s) | Cancel request, return error to LLM | LLM explains "brain query timed out" |
| Invalid tool response from GBrain | Log error, return error string to LLM | LLM handles gracefully |
| Brain data directory missing | Startup error, suggest `gbrain init` | "Brain directory ~/brain not found. Run: gbrain init" |

---

## 10. Acceptance Criteria

- [ ] `gbrain serve` is started automatically when Klawmbing starts (if brain.enabled = true)
- [ ] Startup log shows "GBrain connected: N tools available" with actual tool count
- [ ] Operator asks "What do you know about X?" → agent invokes `gbrain_search` → returns results
- [ ] Operator says "Remember that Y" → agent invokes `gbrain_put` → confirms stored
- [ ] Stored fact is retrievable in a new session (restart Klawmbing, ask again)
- [ ] Killing the gbrain process triggers auto-restart within 30s
- [ ] After 3 failed restarts, degraded mode is entered with clear user notification
- [ ] With brain disabled in config, Klawmbing starts and operates without brain tools

---

## 11. Open Questions

| ID | Question | Default |
|----|----------|---------|
| TODO-B01 | Exact GBrain installation steps may vary by version. Need to test with latest stable. | Test and document during implementation. |
| TODO-B02 | Should Klawmbing manage GBrain's PostgreSQL via Docker Compose, or expect the operator to manage it separately? | Separate management. GBrain's own docs cover Postgres setup. Klawmbing shouldn't own GBrain's infra. |
| TODO-B03 | Tool name collision: if GBrain exposes a tool called "search" and we later add a web search tool also called "search," they'll collide. The prefix (`gbrain_search`) prevents this, but is the prefix the right approach? | Yes, prefix all GBrain tools with `gbrain_`. Consistent, unambiguous, no collision risk. |
| TODO-B04 | Should Klawmbing always include brain context in the system prompt (pre-fetch relevant pages), or let the LLM decide when to search? | Let the LLM decide (tool call). Pre-fetching adds latency and tokens to every message. The LLM is good at deciding when it needs context. Revisit if retrieval quality is poor. |
| TODO-B05 | GBrain brain directory: `~/brain/` or `~/.klawmbing/brain/`? Separate lifecycle vs co-located management. | `~/brain/` — the brain has its own lifecycle, independent of the claw runtime. Multiple claws could theoretically share a brain. |
