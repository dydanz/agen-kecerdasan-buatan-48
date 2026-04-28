# PRD-05: Session Management & Persistence

**Status:** Draft v1.0
**Parent:** PRD-00 (Klawmbing Master PRD)
**Author:** Dandi
**Created:** April 26, 2026
**Dependencies:** PRD-01 (Core Runtime), PRD-02 (Channel Adapters — session IDs)
**Estimated Effort:** 2-3 days

---

## 1. Problem

Without session management, every message is independent — the agent has no memory of what was said 5 minutes ago. Without persistence, restarting Klawmbing erases all conversation context. Without post-turn hooks, there's no place to extract facts for the brain or trigger compaction.

This PRD covers:
- Session resolution (which session does this message belong to?)
- Session state (conversation history maintained in memory)
- Session persistence (JSONL files that survive restarts)
- Post-turn hooks (extensible processing after each agent response)

---

## 2. Goals

- **G1:** Each message is associated with a session that maintains conversation history
- **G2:** Session history is sent to the LLM as message context (the LLM "remembers" the conversation)
- **G3:** Sessions persist to disk as JSONL files and survive process restarts
- **G4:** On startup, Klawmbing loads the most recent session per session ID and resumes
- **G5:** Post-turn hooks run after every agent response (persist session, log metrics)
- **G6:** Session state is bounded — old turns are available but the architecture supports future compaction

## 3. Non-Goals

- Session compaction / summarization (Phase 2 — requires memory flush to brain first)
- Memory flush / fact extraction from sessions (Phase 2)
- Multi-session management (switching between sessions in one chat)
- Session sharing between adapters (CLI session ≠ Telegram session)
- Session encryption

---

## 4. User Stories

| ID | Story | Acceptance Criteria |
|----|-------|-------------------|
| US-P01 | As an operator, I send multiple messages and the agent remembers what I said earlier in the conversation | Message 1: "Our database is Postgres 16." Message 5: "What database are we using?" → "Postgres 16." |
| US-P02 | As an operator, I restart Klawmbing and my conversation context is preserved | After restart, ask "What did I just tell you about the database?" → Agent recalls from loaded session. |
| US-P03 | As an operator, my Telegram session and CLI session are independent | Facts shared in CLI don't appear in Telegram session history (they may appear via brain if stored). |
| US-P04 | As a developer, I can inspect a session file to see the full conversation history | JSONL file contains human-readable entries with role, content, timestamp, and tool calls. |
| US-P05 | As a developer, post-turn hooks run reliably after every response | Hook logs confirm execution. If a hook fails, the error is logged but doesn't affect the user response. |

---

## 5. Technical Design

### 5.1 Session Data Model

```python
@dataclass
class SessionTurn:
    """A single turn in a conversation (user message + assistant response)."""
    turn_id: str                    # UUID
    timestamp: datetime
    user_message: Message           # Incoming message
    assistant_response: str         # Agent's text response
    tool_calls: list[ToolCall]      # Tools invoked during this turn
    tool_results: list[ToolResult]  # Results from tool invocations
    token_usage: TokenUsage         # Tokens consumed
    skill_used: str | None          # Which skill was resolved (or None)
    latency_ms: int                 # End-to-end latency

@dataclass
class Session:
    """A conversation session with persistent state."""
    session_id: str                 # e.g., "main:telegram:123456789"
    created_at: datetime
    updated_at: datetime
    turns: list[SessionTurn]        # Full conversation history
    metadata: dict                  # Arbitrary session metadata
    compacted_summary: str | None   # Summary of compacted turns (Phase 2)

    def to_messages(self) -> list[dict]:
        """
        Convert session history to Claude API message format.
        Returns list of {"role": "user"/"assistant", "content": "..."} dicts.
        Used by the LLM caller to provide conversation context.
        """

    def add_turn(self, turn: SessionTurn) -> None:
        """Append a turn and update updated_at."""

    @property
    def turn_count(self) -> int:
        """Number of turns in this session."""

    @property
    def is_compaction_needed(self) -> bool:
        """True if turn count exceeds threshold (Phase 2)."""
```

