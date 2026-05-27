# Phase 7: Discord @Mention Response in Server Channels

**PRD:** `akb48-prd/PRD-07-discord-mention.md`  
**Goal:** Allow allowed users to prompt AKB48 by mentioning it (`@Kabayan`) in any server text channel. Bot replies in a thread on the original message. DM and slash-command paths unchanged.

**Definition of Done:**
- `mention_response = false` (default): bot ignores all guild-channel messages, no change to existing behaviour
- `mention_response = true`: `@Kabayan <text>` in any guild text or news channel from an allowed user → threaded streaming reply
- Non-allowed user mentioning bot → silent ignore (no message sent)
- Bare `@Kabayan` with no other text → silent ignore
- Mid-sentence `hey @Kabayan what time is it?` → LLM receives `hey what time is it?`
- Both `<@BOT_ID>` and `<@!BOT_ID>` (nickname form) stripped correctly
- Overflow messages thread back to same original mention
- DM path unchanged — existing tests still pass
- Slash command path unchanged
- `go test ./adapters/discord/...` passes without a real Discord connection
- `go build ./...` clean

**Tickets:** KLW-028 → KLW-029 → KLW-030

---

## KLW-028 — Config: `mention_response` Field

**Type:** Configuration  
**Effort:** 1 SP  
**Labels:** `phase/7`, `type/config`, `size/XS`, `component/config`  
**Dependencies:** none  
**Branch:** `feature/KLW-028-discord-mention`

### User Story

> As an operator, I want to opt in to @mention response with a single config flag, so it's off by default and I can enable it without restarting in a way that breaks existing users.

### Implementation Plan

**Modify:** `internal/config/config.go`

Add `MentionResponse` to `DiscordConfig`. Zero value `false` is the correct default — no entry in `applyDefaults()` needed.

```go
type DiscordConfig struct {
    Enabled             bool     `toml:"enabled"`
    TokenEnv            string   `toml:"token_env"`
    AllowedUserIDs      []string `toml:"allowed_user_ids"`
    StreamingIntervalMs int      `toml:"streaming_interval_ms"`
    SlashCommands       bool     `toml:"slash_commands"`
    SlashCommandGuildID string   `toml:"slash_command_guild_id"`
    MentionResponse     bool     `toml:"mention_response"` // NEW
}
```

**Modify:** `config.toml`

Under `[adapters.discord]`, add:
```toml
mention_response = false    # set true to respond to @mentions in server channels
```

**Modify:** `deploy/config.docker.toml`

Same addition under `[adapters.discord]`:
```toml
mention_response = false
```

### Acceptance Criteria

- [ ] `DiscordConfig.MentionResponse` field present with `toml:"mention_response"` tag
- [ ] `config.toml` has `mention_response = false` under `[adapters.discord]`
- [ ] `deploy/config.docker.toml` has `mention_response = false` under `[adapters.discord]`
- [ ] `go build ./...` clean

---

## KLW-029 — Discord Adapter: Mention Detection, Routing, Threaded Reply

**Type:** User Story  
**Effort:** 5 SP  
**Labels:** `phase/7`, `type/user-story`, `size/M`, `component/adapter`  
**Dependencies:** KLW-028  
**Branch:** `feature/KLW-028-discord-mention`

### User Story

> As an operator in a shared Discord server, I want to mention `@Kabayan` in any channel and get a streaming reply threaded to my message, with the same session history as my DMs.

### Implementation Plan

**Modify:** `adapters/discord/discord.go`

#### 1. New helpers — `isMentioned` and `stripMention`

Add after `isAllowed`:

```go
// isMentioned reports whether the bot's own user ID appears in m.Mentions.
func (a *Adapter) isMentioned(m *discordgo.MessageCreate) bool {
    botID := a.session.State.User.ID
    for _, u := range m.Mentions {
        if u.ID == botID {
            return true
        }
    }
    return false
}

// stripMention removes <@BOT_ID> and <@!BOT_ID> from text and trims whitespace.
func (a *Adapter) stripMention(text string) string {
    botID := a.session.State.User.ID
    text = strings.ReplaceAll(text, "<@"+botID+">", "")
    text = strings.ReplaceAll(text, "<@!"+botID+">", "")
    return strings.TrimSpace(text)
}
```

#### 2. Refactor `onMessage` — channel-type switch

Current `onMessage` fetches the channel via API every time and only handles DMs. Replace with a state-cache-first lookup and a switch on channel type:

```go
// onMessage routes DMs to the existing path and guild-channel mentions (opt-in) to
// the threaded reply path.
func (a *Adapter) onMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
    if m.Author == nil || m.Author.Bot {
        return
    }

    ch, err := s.State.Channel(m.ChannelID)
    if err != nil {
        ch, err = s.Channel(m.ChannelID)
        if err != nil {
            slog.Warn("Discord: could not fetch channel", "channel_id", m.ChannelID, "error", err)
            return
        }
    }

    switch ch.Type {
    case discordgo.ChannelTypeDM:
        if !a.isAllowed(m.Author.ID) {
            slog.Debug("Discord: DM from non-allowed user dropped", "user_id", m.Author.ID)
            return
        }
        go a.process(m.ChannelID, m.Author.ID, m.Content, nil)

    case discordgo.ChannelTypeGuildText, discordgo.ChannelTypeGuildNews:
        if !a.cfg.MentionResponse {
            return
        }
        if !a.isMentioned(m) {
            return
        }
        if !a.isAllowed(m.Author.ID) {
            return // silent ignore — no auth error in server channels
        }
        text := a.stripMention(m.Content)
        if text == "" {
            return // bare @mention with no prompt
        }
        ref := m.Reference()
        go a.process(m.ChannelID, m.Author.ID, text, ref)
    }
}
```

**Why state-cache-first:** `s.State.Channel` reads from the in-memory cache populated by the Gateway — zero API calls. Falls back to `s.Channel` (REST) only on cache miss (e.g., cold start or DM channels not pre-cached). Avoids rate-limit pressure on high-traffic servers.

#### 3. Update `process()` — add `ref` parameter

Current signature: `func (a *Adapter) process(channelID, userID, text string)`  
New signature: `func (a *Adapter) process(channelID, userID, text string, ref *discordgo.MessageReference)`

`ref` is `nil` for DM path; non-nil for guild-mention path. Pass through to `sendStreaming`:

```go
func (a *Adapter) process(channelID, userID, text string, ref *discordgo.MessageReference) {
    a.session.ChannelTyping(channelID)

    msg := types.Message{
        SessionID: fmt.Sprintf("main:discord:%s", userID),
        Text:      text,
        UserID:    userID,
        Timestamp: time.Now(),
    }

    tokens := make(chan string, 64)
    var wg sync.WaitGroup
    wg.Add(1)
    go func() {
        defer wg.Done()
        if err := a.sendStreaming(channelID, tokens, ref); err != nil {
            slog.Error("Discord streaming error", "error", err)
        }
    }()

    ctx := context.Background()
    if err := a.handler(ctx, msg, tokens); err != nil {
        slog.Error("Discord handler error", "error", err)
        close(tokens)
        wg.Wait()
        a.session.ChannelMessageSend(channelID, fmt.Sprintf("Error: %v", err))
        return
    }
    close(tokens)
    wg.Wait()
}
```

#### 4. Update `sendStreaming()` — add `ref` parameter, use `ChannelMessageSendReply` when set

Current signature: `func (a *Adapter) sendStreaming(channelID string, tokens <-chan string) error`  
New signature: `func (a *Adapter) sendStreaming(channelID string, tokens <-chan string, ref *discordgo.MessageReference) error`

Use a local helper to send the placeholder (and overflow placeholders):

```go
func (a *Adapter) sendStreaming(channelID string, tokens <-chan string, ref *discordgo.MessageReference) error {
    interval := time.Duration(a.cfg.StreamingIntervalMs) * time.Millisecond

    sendPlaceholder := func() (*discordgo.Message, error) {
        if ref != nil {
            return a.session.ChannelMessageSendReply(channelID, cursor, ref)
        }
        return a.session.ChannelMessageSend(channelID, cursor)
    }

    placeholder, err := sendPlaceholder()
    if err != nil {
        return fmt.Errorf("send placeholder: %w", err)
    }
    msgID := placeholder.ID

    var buf strings.Builder
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case token, ok := <-tokens:
            if !ok {
                return a.editFinal(channelID, msgID, buf.String(), ref)
            }
            buf.WriteString(token)

            if buf.Len() > overflowAt {
                if err := a.editFinal(channelID, msgID, buf.String(), ref); err != nil {
                    slog.Warn("Discord: overflow finalize error", "error", err)
                }
                newMsg, err := sendPlaceholder()
                if err != nil {
                    return fmt.Errorf("send overflow placeholder: %w", err)
                }
                msgID = newMsg.ID
                buf.Reset()
            }

        case <-ticker.C:
            if buf.Len() > 0 {
                if _, err := a.session.ChannelMessageEdit(channelID, msgID, buf.String()+cursor); err != nil {
                    if isRateLimited(err) {
                        interval = min(interval*2, maxBackoff)
                        ticker.Reset(interval)
                        slog.Warn("Discord rate limited — backing off", "new_interval_ms", interval.Milliseconds())
                    } else {
                        slog.Warn("Discord edit error", "error", err)
                    }
                }
            }
        }
    }
}
```

#### 5. Update `editFinal()` — add `ref` parameter for overflow parts

