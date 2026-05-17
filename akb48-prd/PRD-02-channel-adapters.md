# PRD-02: Channel Adapters (CLI + Telegram)

**Status:** Draft v2.0
**Parent:** PRD-00 (AKB48 Master PRD)
**Author:** Dandi
**Created:** April 26, 2026
**Dependencies:** PRD-01 (Core Runtime)
**Estimated Effort:** 3-4 days

---

## 1. Problem

AKB48 needs to receive messages from the operator and send responses back. The operator's primary interface is Telegram (mobile-first, available everywhere). For development and testing, a CLI adapter is essential — it removes the Telegram dependency during local iteration.

Both adapters must implement the same `ChannelAdapter` interface so the runtime treats them identically.

---

## 2. Goals

- **G1:** A CLI adapter that reads from stdin and writes to stdout — works immediately with zero config
- **G2:** A Telegram adapter that receives messages via long-polling and responds in the same chat
- **G3:** Telegram responses stream token-by-token using `EditMessageText` for a responsive UX
- **G4:** Both adapters produce normalized `Message` values that the runtime processes identically
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
| US-A01 | As a developer, I start AKB48 with CLI adapter and type a message, and get a response printed to terminal | Response appears character-by-character as tokens stream in. No Telegram dependency needed. |
| US-A02 | As an operator, I send a Telegram message to my bot and get a response in the same chat | Response appears as a single message that progressively updates as tokens stream in. |
| US-A03 | As an operator, if someone else messages my bot, they are ignored | Non-allowlisted users receive no response. Event is logged as `unauthorized: user_id=X`. |
| US-A04 | As an operator, I see a "typing..." indicator while the agent is processing | Telegram `sendChatAction(typing)` is sent before the LLM call starts. |
| US-A05 | As an operator, if the response is very long (>4096 chars), it's split into multiple messages | Telegram's message limit is 4096 chars. Split at paragraph boundaries when possible. |
| US-A06 | As a developer, I can run both CLI and Telegram adapters simultaneously | Both listen for messages; CLI for local testing, Telegram for mobile. |

---

## 5. Technical Design

### 5.1 Interface Definition

All adapters implement this interface, defined in `adapters/adapter.go`:

```go
package adapters

import "context"

// ChannelAdapter is the normalized interface for all message channels.
// Start blocks until ctx is cancelled. Stop performs cleanup.
// Send delivers a complete response. SendStreaming consumes a token channel,
// updating the chat in place until the channel is closed.
type ChannelAdapter interface {
    Start(ctx context.Context) error
    Stop() error
    Send(ctx context.Context, sessionID, text string) error
    SendStreaming(ctx context.Context, sessionID string, tokens <-chan string) error
}
```

The runtime receives a `HandleMessage` function from `core/runtime.go`. Both adapters call it when a user message arrives:

```go
// HandleMessage is injected into each adapter at construction time.
// It is the entry point into the core runtime for every inbound message.
type HandleMessage func(ctx context.Context, msg Message) error
```

`Message` is defined in `core/types.go` (PRD-01):

```go
type Message struct {
    ID        string
    SessionID string
    Channel   string
    Sender    string
    Content   string
    Timestamp time.Time
    Metadata  map[string]string
}
```

---

### 5.2 CLI Adapter

**File:** `adapters/cli/cli.go`

The CLI adapter is the simplest possible implementation: a `bufio.Scanner` over `os.Stdin` and writes to `io.Writer` (default `os.Stdout`). Total: ~50 lines.