### 5.2 Session Manager

```python
class SessionManager:
    """
    Manages session lifecycle: creation, loading, persistence, resolution.
    """

    def __init__(self, config: SessionConfig):
        self.storage_dir = config.storage_dir
        self.sessions: dict[str, Session] = {}  # In-memory cache
        self.max_turns_in_context = config.max_turns_in_context  # default: 50

    def resolve_or_create(self, message: Message) -> Session:
        """
        Get the existing session for this message's session_id,
        or create a new one if none exists.

        1. Check in-memory cache
        2. If not cached, try loading from disk
        3. If not on disk, create new session
        """

    def get_context_messages(self, session: Session) -> list[dict]:
        """
        Get the message history for the LLM context.

        Returns the most recent N turns as Claude API message format.
        If session has a compacted_summary, prepend it as context.

        Phase 1: Return ALL turns (up to max_turns_in_context).
        Phase 2: Return compacted_summary + recent turns.
        """

    async def persist(self, session: Session) -> None:
        """
        Write the session to a JSONL file.
        Each call appends the latest turn (not the full history).
        On first persist, creates the file.
        """

    def load_from_disk(self, session_id: str) -> Session | None:
        """
        Load a session from its JSONL file.
        Returns None if file doesn't exist.
        """

    def load_all_active(self) -> dict[str, Session]:
        """
        On startup, load the most recently modified session file
        per unique session_id. Cache in memory.
        """
```

### 5.3 JSONL File Format

Each session is stored as a JSONL file: one JSON object per line.

**File location:** `~/.klawmbing/sessions/{session_id_sanitized}.jsonl`

Session ID sanitization: replace `:` with `_` → `main_telegram_123456789.jsonl`

**JSONL line format:**

```json
{"type": "session_created", "session_id": "main:telegram:123456789", "created_at": "2026-04-26T14:00:00Z"}
```

```json
{
  "type": "turn",
  "turn_id": "uuid-here",
  "timestamp": "2026-04-26T14:00:05Z",
  "user": {
    "content": "Remember that our database port is 5433",
    "channel": "telegram",
    "sender": "123456789"
  },
  "assistant": {
    "content": "Stored. Your database runs on port 5433.",
    "skill_used": "note-capture",
    "tool_calls": [
      {
        "name": "gbrain_put",
        "input": {"title": "Database Configuration", "content": "..."},
        "output": "Page created: Database Configuration",
        "duration_ms": 230
      }
    ]
  },
  "token_usage": {"input": 1240, "output": 45, "cache_read": 800},
  "latency_ms": 2100
}
```

**Design decisions:**
- Append-only: each turn is one line, appended to the file. Never rewrite the entire file.
- Human-readable: pretty enough to read with `cat` or `jq`.
- Crash-safe: if the process dies mid-turn, the file has all completed turns. The incomplete turn is simply lost.
- Size guard: log a warning if any session file exceeds 10MB (Phase 2: trigger compaction).

### 5.4 Session Resolution

Session IDs are determined by the channel adapter (defined in PRD-02):

| Source | Session ID | Behavior |
|--------|-----------|----------|
| CLI | `main:cli:local` | Always the same session. Resumes on restart. |
| Telegram DM (operator) | `main:telegram:{user_id}` | One persistent session per operator account. |
| Telegram group (future) | `group:telegram:{chat_id}` | One session per group. Sandboxed. |
| Discord DM (future) | `main:discord:{user_id}` | One session per user. |

**Resolution logic:**
```python
def resolve_or_create(self, message: Message) -> Session:
    session_id = message.session_id  # Set by adapter

    # Check memory cache
    if session_id in self.sessions:
        return self.sessions[session_id]

    # Try loading from disk
    session = self.load_from_disk(session_id)
    if session:
        self.sessions[session_id] = session
        logger.info(f"Session loaded from disk: {session_id} ({session.turn_count} turns)")
        return session

    # Create new session
    session = Session(
        session_id=session_id,
        created_at=datetime.now(UTC),
        updated_at=datetime.now(UTC),
        turns=[],
        metadata={},
        compacted_summary=None
    )
    self.sessions[session_id] = session
    logger.info(f"New session created: {session_id}")
    return session
```

