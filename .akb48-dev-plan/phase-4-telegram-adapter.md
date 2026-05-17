# Phase 4: Telegram Adapter

**Goal:** The operator can chat with AKB48 via Telegram on mobile. Responses stream token-by-token with a cursor animation. Only the operator's user ID can trigger the agent.

**Definition of Done:**
- Telegram message from allowed user → response received on phone
- Long response → split at paragraph boundaries, no truncation
- Streaming: tokens appear progressively within 1 second of first token
- Non-allowed user → silently dropped
- `Ctrl+C` in terminal → Telegram adapter shuts down cleanly

**Tickets:** KLW-018 → KLW-019

---

## KLW-018 — Telegram Adapter Core

**Type:** User Story
**Owner:** Backend
**Effort:** 5 SP
**Labels:** `phase/4`, `type/user-story`, `size/M`, `component/adapter`
**Dependencies:** KLW-007 (runtime)
**Branch:** `feat/telegram-adapter`

### User Story

> As an operator, I want to send messages to my Telegram bot and receive AI responses, so I can use AKB48 from my phone without opening a terminal.

### Implementation Plan

**Files to create:**
- `adapters/telegram/telegram.go`
- `adapters/telegram/split.go`
- `adapters/telegram/telegram_test.go`

**Add dependency:** `go get github.com/go-telegram-bot-api/telegram-bot-api/v5`

**Struct:**

```go
package telegram

import (
    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
    "github.com/dydanz/akb48/internal/config"
    "github.com/dydanz/akb48/internal/runtime"
    "github.com/dydanz/akb48/internal/types"
)

type TelegramAdapter struct {
    bot     *tgbotapi.BotAPI
    cfg     config.TelegramConfig
    handler runtime.MessageHandler
}

func NewTelegramAdapter(cfg config.TelegramConfig, handler runtime.MessageHandler) (*TelegramAdapter, error) {
    bot, err := tgbotapi.NewBotAPI(os.Getenv(cfg.TokenEnv))
    if err != nil {
        return nil, fmt.Errorf("telegram bot init: %w", err)
    }
    return &TelegramAdapter{bot: bot, cfg: cfg, handler: handler}, nil
}
```

**Start — long-polling loop:**

```go
func (a *TelegramAdapter) Start(ctx context.Context) error {
    u := tgbotapi.NewUpdate(0)
    u.Timeout = 60
    updates := a.bot.GetUpdatesChan(u)

    for {
        select {
        case <-ctx.Done():
            a.bot.StopReceivingUpdates()
            return nil
        case update, ok := <-updates:
            if !ok { return nil }
            if update.Message == nil { continue }
            go a.onMessage(ctx, update)
        }
    }
}
```

**onMessage — per-message handler:**

```go
func (a *TelegramAdapter) onMessage(ctx context.Context, update tgbotapi.Update) {
    userID := update.Message.From.ID
    chatID := update.Message.Chat.ID

    // Allowlist check
    if !a.isAllowed(userID) {
        slog.Debug("Telegram: message from non-allowed user dropped", "user_id", userID)
        return
    }

    // Typing indicator
    a.bot.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping))

    // Build normalized message
    sessionID := fmt.Sprintf("main:telegram:%d", userID)
    msg := types.Message{
        SessionID: sessionID,
        Text:      update.Message.Text,
        UserID:    fmt.Sprintf("%d", userID),
        Timestamp: time.Now(),
    }

    // Create token channel for streaming
    tokens := make(chan string, 64)
    var wg sync.WaitGroup
    wg.Add(1)
    go func() {
        defer wg.Done()
        if err := a.SendStreaming(ctx, chatID, tokens); err != nil {
            slog.Error("Telegram streaming error", "error", err)
        }
    }()

    if err := a.handler(ctx, msg, tokens); err != nil {
        slog.Error("Telegram handler error", "error", err)
        close(tokens)
        wg.Wait()
        a.Send(ctx, chatID, fmt.Sprintf("Error: %v", err))
        return
    }
    close(tokens)
    wg.Wait()
}

func (a *TelegramAdapter) isAllowed(userID int64) bool {
    if len(a.cfg.AllowedUserIDs) == 0 {
        return false // no allowlist = no one allowed
    }
    for _, id := range a.cfg.AllowedUserIDs {
        if id == userID { return true }
    }
    return false
}
```

**Send — non-streaming send with message splitting:**