```go
package cli

import (
    "bufio"
    "context"
    "fmt"
    "io"
    "time"

    "github.com/google/uuid"

    "github.com/dydanz/akb48/adapters"
    "github.com/dydanz/akb48/internal/types"
)

// CLIAdapter reads from stdin and writes to stdout.
// Used for development and testing. No authentication required
// (local process = trusted operator).
type CLIAdapter struct {
    scanner       *bufio.Scanner
    out           io.Writer
    handleMessage adapters.HandleMessage
}

func New(in io.Reader, out io.Writer, handle adapters.HandleMessage) *CLIAdapter {
    return &CLIAdapter{
        scanner:       bufio.NewScanner(in),
        out:           out,
        handleMessage: handle,
    }
}

// Start blocks, reading lines from stdin until ctx is cancelled or EOF.
func (a *CLIAdapter) Start(ctx context.Context) error {
    fmt.Fprintln(a.out, "AKB48 CLI — Type a message (Ctrl+C or Ctrl+D to exit)")
    fmt.Fprintln(a.out, "──────────────────────────────────────────────────")
    for {
        fmt.Fprint(a.out, "\n> ")
        if !a.scanner.Scan() {
            return a.scanner.Err() // nil on EOF
        }
        line := a.scanner.Text()
        if line == "" {
            continue
        }
        msg := core.Message{
            ID:        uuid.NewString(),
            SessionID: "main:cli:local",
            Channel:   "cli",
            Sender:    "operator",
            Content:   line,
            Timestamp: time.Now().UTC(),
            Metadata:  map[string]string{},
        }
        if err := a.handleMessage(ctx, msg); err != nil {
            fmt.Fprintf(a.out, "\n[error] %v\n", err)
        }
    }
}

func (a *CLIAdapter) Stop() error { return nil }

// Send prints a complete response to stdout.
func (a *CLIAdapter) Send(_ context.Context, _, text string) error {
    fmt.Fprintf(a.out, "\n%s\n", text)
    return nil
}

// SendStreaming prints tokens to stdout as they arrive.
// No buffering or edit logic needed — the terminal handles it naturally.
func (a *CLIAdapter) SendStreaming(_ context.Context, _ string, tokens <-chan string) error {
    fmt.Fprintln(a.out)
    for tok := range tokens {
        fmt.Fprint(a.out, tok)
    }
    fmt.Fprintln(a.out)
    return nil
}
```

**Implementation notes:**
- Session ID is always `main:cli:local` (single user, single session)
- No authentication (local process = trusted)
- No streaming update logic needed — stdout handles it naturally
- `io.Reader`/`io.Writer` injection makes it trivially testable

---

### 5.3 Telegram Adapter

**File:** `adapters/telegram/telegram.go`

**Library:** `github.com/go-telegram-bot-api/telegram-bot-api/v5`

```go
package telegram

import (
    "context"
    "fmt"
    "log/slog"
    "strings"
    "sync"
    "time"

    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
    "github.com/google/uuid"

    "github.com/dydanz/akb48/adapters"
    "github.com/dydanz/akb48/internal/types"
)

// Config holds Telegram-specific settings loaded from config.toml.
type Config struct {
    Token               string        // read from env at startup
    AllowedUserIDs      []int64       // allowlist; silently reject all others
    StreamingInterval   time.Duration // cadence for EditMessageText (default: 1s)
}

// TelegramAdapter implements ChannelAdapter for Telegram long-polling.
type TelegramAdapter struct {
    bot           *tgbotapi.BotAPI
    cfg           Config
    handleMessage adapters.HandleMessage
    // chatSessions maps sessionID → chatID for reverse-lookup in Send/SendStreaming
    chatSessions  sync.Map
}

func New(cfg Config, handle adapters.HandleMessage) (*TelegramAdapter, error) {
    bot, err := tgbotapi.NewBotAPI(cfg.Token)
    if err != nil {
        return nil, fmt.Errorf("telegram: invalid token or unreachable API: %w", err)
    }
    if cfg.StreamingInterval == 0 {
        cfg.StreamingInterval = time.Second
    }
    return &TelegramAdapter{bot: bot, cfg: cfg, handleMessage: handle}, nil
}
```

#### 5.3.1 Start — Long-Polling Loop

```go
// Start begins long-polling and blocks until ctx is cancelled.
func (a *TelegramAdapter) Start(ctx context.Context) error {
    u := tgbotapi.NewUpdate(0)
    u.Timeout = 60
    updates := a.bot.GetUpdatesChan(u)

    for {
        select {
        case <-ctx.Done():
            return nil
        case update, ok := <-updates:
            if !ok {
                return nil
            }
            if update.Message == nil || !update.Message.IsCommand() && update.Message.Text == "" {
                continue
            }
            go a.onMessage(ctx, update)
        }
    }
}

func (a *TelegramAdapter) Stop() error {
    a.bot.StopReceivingUpdates()
    return nil
}
```

#### 5.3.2 Message Handler — Allowlist + Dispatch

