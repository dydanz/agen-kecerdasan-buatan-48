# PRD-01: Core Runtime & Message Loop

**Status:** Draft v1.0
**Parent:** PRD-00 (Klawmbing Master PRD)
**Author:** Dandi
**Created:** April 26, 2026
**Dependencies:** None (this is the foundation)
**Estimated Effort:** 2-3 days

---

## 1. Problem

Klawmbing needs a core process that starts up, loads configuration, accepts messages from any channel adapter, sends them to the Claude API, executes tool calls in the response, and returns results. Without this core loop, nothing else works.

This PRD covers the minimum viable runtime: the entry point, configuration loader, LLM caller with streaming, tool execution dispatcher, and the interface contracts that all other components (adapters, skills, sessions) plug into.

---

## 2. Goals

- **G1:** A single `python klawmbing.py` command starts the entire runtime
- **G2:** The runtime loads configuration from a TOML file (API keys, model config, adapter toggles)
- **G3:** The LLM caller sends messages to the Claude API and streams tokens back
- **G4:** Tool calls in the LLM response are dispatched to registered tool handlers
- **G5:** The runtime defines clean interface contracts for adapters, tools, and post-turn hooks
- **G6:** Errors in any component do not crash the main process

## 3. Non-Goals

- Specific adapter implementations (PRD-02)
- GBrain/MCP connection (PRD-03)
- Skill resolution and context assembly (PRD-04)
- Session persistence (PRD-05)
- Cron scheduling (Phase 1, Week 4)
- Multi-process / multi-worker architecture

---

## 4. Actors

| Actor | Role in this PRD |
|-------|-----------------|
| Operator | Starts the runtime, provides config, sends test messages via CLI adapter |
| Klawmbing Runtime | Loads config, initializes components, runs the message loop |
| Claude API | Receives prompts, returns streaming responses with optional tool calls |
| Tool Handlers | Execute side effects (gbrain, shell, web search) when the LLM requests them |

---

## 5. User Stories

| ID | Story | Acceptance Criteria |
|----|-------|-------------------|
| US-R01 | As an operator, I run `python klawmbing.py` and the process starts without errors | Process starts, logs "Klawmbing started" with loaded config summary (model, adapters enabled) |
| US-R02 | As an operator, I provide a config.toml with my Anthropic API key and model preferences | Runtime reads config, validates required fields, fails fast with clear error if API key is missing |
| US-R03 | As an operator, I send a message through any adapter and receive a streamed response | First token arrives in < 2s, full response streams to the adapter's `send()` method |
| US-R04 | As an operator, if the LLM returns a tool_use block, the registered handler is called | Tool handler receives the tool name + input, returns result, and the LLM continues with the tool result |
| US-R05 | As an operator, if a tool handler throws an exception, the error is caught and reported gracefully | Error message returned to chat, process continues running, error logged to tool-calls.jsonl |
| US-R06 | As an operator, I can see all LLM calls and tool invocations in a log file | tool-calls.jsonl contains: timestamp, session_id, prompt hash, tool name, input, output, tokens used, latency |

---

## 6. Technical Design

### 6.1 Directory Structure (this PRD's scope)

```
~/.klawmbing/
├── klawmbing.py           # Entry point
├── config.toml            # Configuration
├── core/
│   ├── __init__.py
│   ├── config.py          # Config loader + validation
│   ├── runtime.py         # Main loop, component orchestration
│   ├── llm.py             # Claude API caller with streaming
│   ├── tools.py           # Tool registry + dispatcher
│   └── types.py           # Shared data types (Message, ToolCall, etc.)
├── adapters/
│   └── base.py            # ChannelAdapter abstract interface
├── logs/
│   └── tool-calls.jsonl   # Audit trail
└── identity/              # Created empty, populated by PRD-04
```

### 6.2 Configuration Schema (config.toml)

```toml
[klawmbing]
name = "Klawmbing"
log_level = "INFO"                    # DEBUG | INFO | WARNING | ERROR

[llm]
provider = "anthropic"
model = "claude-sonnet-4-6-20260326"  # Primary model
extraction_model = "claude-haiku-4-5-20251001"  # Cheap model for extraction
max_tokens = 8192
temperature = 0.7
api_key_env = "ANTHROPIC_API_KEY"     # Read from env var, never stored in file

[adapters]
cli_enabled = true
telegram_enabled = false              # Enabled in PRD-02
telegram_token_env = "TELEGRAM_BOT_TOKEN"
discord_enabled = false               # Future
discord_token_env = "DISCORD_BOT_TOKEN"

[brain]
enabled = false                       # Enabled in PRD-03
mcp_transport = "stdio"              # "stdio" | "sse"
gbrain_command = "gbrain serve"      # Command to start MCP server

[session]
storage_dir = "sessions"
max_turns_before_compaction = 30      # Phase 2

[skills]
skills_dir = "skills"
identity_dir = "identity"
```

