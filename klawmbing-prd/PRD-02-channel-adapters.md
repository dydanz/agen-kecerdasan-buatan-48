# PRD-02: Channel Adapters (CLI + Telegram)

**Status:** Draft v1.0
**Parent:** PRD-00 (Klawmbing Master PRD)
**Author:** Dandi
**Created:** April 26, 2026
**Dependencies:** PRD-01 (Core Runtime)
**Estimated Effort:** 3-4 days

---

## 1. Problem

Klawmbing needs to receive messages from the operator and send responses back. The operator's primary interface is Telegram (mobile-first, available everywhere). For development and testing, a CLI adapter is essential — it removes the Telegram dependency during local iteration.

Both adapters must implement the same `ChannelAdapter` interface from PRD-01 so the runtime treats them identically.

---

## 2. Goals

- **G1:** A CLI adapter that reads from stdin and prints to stdout — works immediately with zero config
- **G2:** A Telegram adapter that receives messages via long-polling and responds in the same chat
- **G3:** Telegram responses stream token-by-token using `editMessageText` for a responsive UX
- **G4:** Both adapters produce normalized `Message` objects that the runtime processes identically
- **G5:** Telegram adapter enforces an allowlist — only the operator's user ID can trigger the agent

## 3. Non-Goals

- Discord adapter (Phase 2)
- Group chat handling (Phase 2)
- Voice messages
- Inline keyboards / interactive buttons (Phase 2)
- File upload handling (Phase 2)
- Webhook mode for Telegram (long-polling is sufficient for single-operator)

---

## 4. User Stories

| ID | Story | Acceptance Criteria |
|----|-------|-------------------|
| US-A01 | As a developer, I start Klawmbing with CLI adapter and type a message, and get a response printed to terminal | Response appears line-by-line as tokens stream in. No Telegram dependency needed. |
| US-A02 | As an operator, I send a Telegram message to my bot and get a response in the same chat | Response appears as a single message that progressively updates as tokens stream in. |
| US-A03 | As an operator, if someone else messages my bot, they are ignored | Non-allowlisted users receive no response. Event is logged as "unauthorized: user_id=X". |
| US-A04 | As an operator, I see a "typing..." indicator while the agent is processing | Telegram `sendChatAction(typing)` is sent before the LLM call starts. |
| US-A05 | As an operator, if the response is very long (>4096 chars), it's split into multiple messages | Telegram's message limit is 4096 chars. Split at paragraph boundaries when possible. |
| US-A06 | As a developer, I can run both CLI and Telegram adapters simultaneously | Both listen for messages; CLI for local testing, Telegram for mobile. |

---

## 5. Technical Design

### 5.1 CLI Adapter

```python
class CLIAdapter(ChannelAdapter):
    """
    Simplest possible adapter. Reads from stdin, writes to stdout.
    Used for development and testing.
    """

    async def start(self):
        """Start the input loop. Reads lines from stdin."""
        print("Klawmbing CLI — Type a message (Ctrl+C to exit)")
        print("─" * 50)
        while True:
            try:
                user_input = await asyncio.get_event_loop().run_in_executor(
                    None, input, "\n> "
                )
                if not user_input.strip():
                    continue

                message = Message(
                    id=str(uuid4()),
                    session_id="main:cli:local",
                    channel="cli",
                    sender="operator",
                    content=user_input,
                    timestamp=datetime.now(UTC),
                    attachments=[],
                    metadata={}
                )
                await self._message_handler(message)
            except (EOFError, KeyboardInterrupt):
                break

    async def send(self, session_id, content, attachments=None):
        """Print response to stdout."""
        print(f"\n{content}")

    async def send_streaming(self, session_id, token_generator):
        """Print tokens as they arrive, no newline between tokens."""
        print()  # Start on new line
        async for token in token_generator:
            print(token, end="", flush=True)
        print()  # End with newline
```

**Implementation notes:**
- Session ID is always `main:cli:local` (single user, single session)
- No authentication (local process = trusted)
- No streaming update logic needed (stdout handles it naturally)
- Total: ~50 lines

### 5.2 Telegram Adapter