```go
func (a *TelegramAdapter) onMessage(ctx context.Context, update tgbotapi.Update) {
    userID := update.Message.From.ID
    chatID := update.Message.Chat.ID

    // Allowlist check — silent rejection
    if !a.isAllowed(userID) {
        slog.Warn("unauthorized telegram message", "user_id", userID)
        return
    }

    sessionID := fmt.Sprintf("main:telegram:%d", userID)
    a.chatSessions.Store(sessionID, chatID)

    // Typing indicator before LLM call
    typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
    if _, err := a.bot.Send(typing); err != nil {
        slog.Warn("telegram: failed to send typing action", "err", err)
    }

    msg := core.Message{
        ID:        uuid.NewString(),
        SessionID: sessionID,
        Channel:   "telegram",
        Sender:    "operator",
        Content:   update.Message.Text,
        Timestamp: time.Now().UTC(),
        Metadata:  map[string]string{"chat_id": fmt.Sprintf("%d", chatID)},
    }

    if err := a.handleMessage(ctx, msg); err != nil {
        slog.Error("telegram: handleMessage error", "err", err)
    }
}

func (a *TelegramAdapter) isAllowed(userID int64) bool {
    for _, id := range a.cfg.AllowedUserIDs {
        if id == userID {
            return true
        }
    }
    return false
}
```

#### 5.3.3 Send — Complete Response

```go
// Send delivers a complete (non-streaming) response.
// Splits into multiple messages if text exceeds 4096 chars.
func (a *TelegramAdapter) Send(ctx context.Context, sessionID, text string) error {
    chatID, err := a.resolveChatID(sessionID)
    if err != nil {
        return err
    }
    chunks := splitMessage(text, 4000)
    for i, chunk := range chunks {
        m := tgbotapi.NewMessage(chatID, chunk)
        if _, err := a.sendWithRetry(m); err != nil {
            return fmt.Errorf("telegram: send chunk %d: %w", i, err)
        }
        if i < len(chunks)-1 {
            time.Sleep(200 * time.Millisecond)
        }
    }
    return nil
}
```

#### 5.3.4 SendStreaming — Token-by-Token Editing

The streaming pattern: send a placeholder immediately, accumulate tokens into a buffer, fire a `time.Ticker` every `StreamingInterval` to call `EditMessageText`, and on channel close perform one final edit without the cursor.

```go
// SendStreaming consumes tokens from the channel, updating a single Telegram
// message in place until the channel closes.
func (a *TelegramAdapter) SendStreaming(ctx context.Context, sessionID string, tokens <-chan string) error {
    chatID, err := a.resolveChatID(sessionID)
    if err != nil {
        return err
    }

    // Send placeholder with block cursor
    placeholder := tgbotapi.NewMessage(chatID, "▍")
    sent, err := a.sendWithRetry(placeholder)
    if err != nil {
        return fmt.Errorf("telegram: send placeholder: %w", err)
    }

    var (
        mu      sync.Mutex
        buf     strings.Builder
        ticker  = time.NewTicker(a.cfg.StreamingInterval)
        msgID   = sent.MessageID
        done    = make(chan struct{})
    )
    defer ticker.Stop()

    // Goroutine: drain tokens into buffer; close done when channel closes
    go func() {
        for tok := range tokens {
            mu.Lock()
            buf.WriteString(tok)
            mu.Unlock()
        }
        close(done)
    }()

    edit := func(text string, final bool) {
        if text == "" {
            return
        }
        // Roll over to a new message when approaching the 4096-char limit
        if len(text) > 4000 {
            // Emit what we have, start fresh
            a.editWithRetry(chatID, msgID, text[:4000])
            remainder := text[4000:]
            m := tgbotapi.NewMessage(chatID, "▍")
            if newMsg, err := a.sendWithRetry(m); err == nil {
                msgID = newMsg.MessageID
            }
            mu.Lock()
            existing := buf.String()
            buf.Reset()
            buf.WriteString(existing[len(text[:4000]):])
            _ = existing
            mu.Unlock()
            if !final {
                return
            }
            text = remainder
        }
        display := text
        if !final {
            display = text + " ▍"
        }
        a.editWithRetry(chatID, msgID, display)
    }

    for {
        select {
        case <-ticker.C:
            mu.Lock()
            current := buf.String()
            mu.Unlock()
            edit(current, false)
        case <-done:
            mu.Lock()
            final := buf.String()
            mu.Unlock()
            edit(final, true)
            return nil
        case <-ctx.Done():
            return ctx.Err()
        }
    }
}
```

#### 5.3.5 Helpers — Retry and Flood Control