### 5.5 Context Window Management

The `get_context_messages` method converts session history to Claude API messages. It needs to respect context window limits.

**Phase 1 strategy (simple):**
- Include the most recent `max_turns_in_context` turns (default: 50)
- Each turn becomes a user message + assistant message pair
- Tool calls are included as tool_use/tool_result content blocks
- If turn count exceeds limit, silently drop oldest turns

**Phase 2 strategy (compaction):**
- When turn count exceeds `max_turns_before_compaction` (default: 30)
- Run memory flush (extract facts to gbrain)
- Summarize oldest 50% of turns into a paragraph
- Store summary as `compacted_summary`
- Delete raw turns that were summarized
- Prepend summary to context on subsequent turns

```python
def get_context_messages(self, session: Session) -> list[dict]:
    messages = []

    # Prepend compacted summary if it exists
    if session.compacted_summary:
        messages.append({
            "role": "user",
            "content": f"[Previous conversation summary: {session.compacted_summary}]"
        })
        messages.append({
            "role": "assistant",
            "content": "I understand the context from our previous conversation. How can I help?"
        })

    # Add recent turns
    recent_turns = session.turns[-self.max_turns_in_context:]
    for turn in recent_turns:
        messages.append({"role": "user", "content": turn.user_message.content})

        # If there were tool calls, include them properly
        if turn.tool_calls:
            # Build assistant content with tool_use blocks
            assistant_content = self._build_tool_use_messages(turn)
            messages.extend(assistant_content)
        else:
            messages.append({"role": "assistant", "content": turn.assistant_response})

    return messages
```

### 5.6 Post-Turn Hooks

Post-turn hooks are functions that execute after every agent response. They are fire-and-forget — failures in hooks do not affect the user-facing response.

```python
class PostTurnHookManager:
    """
    Manages hooks that run after each agent turn.
    Hooks are async functions that receive the session and the latest turn.
    """

    def __init__(self):
        self.hooks: list[Callable] = []

    def register(self, hook: Callable) -> None:
        """Register a post-turn hook."""

    async def execute_all(self, session: Session, turn: SessionTurn) -> None:
        """
        Run all hooks concurrently. Catch and log any errors.
        Do not propagate exceptions — hooks must never break the main loop.
        """
        tasks = [self._safe_execute(hook, session, turn) for hook in self.hooks]
        await asyncio.gather(*tasks)

    async def _safe_execute(self, hook, session, turn):
        try:
            await hook(session, turn)
        except Exception as e:
            logger.error(f"Post-turn hook {hook.__name__} failed: {e}", exc_info=True)
```

**Built-in hooks (Phase 1):**

```python
# Hook 1: Persist session to disk
async def persist_session_hook(session: Session, turn: SessionTurn) -> None:
    """Append the latest turn to the session's JSONL file."""
    await session_manager.persist(session)

# Hook 2: Log turn metrics
async def log_metrics_hook(session: Session, turn: SessionTurn) -> None:
    """Log token usage, latency, skill used, tool calls to tool-calls.jsonl."""
    log_entry = {
        "timestamp": turn.timestamp.isoformat(),
        "session_id": session.session_id,
        "turn_id": turn.turn_id,
        "tokens": asdict(turn.token_usage),
        "latency_ms": turn.latency_ms,
        "skill_used": turn.skill_used,
        "tool_call_count": len(turn.tool_calls),
    }
    append_jsonl("logs/tool-calls.jsonl", log_entry)
```

**Future hooks (Phase 2):**
```python
# Hook 3: Memory flush (extract facts to brain)
async def memory_flush_hook(session, turn):
    """Extract structured facts from the turn and store in gbrain."""

# Hook 4: Compaction check
async def compaction_check_hook(session, turn):
    """If turn count exceeds threshold, trigger compaction."""
```