**Validation rules:**
- `api_key_env` must resolve to a non-empty environment variable
- If `telegram_enabled = true`, `telegram_token_env` must resolve
- `model` must be a valid Anthropic model string
- Fail fast on startup with clear error messages for any invalid config

### 6.3 Core Data Types (types.py)

```python
@dataclass
class Message:
    """Normalized message from any channel adapter."""
    id: str                    # Unique message ID (UUID)
    session_id: str            # Session this message belongs to
    channel: str               # "telegram" | "discord" | "cli"
    sender: str                # User identifier
    content: str               # Message text
    timestamp: datetime        # When received
    attachments: list[str]     # File paths (future)
    metadata: dict             # Channel-specific extras

@dataclass
class Response:
    """Response from the LLM, potentially with tool calls."""
    content: str               # Text response
    tool_calls: list[ToolCall] # Tool invocations requested by LLM
    usage: TokenUsage          # Token counts
    model: str                 # Model that generated this

@dataclass
class ToolCall:
    """A single tool invocation."""
    id: str                    # Tool use ID from Claude API
    name: str                  # Tool name
    input: dict                # Tool input parameters
    idempotency_key: str       # UUID for dedup (generated by runtime)

@dataclass
class ToolResult:
    """Result of executing a tool."""
    tool_call_id: str
    output: str                # Stringified result
    is_error: bool             # Whether the tool failed
    duration_ms: int           # Execution time

@dataclass
class TokenUsage:
    input_tokens: int
    output_tokens: int
    cache_read_tokens: int     # Prompt caching hits
    cache_write_tokens: int
```

### 6.4 Channel Adapter Interface (adapters/base.py)

```python
class ChannelAdapter(ABC):
    """
    Normalized interface for all chat platforms.
    Each platform implements this contract.
    """

    @abstractmethod
    async def start(self) -> None:
        """Start listening for messages. Called once at startup."""

    @abstractmethod
    async def stop(self) -> None:
        """Graceful shutdown."""

    @abstractmethod
    async def send(self, session_id: str, content: str,
                   attachments: list[str] | None = None) -> None:
        """Send a message back to the user."""

    @abstractmethod
    async def send_streaming(self, session_id: str,
                             token_generator) -> None:
        """Stream tokens back to the user as they arrive."""

    def set_message_handler(self, handler: Callable) -> None:
        """Register the callback for incoming messages.
        handler signature: async def handle(message: Message) -> None
        """
        self._message_handler = handler
```

### 6.5 Tool Registry (tools.py)

```python
class ToolRegistry:
    """
    Registry of available tools that the LLM can invoke.
    Each tool has a name, description (for the LLM), and handler function.
    """

    def register(self, name: str, description: str,
                 input_schema: dict, handler: Callable) -> None:
        """Register a tool. Called at startup by each component."""

    def get_tool_definitions(self) -> list[dict]:
        """Return tool definitions in Claude API format."""

    async def execute(self, tool_call: ToolCall) -> ToolResult:
        """
        Execute a tool call with:
        1. Idempotency check (skip if already executed)
        2. Timeout enforcement
        3. Error wrapping
        4. Audit logging
        """

    def _log_tool_call(self, tool_call: ToolCall,
                       result: ToolResult) -> None:
        """Append to tool-calls.jsonl."""
```

### 6.6 LLM Caller (llm.py)

```python
class LLMCaller:
    """
    Wraps the Anthropic Python SDK.
    Handles: streaming, tool use loops, token tracking.
    """

    async def call(self, messages: list[dict],
                   system_prompt: str,
                   tools: list[dict] | None = None,
                   model: str | None = None) -> AsyncGenerator:
        """
        Send messages to Claude API with streaming.

        Yields:
        - TextDelta events (for streaming to chat)
        - ToolCall events (for tool execution)
        - Final Response (when complete)

        Handles the tool use loop internally:
        1. Send messages to Claude
        2. If response contains tool_use, execute via ToolRegistry
        3. Append tool_result to messages
        4. Call Claude again with results
        5. Repeat until Claude returns end_turn
        """

    async def call_extraction(self, content: str,
                               prompt: str) -> str:
        """
        Use the cheap extraction model (Haiku) for
        fact extraction, summarization, etc.
        Non-streaming, returns full text.
        """
```

**Critical implementation notes:**
- Use `anthropic.AsyncAnthropic` for async operation
- Set `stream=True` for all primary calls
- Track token usage across the full tool-use loop (sum all turns)
- Implement exponential backoff on rate limit errors (429)
- Timeout: 120 seconds per API call (long-running tool loops can take time)