```go
// sendWithRetry sends a Chattable, respecting Telegram flood-control (429).
func (a *TelegramAdapter) sendWithRetry(c tgbotapi.Chattable) (tgbotapi.Message, error) {
    for {
        msg, err := a.bot.Send(c)
        if err == nil {
            return msg, nil
        }
        var tgErr *tgbotapi.Error
        if errors.As(err, &tgErr) && tgErr.Code == 429 {
            wait := time.Duration(tgErr.Parameters.RetryAfter) * time.Second
            slog.Warn("telegram: flood control", "retry_after", wait)
            time.Sleep(wait)
            continue
        }
        return tgbotapi.Message{}, err
    }
}

// editWithRetry calls EditMessageText, respecting flood control.
func (a *TelegramAdapter) editWithRetry(chatID int64, msgID int, text string) {
    edit := tgbotapi.NewEditMessageText(chatID, msgID, text)
    for {
        if _, err := a.bot.Send(edit); err == nil {
            return
        } else {
            var tgErr *tgbotapi.Error
            if errors.As(err, &tgErr) && tgErr.Code == 429 {
                wait := time.Duration(tgErr.Parameters.RetryAfter) * time.Second
                slog.Warn("telegram: flood control on edit", "retry_after", wait)
                time.Sleep(wait)
                continue
            }
            slog.Warn("telegram: editMessageText failed", "err", err)
            return
        }
    }
}

// resolveChatID looks up the Telegram chat ID for a given session ID.
func (a *TelegramAdapter) resolveChatID(sessionID string) (int64, error) {
    v, ok := a.chatSessions.Load(sessionID)
    if !ok {
        return 0, fmt.Errorf("telegram: no chat mapped for session %q", sessionID)
    }
    return v.(int64), nil
}
```

---

### 5.4 Message Splitting

`splitMessage` is a pure function with no external dependencies. It lives in `adapters/telegram/split.go`.

```go
// splitMessage splits text into chunks no longer than limit bytes,
// preferring paragraph boundaries (\n\n), then line boundaries (\n),
// and falling back to a hard split.
func splitMessage(text string, limit int) []string {
    if len(text) <= limit {
        return []string{text}
    }
    var chunks []string
    for len(text) > limit {
        cut := strings.LastIndex(text[:limit], "\n\n")
        if cut <= 0 {
            cut = strings.LastIndex(text[:limit], "\n")
        }
        if cut <= 0 {
            cut = limit
        }
        chunks = append(chunks, strings.TrimSpace(text[:cut]))
        text = strings.TrimSpace(text[cut:])
    }
    if text != "" {
        chunks = append(chunks, text)
    }
    return chunks
}
```

---

### 5.5 Telegram Security: Allowlist

```toml
# config.toml
[adapters.telegram]
enabled     = true
token_env   = "TELEGRAM_BOT_TOKEN"
allowed_user_ids   = [123456789]  # Operator's Telegram user ID
streaming_interval_ms = 1000
```

**How to find your Telegram user ID:**
1. Message `@userinfobot` on Telegram
2. It replies with your numeric user ID
3. Add to config

Enforcement happens in `onMessage` before any processing. Non-allowlisted users receive no response; the event is logged via `slog.Warn`.

---

### 5.6 Session ID Convention

Each adapter generates session IDs following `{scope}:{channel}:{identifier}`:

| Adapter | Session ID | Example |
|---------|-----------|---------|
| CLI | `main:cli:local` | Always the same |
| Telegram DM (operator) | `main:telegram:{user_id}` | `main:telegram:123456789` |
| Telegram group (future) | `group:telegram:{chat_id}` | `group:telegram:-100123456` |
| Discord DM (future) | `main:discord:{user_id}` | `main:discord:456789` |
| Discord channel (future) | `group:discord:{channel_id}` | `group:discord:789012` |

The `main:` prefix grants full tool permissions. `group:` prefix triggers sandboxing (Phase 2).

---

## 6. Configuration

```toml
[adapters]
cli_enabled = true                    # Always true for dev

[adapters.telegram]
enabled               = false         # Set true when ready
token_env             = "TELEGRAM_BOT_TOKEN"
allowed_user_ids      = []            # REQUIRED when enabled
streaming_interval_ms = 1000          # How often to call EditMessageText

# Future
[adapters.discord]
enabled   = false
token_env = "DISCORD_BOT_TOKEN"
```

Config is validated via `config.Validate() error` in PRD-01. Startup fails immediately if `telegram.enabled = true` and `allowed_user_ids` is empty, or if the token env var is unset.