```python
class TelegramAdapter(ChannelAdapter):
    """
    Telegram bot adapter using python-telegram-bot (v21+).
    Long-polling mode. Single-user allowlist.
    """

    def __init__(self, config: TelegramConfig):
        self.token = config.token
        self.allowed_user_ids: set[int] = config.allowed_user_ids
        self.streaming_interval_ms: int = config.streaming_interval_ms  # default: 1000

    async def start(self):
        """
        Initialize python-telegram-bot Application and start polling.
        Register message handler for text messages.
        """

    async def _on_message(self, update, context):
        """
        Called on every incoming message.

        1. Check allowlist → reject if unauthorized
        2. Send typing indicator
        3. Create normalized Message
        4. Call self._message_handler(message)
        """

    async def send(self, session_id, content, attachments=None):
        """
        Send a complete message to the chat.
        Split into multiple messages if > 4096 chars.
        """

    async def send_streaming(self, session_id, token_generator):
        """
        Stream tokens to Telegram using editMessageText.

        Algorithm:
        1. Send initial message: "▍" (cursor character)
        2. Buffer incoming tokens
        3. Every streaming_interval_ms, call editMessageText with accumulated text + "▍"
        4. On completion, final editMessageText without cursor
        5. On Telegram API error (flood control), increase interval temporarily
        """
```

### 5.3 Telegram Streaming Implementation Detail

Telegram doesn't support true streaming. The proven pattern is:

1. Send a placeholder message immediately: `"▍"` (block cursor)
2. As tokens arrive, buffer them
3. Every 1 second (configurable), edit the message with all accumulated text + `"▍"`
4. When streaming is complete, edit one final time without the cursor
5. If `editMessageText` fails with flood control (HTTP 429), back off and increase the edit interval

```python
async def send_streaming(self, session_id, token_generator):
    chat_id = self._session_to_chat_id(session_id)
    buffer = ""
    last_edit_time = 0

    # Send placeholder
    msg = await self.bot.send_message(chat_id=chat_id, text="▍")

    async for token in token_generator:
        buffer += token
        now = time.monotonic()

        if now - last_edit_time >= self.streaming_interval_ms / 1000:
            try:
                display = buffer + " ▍"
                # Telegram max message length is 4096
                if len(display) > 4000:
                    # Send current message, start new one
                    await self.bot.edit_message_text(
                        chat_id=chat_id, message_id=msg.message_id,
                        text=buffer[:4000]
                    )
                    buffer = buffer[4000:]
                    msg = await self.bot.send_message(chat_id=chat_id, text="▍")
                else:
                    await self.bot.edit_message_text(
                        chat_id=chat_id, message_id=msg.message_id,
                        text=display
                    )
                last_edit_time = now
            except RetryAfter as e:
                await asyncio.sleep(e.retry_after)

    # Final edit without cursor
    if buffer:
        await self.bot.edit_message_text(
            chat_id=chat_id, message_id=msg.message_id,
            text=buffer
        )
```

### 5.4 Telegram Security: Allowlist

```toml
# config.toml
[adapters.telegram]
enabled = true
token_env = "TELEGRAM_BOT_TOKEN"
allowed_user_ids = [123456789]  # Operator's Telegram user ID
```

**How to find your Telegram user ID:**
1. Message `@userinfobot` on Telegram
2. It replies with your numeric user ID
3. Add to config

**Enforcement:**
```python
async def _on_message(self, update, context):
    user_id = update.effective_user.id
    if user_id not in self.allowed_user_ids:
        logger.warning(f"Unauthorized message from user_id={user_id}")
        return  # Silent rejection — no response to unauthorized users
    # ... proceed with message handling
```

### 5.5 Session ID Convention

Each adapter generates session IDs following the pattern: `{scope}:{channel}:{identifier}`

| Adapter | Session ID | Example |
|---------|-----------|---------|
| CLI | `main:cli:local` | Always the same |
| Telegram (DM from operator) | `main:telegram:{user_id}` | `main:telegram:123456789` |
| Telegram (group, future) | `group:telegram:{chat_id}` | `group:telegram:-100123456` |
| Discord (DM, future) | `main:discord:{user_id}` | `main:discord:456789` |
| Discord (channel, future) | `group:discord:{channel_id}` | `group:discord:789012` |