Current signature: `func (a *Adapter) editFinal(channelID, msgID, text string) error`  
New signature: `func (a *Adapter) editFinal(channelID, msgID, text string, ref *discordgo.MessageReference) error`

Overflow parts (parts[1:]) use `ChannelMessageSendReply` when `ref` is non-nil:

```go
func (a *Adapter) editFinal(channelID, msgID, text string, ref *discordgo.MessageReference) error {
    if text == "" {
        text = "(empty response)"
    }
    parts := shared.SplitMessage(text, maxMsgLen)
    if _, err := a.session.ChannelMessageEdit(channelID, msgID, parts[0]); err != nil {
        slog.Warn("Discord: final edit failed", "error", err)
    }
    for _, part := range parts[1:] {
        if ref != nil {
            if _, err := a.session.ChannelMessageSendReply(channelID, part, ref); err != nil {
                slog.Warn("Discord: overflow reply send failed", "error", err)
            }
        } else {
            if _, err := a.session.ChannelMessageSend(channelID, part); err != nil {
                slog.Warn("Discord: overflow part send failed", "error", err)
            }
        }
    }
    return nil
}
```

#### No intent changes required

`IntentsGuildMessages` and `IntentsMessageContent` are already set in `New()`. `m.Mentions` is populated from parsed message content — no additional intent needed.

#### No session ID changes

Session stays `main:discord:{user_id}`. DM history and guild-mention history share the same JSONL session file per user. Intentional: the operator's context is portable across channels.

### Acceptance Criteria

- [ ] `isMentioned()` returns true iff bot's own ID appears in `m.Mentions`
- [ ] `stripMention()` removes both `<@BOT_ID>` and `<@!BOT_ID>`, trims whitespace
- [ ] `onMessage()` uses state-cache-first channel lookup
- [ ] DM path unchanged: no ref passed, `ChannelMessageSend` used for placeholder
- [ ] Guild path: `mention_response = false` → no response even if mentioned
- [ ] Guild path: `mention_response = true`, mentioned, allowed, non-empty text → threaded reply
- [ ] Guild path: non-allowed user → silent ignore (no message sent to channel)
- [ ] Guild path: bare `@Kabayan` → silent ignore
- [ ] Overflow messages in guild path use `ChannelMessageSendReply` with same `ref`
- [ ] `go build ./...` clean

---

## KLW-030 — Tests

**Type:** Testing  
**Effort:** 2 SP  
**Labels:** `phase/7`, `type/test`, `size/S`, `component/adapter`  
**Dependencies:** KLW-029  
**Branch:** `feature/KLW-028-discord-mention`

### User Story

> As a developer, I want unit tests for `isMentioned`, `stripMention`, and the `onMessage` routing logic so I can refactor with confidence.

### Implementation Plan

**Modify:** `adapters/discord/discord_test.go`

Add the following test functions:

```go
func TestIsMentioned(t *testing.T) {
    botID := "111000111000111000"

    // Must set a.session.State.User — use a minimal fake session state
    // isMentioned reads a.session.State.User.ID; we inject via a stub Adapter.
    // Since discordgo.Session.State is exported and User is settable, we
    // create a real session with a fake token and set State.User directly.
    s, _ := discordgo.New("Bot fake-token")
    s.State.User = &discordgo.User{ID: botID}
    a := &Adapter{session: s, cfg: config.DiscordConfig{}}

    mentioned := &discordgo.MessageCreate{
        Message: &discordgo.Message{
            Mentions: []*discordgo.User{{ID: botID}},
        },
    }
    if !a.isMentioned(mentioned) {
        t.Error("expected isMentioned=true when bot ID in Mentions")
    }

    notMentioned := &discordgo.MessageCreate{
        Message: &discordgo.Message{
            Mentions: []*discordgo.User{{ID: "999999999999999999"}},
        },
    }
    if a.isMentioned(notMentioned) {
        t.Error("expected isMentioned=false when bot ID not in Mentions")
    }

    empty := &discordgo.MessageCreate{
        Message: &discordgo.Message{Mentions: nil},
    }
    if a.isMentioned(empty) {
        t.Error("expected isMentioned=false on empty Mentions")
    }
}

func TestStripMention(t *testing.T) {
    botID := "111000111000111000"
    s, _ := discordgo.New("Bot fake-token")
    s.State.User = &discordgo.User{ID: botID}
    a := &Adapter{session: s, cfg: config.DiscordConfig{}}

    cases := []struct {
        input string
        want  string
    }{
        {fmt.Sprintf("<@%s> explain goroutine leaks", botID), "explain goroutine leaks"},
        {fmt.Sprintf("<@!%s> explain goroutine leaks", botID), "explain goroutine leaks"},
        {fmt.Sprintf("hey <@%s> what time is it?", botID), "hey  what time is it?"},
        {fmt.Sprintf("<@%s>", botID), ""},                    // bare mention
        {fmt.Sprintf("  <@%s>  ", botID), ""},               // whitespace only after strip
        {"no mention here", "no mention here"},
    }

    for _, c := range cases {
        got := a.stripMention(c.input)
        if got != c.want {
            t.Errorf("stripMention(%q) = %q, want %q", c.input, got, c.want)
        }
    }
}

func TestIsMentioned_NilMentions(t *testing.T) {
    s, _ := discordgo.New("Bot fake-token")
    s.State.User = &discordgo.User{ID: "111"}
    a := &Adapter{session: s}
    m := &discordgo.MessageCreate{Message: &discordgo.Message{}}
    if a.isMentioned(m) {
        t.Error("nil Mentions should not be considered a mention")
    }
}
```