---

## 7. Error Handling

| Error | Handling | User-facing |
|-------|----------|-------------|
| Telegram token invalid | Fail fast on startup: log fatal + exit(1) | Process does not start |
| Telegram API unreachable | `sendWithRetry` with exponential backoff; log warning | Bot appears offline |
| `EditMessageText` flood (429) | Respect `Parameters.RetryAfter`; sleep then retry | Brief pause in streaming, then resumes |
| Message from unauthorized user | Silent drop; `slog.Warn` with user ID | No response |
| Response exceeds 4096 chars | `splitMessage` → multiple messages, 200ms apart | Multiple messages arrive in sequence |
| Network timeout during streaming | Deliver accumulated buffer; log error | Partial response visible; operator can ask to continue |
| `tokens` channel closed before first token | Final edit removes placeholder (empty string skipped) | Nothing visible; no crash |

---

## 8. Project Layout

```
adapters/
├── adapter.go              # ChannelAdapter interface + HandleMessage type
├── cli/
│   └── cli.go              # ~50 lines
└── telegram/
    ├── telegram.go         # TelegramAdapter struct, Start, Stop, Send, SendStreaming
    └── split.go            # splitMessage pure function
```

---

## 9. Testing Strategy

| Test | Type | How |
|------|------|-----|
| CLI adapter receives input and calls handler | Unit | Inject `strings.NewReader`, verify `HandleMessage` called with correct `Message` |
| CLI `SendStreaming` writes tokens in order | Unit | Pass token channel, verify `strings.Builder` output |
| Telegram allowlist blocks unauthorized users | Unit | Call `onMessage` with non-allowlisted user ID, verify handler never called |
| Telegram streaming edits at correct interval | Unit | Mock `BotAPI` (interface), advance ticker, verify `EditMessageText` call count |
| `splitMessage` at paragraph boundary | Unit | 5000-char input with `\n\n` at 3800; verify two chunks, split at paragraph |
| `splitMessage` falls back to newline | Unit | 5000-char input, `\n` at 3900, no `\n\n`; verify split at newline |
| `splitMessage` hard-splits if no boundary | Unit | 5000-char input, no whitespace; verify chunk at exactly 4000 chars |
| Both adapters run concurrently | Integration | Start both in goroutines, send to each, verify independent `HandleMessage` calls |
| Session IDs follow convention | Unit | Verify format string for each adapter and input |

---

## 10. Acceptance Criteria

- [ ] CLI adapter: type message → receive streamed response in terminal
- [ ] Telegram adapter: send message → receive streaming response with cursor animation (`▍`)
- [ ] Unauthorized Telegram users are silently rejected and logged with their user ID
- [ ] Response > 4096 chars splits into multiple Telegram messages at paragraph boundaries
- [ ] Both adapters produce `Message` values with identical field structure for the same input text
- [ ] Typing indicator (`ChatTyping`) fires before `HandleMessage` is called
- [ ] Telegram edit interval is configurable via `streaming_interval_ms` and defaults to 1000ms
- [ ] Process survives Telegram API errors (429, network timeout) without crashing
- [ ] `Stop()` on Telegram adapter calls `StopReceivingUpdates()` cleanly

---

## 11. Open Questions

| ID | Question | Default |
|----|----------|---------|
| TODO-A01 | Should Telegram responses use Markdown formatting? Claude often returns markdown. Telegram supports MarkdownV2 but it's strict and breaks on unescaped characters. | Default: plain text (no `ParseMode`). Add Markdown support in Phase 2 after building an escaping layer. Prevents broken messages from unescaped `_*[]()~` characters. |
| TODO-A02 | Should we support Telegram inline keyboards for approval gates (e.g., "Deploy to staging? [Yes] [No]")? | Phase 2. Not needed for hello world. |
| TODO-A03 | Should `/commands` be supported (e.g., `/status`, `/skills`, `/brain`)? Or is natural language sufficient? | Phase 2. Start with natural language only. Add slash commands when specific shortcuts prove needed. |
| TODO-A04 | Photo/document attachments: should the adapter accept them and pass as attachments in `Message`? | Phase 2. Text-only for hello world. |
| TODO-A05 | Should `BotAPI` be wrapped in an interface for unit-testing without a live Telegram connection? | Yes. Define a minimal `TelegramClient` interface in `telegram.go` covering `Send` and `GetUpdatesChan`. Inject in `New()`. |