---

## 6. Integration with Core Runtime (PRD-01)

The `handle_message` method in runtime.py uses all components together:

```python
async def handle_message(self, message: Message) -> None:
    """
    The core loop — ties PRD-01 through PRD-05 together.
    """

    # 1. Session resolution (PRD-05)
    session = self.session_manager.resolve_or_create(message)

    # 2. Context assembly (PRD-04)
    context_messages = self.session_manager.get_context_messages(session)
    system_prompt = self.context_assembler.assemble(
        message=message,
        session_context=session.compacted_summary,
        brain_context=None  # Let LLM decide to search brain via tools
    )

    # 3. Append user message to context
    context_messages.append({"role": "user", "content": message.content})

    # 4. Call LLM with streaming (PRD-01)
    full_response = ""
    tool_calls = []
    tool_results = []
    start_time = time.monotonic()

    adapter = self.get_adapter(message.channel)

    async for event in self.llm.call(
        messages=context_messages,
        system_prompt=system_prompt,
        tools=self.tool_registry.get_tool_definitions()
    ):
        if isinstance(event, TextDelta):
            # Stream token to adapter (PRD-02)
            await adapter.send_streaming_token(message.session_id, event.text)
            full_response += event.text
        elif isinstance(event, ToolCallEvent):
            tool_calls.append(event.tool_call)
            result = await self.tool_registry.execute(event.tool_call)
            tool_results.append(result)
        elif isinstance(event, FinalResponse):
            usage = event.usage

    latency_ms = int((time.monotonic() - start_time) * 1000)

    # 5. Create turn record
    turn = SessionTurn(
        turn_id=str(uuid4()),
        timestamp=datetime.now(UTC),
        user_message=message,
        assistant_response=full_response,
        tool_calls=tool_calls,
        tool_results=tool_results,
        token_usage=usage,
        skill_used=self.context_assembler.last_skill_used,
        latency_ms=latency_ms
    )

    # 6. Add turn to session
    session.add_turn(turn)

    # 7. Post-turn hooks (PRD-05)
    await self.post_turn_hooks.execute_all(session, turn)
```

---

## 7. Configuration

```toml
[session]
storage_dir = "sessions"              # Relative to ~/.klawmbing/
max_turns_in_context = 50             # Max turns sent to LLM
max_turns_before_compaction = 30      # Phase 2: triggers compaction
max_file_size_mb = 10                 # Warn if session file exceeds this
load_on_startup = true                # Load most recent sessions on startup
```

---

## 8. Error Handling