The `main:` prefix grants full permissions. `group:` prefix triggers sandboxing (Phase 2).

### 5.6 Message Length Handling

Telegram limits: 4096 characters per message.

**Splitting strategy:**
1. If response < 4096 chars: send as single message
2. If response > 4096 chars: split at the nearest paragraph boundary (`\n\n`) before 4000 chars
3. If no paragraph boundary exists: split at the nearest newline (`\n`) before 4000 chars
4. If no newline exists: hard split at 4000 chars
5. Send each chunk as a separate message with 200ms delay between sends

---

## 6. Configuration

### New config.toml sections for this PRD:

```toml
[adapters]
cli_enabled = true                    # Always true for dev

[adapters.telegram]
enabled = false                       # Set true when ready
token_env = "TELEGRAM_BOT_TOKEN"
allowed_user_ids = []                 # REQUIRED when enabled
streaming_interval_ms = 1000          # How often to update the streaming message
parse_mode = "Markdown"               # "Markdown" | "HTML" | "None"

# Future
[adapters.discord]
enabled = false
token_env = "DISCORD_BOT_TOKEN"
```

---

## 7. Error Handling

| Error | Handling | User-facing |
|-------|----------|-------------|
| Telegram token invalid | Fail fast on startup: "Invalid TELEGRAM_BOT_TOKEN" | Process doesn't start |
| Telegram API unreachable | Retry with backoff, log warning | Bot appears offline |
| editMessageText flood (429) | Respect `retry_after`, increase edit interval | Brief pause in streaming, then resumes |
| Message from unauthorized user | Silent drop, log warning | No response |
| Response exceeds 4096 chars | Split into multiple messages | Multiple messages arrive in sequence |
| Telegram network timeout during streaming | Send whatever has been accumulated, log error | Partial response visible, operator can ask to continue |

---

## 8. Testing Strategy

| Test | Type | How |
|------|------|-----|
| CLI adapter receives input and returns output | Unit | Mock LLM caller, verify Message creation and send() |
| Telegram allowlist blocks unauthorized users | Unit | Mock update with wrong user_id, verify no handler call |
| Telegram streaming edits message at correct intervals | Integration | Mock bot API, verify editMessageText called every ~1s |
| Long response splits correctly | Unit | Generate 10,000 char response, verify splits at paragraph boundaries |
| Both adapters can run simultaneously | Integration | Start both, send messages via each, verify independent handling |
| Session IDs follow convention | Unit | Verify format for each adapter type |

---

## 9. Acceptance Criteria

- [ ] CLI adapter: type message → receive streamed response in terminal
- [ ] Telegram adapter: send message → receive streaming response with cursor animation
- [ ] Unauthorized Telegram users are silently rejected and logged
- [ ] Response > 4096 chars splits into multiple Telegram messages
- [ ] Both adapters produce identical `Message` objects for the same input text
- [ ] Typing indicator (`sendChatAction`) fires before LLM call
- [ ] Telegram edit interval is configurable and defaults to 1000ms
- [ ] Process survives Telegram API errors without crashing

---

## 10. Open Questions

| ID | Question | Default |
|----|----------|---------|
| TODO-A01 | Should Telegram responses use Markdown formatting? Claude often returns markdown. Telegram supports MarkdownV2 but it's strict and breaks on unescaped characters. | Default: plain text (no parse_mode). Add Markdown support in Phase 2 after building an escaping layer. Prevents broken messages from unescaped `_*[]()~` characters. |
| TODO-A02 | Should we support Telegram inline keyboards for approval gates (e.g., "Deploy to staging? [Yes] [No]")? | Phase 2. Not needed for hello world. |
| TODO-A03 | Should `/commands` be supported (e.g., `/status`, `/skills`, `/brain`)? Or is natural language sufficient? | Phase 2. Start with natural language only. Add slash commands when specific shortcuts prove needed. |
| TODO-A04 | Photo/document attachments: should the adapter accept them and pass as attachments in Message? | Phase 2. For hello world, text-only is sufficient. |