### 6.7 Runtime Orchestration (runtime.py)

```python
class KlawmbingRuntime:
    """
    The main orchestrator. Wires everything together.
    """

    async def start(self):
        """
        1. Load config
        2. Initialize LLM caller
        3. Initialize tool registry
        4. Start MCP connections (PRD-03)
        5. Load identity files (PRD-04)
        6. Start enabled channel adapters
        7. Log startup summary
        """

    async def handle_message(self, message: Message) -> None:
        """
        The core loop — called by channel adapters on every incoming message.

        1. Resolve session (PRD-05)
        2. Assemble context (PRD-04: identity + skill + session history + brain context)
        3. Call LLM with assembled context
        4. Stream response tokens to adapter
        5. If tool calls: execute, continue LLM loop
        6. Post-turn hook: persist session, extract facts (PRD-05)
        7. Log completion metrics
        """

    async def shutdown(self):
        """Graceful shutdown: stop adapters, flush logs, close connections."""
```

### 6.8 Entry Point (klawmbing.py)

```python
"""
Klawmbing entry point.

Usage:
    python klawmbing.py                    # Start with default config
    python klawmbing.py --config path.toml # Start with custom config
    python klawmbing.py --validate         # Validate config and exit
"""

async def main():
    config = load_config(args.config)
    runtime = KlawmbingRuntime(config)

    # Graceful shutdown on SIGINT/SIGTERM
    loop = asyncio.get_event_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(sig, lambda: asyncio.create_task(runtime.shutdown()))

    await runtime.start()

if __name__ == "__main__":
    asyncio.run(main())
```

---

## 7. Error Handling Strategy

| Error Type | Handling | User-Facing |
|------------|----------|-------------|
| Config validation failure | Fail fast, print error, exit(1) | "Missing ANTHROPIC_API_KEY environment variable" |
| Claude API rate limit (429) | Exponential backoff (1s, 2s, 4s, 8s, max 30s), 3 retries | "Processing... (retry N/3)" |
| Claude API error (500) | Retry once after 2s, then report | "Something went wrong with the AI service. Try again." |
| Tool handler exception | Catch, log full traceback, return error as tool result | LLM sees error and can retry or explain |
| Adapter connection lost | Log warning, attempt reconnect every 10s | No immediate user message (they'll see the bot go offline) |
| Unhandled exception in message handler | Catch at top level, log, send error to chat | "An unexpected error occurred. Check logs." |

---

## 8. Logging & Observability

### tool-calls.jsonl format

```json
{
  "timestamp": "2026-04-26T14:30:00Z",
  "session_id": "main:telegram:123456",
  "message_id": "uuid-here",
  "type": "llm_call",
  "model": "claude-sonnet-4-6-20260326",
  "input_tokens": 2340,
  "output_tokens": 512,
  "cache_read_tokens": 1800,
  "latency_ms": 1450,
  "tool_calls": [
    {
      "name": "gbrain_search",
      "input": {"query": "staging cluster"},
      "output_length": 340,
      "is_error": false,
      "duration_ms": 230,
      "idempotency_key": "uuid-here"
    }
  ]
}
```

### Console logging

- Startup: config summary, enabled adapters, tool count
- Per-message: session_id, token usage, latency, tool call count
- Errors: full context including message content (truncated to 200 chars)

---

## 9. Acceptance Criteria (Definition of Done)

- [ ] `python klawmbing.py` starts without errors with valid config.toml
- [ ] `python klawmbing.py --validate` checks config and exits cleanly
- [ ] Missing API key causes immediate, clear error message
- [ ] CLI adapter (implemented as the simplest possible adapter in PRD-02) can send a message and receive a streamed response
- [ ] If a tool handler is registered and the LLM invokes it, the handler runs and the LLM receives the result
- [ ] If a tool handler throws, the error is caught, logged, and the process continues
- [ ] tool-calls.jsonl is written after every LLM call
- [ ] SIGINT/SIGTERM triggers graceful shutdown (flush logs, close connections)
- [ ] The process runs for 1 hour without memory leaks or crashes under idle + periodic message load

---

## 10. Open Questions

| ID | Question | Default |
|----|----------|---------|
| TODO-R01 | Should we use `pydantic` for config validation or keep it stdlib-only? Pydantic adds a dependency but gives schema validation for free. | Use pydantic. Single dependency, huge validation quality gain. |
| TODO-R02 | Prompt caching: enable from day 1 or add later? Claude supports cache_control blocks that can reduce costs 90% for repeated system prompts. | Enable from day 1. The system prompt (AGENTS.md + SOUL.md) is identical across turns — perfect cache candidate. |
| TODO-R03 | Should tool-calls.jsonl be rotated? At scale it could grow large. | Rotate daily. Keep 30 days. Implement in Phase 2. |