```go
func (a *TelegramAdapter) Send(ctx context.Context, chatID int64, text string) error {
    parts := splitMessage(text, 4096)
    for _, part := range parts {
        msg := tgbotapi.NewMessage(chatID, part)
        if _, err := a.bot.Send(msg); err != nil {
            return fmt.Errorf("telegram send: %w", err)
        }
    }
    return nil
}
```

**splitMessage — split at paragraph boundaries:**

```go
// splitMessage splits text into chunks of at most maxLen characters.
// Prefers splitting at double-newline (paragraph) boundaries.
func splitMessage(text string, maxLen int) []string {
    if len(text) <= maxLen {
        return []string{text}
    }

    var parts []string
    for len(text) > maxLen {
        cut := maxLen
        // Try to find last paragraph break before maxLen
        if idx := strings.LastIndex(text[:maxLen], "\n\n"); idx > 0 {
            cut = idx + 2
        } else if idx := strings.LastIndex(text[:maxLen], "\n"); idx > 0 {
            cut = idx + 1
        }
        parts = append(parts, strings.TrimSpace(text[:cut]))
        text = strings.TrimSpace(text[cut:])
    }
    if text != "" {
        parts = append(parts, text)
    }
    return parts
}
```

### Acceptance Criteria

- [ ] Message from `allowed_user_ids` → handler called → response sent back to Telegram
- [ ] Message from non-allowed user ID → `handler` NOT called; message silently dropped
- [ ] Response > 4096 chars → split into multiple messages, all delivered
- [ ] Split happens at `\n\n` (paragraph boundary), not mid-sentence
- [ ] Typing indicator sent before LLM call begins
- [ ] Invalid bot token (`TELEGRAM_BOT_TOKEN` wrong) → `NewTelegramAdapter` returns error at startup
- [ ] `Ctrl+C` → long-polling stops cleanly, no goroutine leak

### Testing Plan

```go
func TestIsAllowed(t *testing.T) {
    a := &TelegramAdapter{cfg: config.TelegramConfig{AllowedUserIDs: []int64{123}}}
    assert a.isAllowed(123) == true
    assert a.isAllowed(456) == false
    assert a.isAllowed(0) == false
}

func TestSplitMessage_ShortText(t *testing.T) {
    parts := splitMessage("hello", 4096)
    assert len(parts) == 1 && parts[0] == "hello"
}

func TestSplitMessage_ParagraphSplit(t *testing.T) {
    text := strings.Repeat("a", 3000) + "\n\n" + strings.Repeat("b", 2000)
    parts := splitMessage(text, 4096)
    assert len(parts) == 2
    assert strings.HasSuffix(parts[0], strings.Repeat("a", 3000))
}

func TestSplitMessage_NoNaturalBreak(t *testing.T) {
    // 5000-char string with no newlines → splits at 4096
    text := strings.Repeat("a", 5000)
    parts := splitMessage(text, 4096)
    assert len(parts) == 2
    assert len(parts[0]) == 4096
}
```

---

## KLW-019 — Telegram Streaming (editMessageText)

**Type:** User Story
**Owner:** Backend
**Effort:** 5 SP
**Labels:** `phase/4`, `type/user-story`, `size/M`, `component/adapter`
**Dependencies:** KLW-018
**Branch:** `feat/telegram-streaming`

### User Story

> As an operator, I want Telegram responses to appear progressively as they are generated, so the UX feels responsive — not like waiting for a batch response.

### Implementation Plan

**Modify:** `adapters/telegram/telegram.go`

**SendStreaming implementation:**