| Error | Handling | User-facing |
|-------|----------|-------------|
| Session file corrupted (invalid JSON line) | Skip corrupt line, log warning, continue loading remaining lines | None (transparent recovery) |
| Disk full (can't write session) | Log error, continue operating in memory-only mode | None (session persists in memory until disk space freed) |
| Session file exceeds max_file_size_mb | Log warning, continue operating | None (warning only, no action until compaction is implemented) |
| Post-turn hook fails | Catch exception, log full traceback, continue | None (hooks never affect user response) |
| Session loading takes > 5s on startup | Log warning with file size | None (startup takes a moment, acceptable) |

---

## 9. Acceptance Criteria

- [ ] Multi-turn conversation works: agent recalls what was said earlier in the same session
- [ ] After restarting Klawmbing, the agent recalls previous conversation from loaded session
- [ ] CLI and Telegram sessions are independent (different session IDs, different histories)
- [ ] Session JSONL file contains readable, complete turn records
- [ ] Corrupt JSONL lines are skipped with a warning (don't crash on bad data)
- [ ] Post-turn hooks execute after every response
- [ ] Post-turn hook failure is logged but doesn't affect the response
- [ ] Startup log shows loaded sessions: "Loaded N sessions (M total turns)"
- [ ] Session file size warning triggers at 10MB

---

## 10. Open Questions

| ID | Question | Default |
|----|----------|---------|
| TODO-P01 | Should tool call details (full input/output) be stored in session history? They can be large (e.g., gbrain search results). | Store tool name + truncated output (first 500 chars). Full details go in tool-calls.jsonl. Keeps session files manageable. |
| TODO-P02 | Session file rotation: should we create a new file per day, or one file per session forever? Daily files are smaller but complicate loading. | One file per session. Phase 2: implement compaction to control size. |
| TODO-P03 | Should the agent see tool call history in the context? Claude's API supports tool_use/tool_result content blocks in history. Including them gives better continuity but uses more tokens. | Yes, include tool calls in context. The LLM needs to know what tools were used and what they returned to maintain coherent conversation. Truncate large tool outputs to 500 chars in session history. |
| TODO-P04 | Maximum session age: should sessions expire after N days of inactivity? | No expiry. Sessions are cheap to store. If the operator picks up a conversation after 2 weeks, the context should still be available. The brain stores durable facts; the session stores conversation flow. |
| TODO-P05 | Should there be a `/reset` command to start a fresh session? | Yes, add in Phase 2 as a slash command. For hello world, manually deleting the JSONL file achieves this. |

---

## 11. Appendix: End-to-End "Hello World" Flow

This is the complete flow when all five PRDs are implemented:

```
1. Operator runs: python klawmbing.py
   ├── config.toml loaded and validated (PRD-01)
   ├── LLM caller initialized with Anthropic SDK (PRD-01)
   ├── Tool registry created (PRD-01)
   ├── GBrain MCP server started, tools discovered and registered (PRD-03)
   ├── Identity files loaded: AGENTS.md, SOUL.md, USER.md (PRD-04)
   ├── Skills loaded: note-capture, research (PRD-04)
   ├── Active sessions loaded from disk (PRD-05)
   ├── CLI adapter started (PRD-02)
   ├── Telegram adapter started, polling (PRD-02)
   └── Log: "Klawmbing started. Adapters: CLI, Telegram. Brain: connected (32 tools). Skills: 2."

2. Operator sends via Telegram: "Hello, who are you?"
   ├── Telegram adapter receives message (PRD-02)
   ├── Message normalized to Message object (PRD-02)
   ├── Session resolved: main:telegram:123456789 (PRD-05) — new session created
   ├── Skill resolver: no match → general mode (PRD-04)
   ├── Context assembled: AGENTS.md + SOUL.md + USER.md (PRD-04)
   ├── LLM called with context + message (PRD-01)
   ├── Response streamed via editMessageText (PRD-02)
   ├── Turn persisted to sessions/main_telegram_123456789.jsonl (PRD-05)
   └── Agent: "I'm Klawmbing, your personal AI agent. I have access to a knowledge
        brain and can research, remember things, and help you think through problems.
        What are you working on?"

3. Operator sends: "Remember that our staging cluster is ap-southeast-1"
   ├── Skill resolver: "remember" matches note-capture (PRD-04)
   ├── Context assembled: identity + note-capture skill (PRD-04)
   ├── LLM called → decides to invoke gbrain_put tool (PRD-01, PRD-03)
   ├── Tool executed: gbrain_put(title="Staging Cluster", ...) (PRD-03)
   ├── LLM receives tool result, generates confirmation
   ├── Response: "Stored. Staging cluster is in ap-southeast-1."
   └── Turn persisted with tool call details (PRD-05)

4. Operator restarts Klawmbing (kill + restart)
   ├── Session loaded from disk: main_telegram_123456789 (2 turns) (PRD-05)
   └── GBrain reconnected (PRD-03)

5. Operator sends: "What do you know about our staging cluster?"
   ├── Context includes 2 previous turns from loaded session (PRD-05)
   ├── LLM called → decides to invoke gbrain_search("staging cluster") (PRD-03)
   ├── GBrain returns the stored page
   ├── Response: "Your staging cluster is in ap-southeast-1."
   └── ✅ Hello World complete: chat → brain → persistence → recall works end-to-end.
```