Note on `"hey <@%s> what time is it?"` → `"hey  what time is it?"`: the double space is correct (the mention token is replaced with an empty string, leaving the surrounding spaces). If trimming interior spaces is preferred in future, that's a separate enhancement. Current behaviour: `TrimSpace` only trims leading/trailing.

**Run validation:**

```bash
go test ./adapters/discord/... -v -run TestIsMentioned
go test ./adapters/discord/... -v -run TestStripMention
go test ./adapters/discord/...        # full suite
go test ./adapters/telegram/...       # regression check
go build ./...
```

### Acceptance Criteria

- [ ] `TestIsMentioned` passes — all three cases (mentioned, not mentioned, empty)
- [ ] `TestStripMention` passes — standard form, nickname form, mid-sentence, bare, whitespace-only, no mention
- [ ] `TestIsAllowed` still passes (existing)
- [ ] `TestNew_MissingToken` still passes (existing)
- [ ] `go test ./adapters/telegram/...` still green (no regression from refactor)
- [ ] `go build ./...` clean

---

## Manual Test Checklist

Run after all three tickets are merged:

```bash
# 1. Enable mention_response in config
# config.toml: mention_response = true

export DISCORD_BOT_TOKEN=...
./akb48

# Expected startup: "Discord adapter started username=Kabayan#XXXX"

# Test 1: DM (existing path — must still work)
# DM the bot: "Hello"
# Expected: streamed response, no change from Phase 6 behaviour

# Test 2: @mention in server channel
# In any server text channel: "@Kabayan explain what a goroutine is"
# Expected: reply threaded to your message, streams progressively

# Test 3: bare @mention
# "@Kabayan" with no other text
# Expected: no response

# Test 4: mid-sentence mention
# "hey @Kabayan what day is it?"
# Expected: bot receives "hey  what day is it?" — responds correctly

# Test 5: non-allowed user @mention
# Have a non-allowlisted account mention the bot
# Expected: no response, no error message sent to channel

# Test 6: mention_response = false (default)
# Set mention_response = false, restart
# @mention the bot in a server channel
# Expected: no response

# Test 7: overflow in guild channel
# Prompt a response > 1900 chars
# Expected: overflow parts also reply to original mention (all threaded)

# Test 8: /ask unchanged
# /ask what time is it
# Expected: ephemeral response as before
```

---

## Risk Register

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| `s.State.Channel` cache miss on cold start | Low | Falls back to `s.Channel` REST call — adds ~50ms latency on first event |
| `m.Reference()` returns nil on DM (panics if unchecked) | None — DM path passes `nil` explicitly, never calls `m.Reference()` | Handled by routing switch |
| `ChannelMessageSendReply` requires `MessageReference.MessageID` set | Low | `m.Reference()` on a `discordgo.Message` returns a `MessageReference` with `MessageID`, `ChannelID`, `GuildID` pre-populated |
| Double-space in mid-sentence mention strip | Low | `strings.ReplaceAll` leaves adjacent spaces; `TrimSpace` only trims edges. Acceptable; fix with `strings.Join(strings.Fields(...))` in a follow-up if needed |
| Guild news channels (`ChannelTypeGuildNews`) may have restrictions | Low | Included in switch; bot needs `SEND_MESSAGES` permission in that channel |
| `discordgo.Session.State.User` nil before `Open()` completes | Low | `isMentioned`/`stripMention` only called from `onMessage` handler, registered after `Open()` returns; `State.User` is set at that point |

---

## File Change Summary

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `MentionResponse bool` field to `DiscordConfig` |
| `config.toml` | Add `mention_response = false` |
| `deploy/config.docker.toml` | Add `mention_response = false` |
| `adapters/discord/discord.go` | Add `isMentioned()`, `stripMention()`; refactor `onMessage()`; add `ref` param to `process()`, `sendStreaming()`, `editFinal()` |
| `adapters/discord/discord_test.go` | Add `TestIsMentioned`, `TestStripMention`, `TestIsMentioned_NilMentions` |