```go
func (a *TelegramAdapter) SendStreaming(ctx context.Context, chatID int64, tokens <-chan string) error {
    interval := time.Duration(a.cfg.StreamingIntervalMs) * time.Millisecond

    // Send placeholder to get message ID
    placeholder, err := a.bot.Send(tgbotapi.NewMessage(chatID, "▍"))
    if err != nil {
        return fmt.Errorf("send placeholder: %w", err)
    }
    msgID := placeholder.MessageID
    currentChatID := chatID

    var buf strings.Builder
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case token, ok := <-tokens:
            if !ok {
                // Channel closed — final edit without cursor
                return a.editFinal(currentChatID, msgID, buf.String())
            }
            buf.WriteString(token)

            // If buffer approaches 4000 chars, start a new message
            if buf.Len() > 3800 {
                if err := a.editFinal(currentChatID, msgID, buf.String()); err != nil {
                    slog.Warn("Telegram: error finalizing overflowed message", "error", err)
                }
                // Start new message for overflow
                newMsg, err := a.bot.Send(tgbotapi.NewMessage(chatID, "▍"))
                if err != nil {
                    return fmt.Errorf("send overflow message: %w", err)
                }
                msgID = newMsg.MessageID
                currentChatID = chatID
                buf.Reset()
            }

        case <-ticker.C:
            if buf.Len() > 0 {
                edit := tgbotapi.NewEditMessageText(currentChatID, msgID, buf.String()+"▍")
                if _, err := a.bot.Send(edit); err != nil {
                    if isFloodControl(err) {
                        // Back off: double the interval once
                        interval = min(interval*2, 3*time.Second)
                        ticker.Reset(interval)
                        slog.Warn("Telegram flood control hit — backing off", "new_interval_ms", interval.Milliseconds())
                    } else {
                        slog.Warn("Telegram edit error", "error", err)
                    }
                }
            }

        case <-ctx.Done():
            return ctx.Err()
        }
    }
}

func (a *TelegramAdapter) editFinal(chatID int64, msgID int, text string) error {
    if text == "" { text = "(empty response)" }
    parts := splitMessage(text, 4096)
    // Edit last message with final text (no cursor)
    edit := tgbotapi.NewEditMessageText(chatID, msgID, parts[0])
    if _, err := a.bot.Send(edit); err != nil {
        slog.Warn("Telegram: final edit failed", "error", err)
    }
    // If multiple parts, send the rest as new messages
    for _, part := range parts[1:] {
        a.bot.Send(tgbotapi.NewMessage(chatID, part))
    }
    return nil
}

func isFloodControl(err error) bool {
    var tgErr *tgbotapi.Error
    return errors.As(err, &tgErr) && tgErr.Code == 429
}
```

**Wire into entry point** — update `cmd/akb48/main.go` to start Telegram adapter when configured:

```go
if cfg.Adapters.Telegram.Enabled {
    tgAdapter, err := telegram.NewTelegramAdapter(cfg.Adapters.Telegram, rt.HandleMessage)
    if err != nil {
        slog.Error("Telegram adapter init failed", "error", err)
        os.Exit(1)
    }
    go func() {
        if err := tgAdapter.Start(ctx); err != nil && err != context.Canceled {
            slog.Error("Telegram adapter error", "error", err)
        }
    }()
    slog.Info("Telegram adapter started")
}
```

### Acceptance Criteria

- [ ] First token received → placeholder "▍" replaced within 1 second
- [ ] Tokens stream progressively — multiple edits observed before full response
- [ ] Final message has NO cursor character (▍ removed)
- [ ] Response > 4000 chars → overflows into a new message seamlessly
- [ ] Telegram 429 → interval doubled (once), no crash, response eventually completes
- [ ] `SendStreaming` with empty token stream → placeholder edited to "(empty response)"
- [ ] `ctx.Done()` while streaming → streaming stops, clean exit

### Testing Plan

```go
// Mock bot API type
type mockBot struct {
    sentMessages   []tgbotapi.Chattable
    editedMessages []tgbotapi.EditMessageTextConfig
    nextErr        error
}

func TestSendStreaming_ProgressiveEdits(t *testing.T) {
    mock := &mockBot{}
    adapter := &TelegramAdapter{bot: mock, cfg: config.TelegramConfig{StreamingIntervalMs: 10}}

    tokens := make(chan string, 3)
    tokens <- "Hello"
    tokens <- " world"
    tokens <- "."
    close(tokens)

    adapter.SendStreaming(context.Background(), 123, tokens)

    // Assert: at least one editedMessages entry had "▍" suffix
    // Assert: final editedMessages entry has no "▍"
    hasCursor := false
    for _, e := range mock.editedMessages[:len(mock.editedMessages)-1] {
        if strings.HasSuffix(e.Text, "▍") { hasCursor = true }
    }
    assert hasCursor
    assert !strings.HasSuffix(mock.editedMessages[len(mock.editedMessages)-1].Text, "▍")
}

func TestSendStreaming_FloodControl(t *testing.T) {
    // Mock bot returns 429 on first edit, then succeeds
    // Assert adapter does not crash
    // Assert interval was doubled
}

func TestSendStreaming_Overflow(t *testing.T) {
    // Send 3800+ characters via tokens channel
    // Assert two messages were created (original + overflow)
}
```

**Manual test:**
```bash
# Set TELEGRAM_BOT_TOKEN and add your user ID to allowed_user_ids
TELEGRAM_BOT_TOKEN=... ./akb48
# On phone: send "Tell me about the history of the internet in 500 words"
# Observe: "▍" appears, then text streams in, cursor disappears at end
```
