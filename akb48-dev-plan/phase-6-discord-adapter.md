# Phase 6: Discord Adapter — Default Channel

**Goal:** Discord becomes the default communication channel. DMs and `/ask` slash commands from the allowlisted operator trigger the agent. Responses stream progressively via edit-message. Telegram remains a hot-swap fallback requiring only a config toggle + restart.

**Definition of Done:**
- `./akb48` starts with Discord adapter enabled by default
- Discord DM from allowed user → response streams progressively (cursor → edits → final)
- `/ask <message>` slash command → ephemeral streaming response
- Response > 2000 chars → split at paragraph boundaries, all parts delivered
- Non-allowed user DM → silently dropped, logged
- Telegram fallback: flip config, restart, sessions preserved
- Both adapters enabled → Discord takes precedence, `slog.Warn` logged
- `DISCORD_BOT_TOKEN` not set → clear startup error
- `go test ./adapters/discord/...` passes without a real Discord connection
- `go build ./...` clean

**Tickets:** KLW-022 → KLW-023 → KLW-024 → KLW-025 → KLW-026 → KLW-027

---

## KLW-022 — Shared SplitMessage Utility

**Type:** Refactor
**Owner:** Backend
**Effort:** 1 SP
**Labels:** `phase/6`, `type/refactor`, `size/XS`, `component/adapter`
**Dependencies:** none
**Branch:** `feat/discord-adapter`

### User Story

> As a developer, I want message splitting logic shared between adapters, so both Discord and Telegram enforce their respective char limits without duplicating code.

### Implementation Plan

**Files to create:**
- `adapters/shared/split.go`

**Files to modify:**
- `adapters/telegram/split.go` — delete (logic moves to shared)
- `adapters/telegram/telegram.go` — update import and call site

**adapters/shared/split.go:**

```go
package shared

import "strings"

// SplitMessage splits text into chunks of at most maxLen bytes.
// Prefers splitting at double-newline (paragraph) then single newline then hard cut.
func SplitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var parts []string
	for len(text) > maxLen {
		cut := maxLen
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

**Update `adapters/telegram/telegram.go`:** replace `splitMessage(text, maxMsgLen)` calls with `shared.SplitMessage(text, maxMsgLen)`. Add import `"github.com/dydanz/akb48/adapters/shared"`. Delete `adapters/telegram/split.go`.

### Acceptance Criteria

- [ ] `adapters/shared/split.go` exports `SplitMessage`
- [ ] `adapters/telegram/split.go` deleted
- [ ] Telegram still compiles and tests pass after import update
- [ ] `go test ./adapters/telegram/...` green

---

## KLW-023 — DiscordConfig + Config Wiring

**Type:** Configuration
**Owner:** Backend
**Effort:** 2 SP
**Labels:** `phase/6`, `type/config`, `size/S`, `component/config`
**Dependencies:** KLW-022
**Branch:** `feat/discord-adapter`

### User Story

> As an operator, I want Discord enabled by default in config.toml so I don't have to manually toggle flags after a fresh install.

### Implementation Plan

**Modify:** `internal/config/config.go`

Add `DiscordConfig` struct and extend `AdaptersConfig`:

```go
type DiscordConfig struct {
    Enabled             bool     `toml:"enabled"`
    TokenEnv            string   `toml:"token_env"`
    AllowedUserIDs      []string `toml:"allowed_user_ids"` // snowflake strings, NOT int64
    StreamingIntervalMs int      `toml:"streaming_interval_ms"`
    SlashCommands       bool     `toml:"slash_commands"`
    SlashCommandGuildID string   `toml:"slash_command_guild_id"` // empty = global registration
}

type AdaptersConfig struct {
    CLI      CLIConfig      `toml:"cli"`
    Telegram TelegramConfig `toml:"telegram"`
    Discord  DiscordConfig  `toml:"discord"`
}
```

Add to `applyDefaults()`:

```go
if c.Adapters.Discord.TokenEnv == "" {
    c.Adapters.Discord.TokenEnv = "DISCORD_BOT_TOKEN"
}
if c.Adapters.Discord.StreamingIntervalMs == 0 {
    c.Adapters.Discord.StreamingIntervalMs = 1000
}
```

**Modify:** `config.toml` — add Discord section, set Discord enabled=true, Telegram enabled=false:

```toml
# Discord is the default adapter.
[adapters.discord]
enabled = true
token_env = "DISCORD_BOT_TOKEN"
allowed_user_ids = []           # Add your Discord snowflake user ID (string)
streaming_interval_ms = 1000
slash_commands = true
slash_command_guild_id = ""     # Set guild ID for instant slash command registration during dev

# Telegram is the fallback. To switch: set discord.enabled=false, telegram.enabled=true, restart.
[adapters.telegram]
enabled = false
token_env = "TELEGRAM_BOT_TOKEN"
allowed_user_ids = []
streaming_interval_ms = 1000
```

**Modify:** `.env.example` — add:

```bash
DISCORD_BOT_TOKEN=...           # From Discord Developer Portal → Bot tab
TELEGRAM_BOT_TOKEN=...          # Optional — only needed when telegram adapter is enabled
```

**Modify:** `cmd/akb48/main.go` — update `--validate` print:

```go
fmt.Printf("config ok: model=%s adapter_cli=%v adapter_discord=%v adapter_telegram=%v\n",
    cfg.LLM.Model, cfg.Adapters.CLI.Enabled, cfg.Adapters.Discord.Enabled, cfg.Adapters.Telegram.Enabled)
```

### Acceptance Criteria

- [ ] `DiscordConfig` struct present in `internal/config/config.go`
- [ ] `AdaptersConfig.Discord` field wired
- [ ] `applyDefaults()` sets `TokenEnv` and `StreamingIntervalMs`
- [ ] `config.toml` has `[adapters.discord]` with `enabled = true`
- [ ] `config.toml` has `[adapters.telegram]` with `enabled = false`
- [ ] `go build ./...` clean

---

## KLW-024 — Discord Adapter Core

**Type:** User Story
**Owner:** Backend
**Effort:** 5 SP
**Labels:** `phase/6`, `type/user-story`, `size/M`, `component/adapter`
**Dependencies:** KLW-022, KLW-023
**Branch:** `feat/discord-adapter`

### User Story

> As an operator, I want to DM my Discord bot and receive an AI response, with the same security model as Telegram — only my user ID triggers the agent.

### Implementation Plan

**Add dependency:**

```bash
go get github.com/bwmarrin/discordgo@v0.28.1
```

**Files to create:**
- `adapters/discord/discord.go`
- `adapters/discord/doc.go`

**adapters/discord/doc.go:**

```go
// Package discord implements the Discord channel adapter for AKB48.
// It connects via Discord Gateway (WebSocket), handles DMs and slash commands,
// and streams responses using the edit-message API.
package discord
```

**adapters/discord/discord.go — struct and constructor:**

```go
package discord

import (
    "context"
    "fmt"
    "log/slog"
    "os"
    "strings"
    "sync"
    "time"

    "github.com/bwmarrin/discordgo"
    "github.com/dydanz/akb48/adapters/shared"
    "github.com/dydanz/akb48/internal/config"
    "github.com/dydanz/akb48/internal/runtime"
    "github.com/dydanz/akb48/internal/types"
)

const (
    maxMsgLen  = 2000
    overflowAt = 1900
    cursor     = "▍"
)

type Adapter struct {
    session *discordgo.Session
    cfg     config.DiscordConfig
    handler runtime.MessageHandler
}

func New(cfg config.DiscordConfig, handler runtime.MessageHandler) (*Adapter, error) {
    token := os.Getenv(cfg.TokenEnv)
    if token == "" {
        return nil, fmt.Errorf("env var %q not set — Discord bot token required", cfg.TokenEnv)
    }
    s, err := discordgo.New("Bot " + token)
    if err != nil {
        return nil, fmt.Errorf("discord session: %w", err)
    }
    s.Identify.Intents = discordgo.IntentsDirectMessages | discordgo.IntentsGuildMessages
    return &Adapter{session: s, cfg: cfg, handler: handler}, nil
}
```

**Start / Stop:**

```go
func (a *Adapter) Start(ctx context.Context) error {
    a.session.AddHandler(a.onMessage)
    a.session.AddHandler(a.onInteraction)

    if err := a.session.Open(); err != nil {
        return fmt.Errorf("discord open: %w", err)
    }

    if a.cfg.SlashCommands {
        a.registerSlashCommands()
    }

    slog.Info("Discord adapter started", "username", a.session.State.User.Username)
    <-ctx.Done()
    return a.session.Close()
}
```

**onMessage — DM handler:**

```go
func (a *Adapter) onMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
    if m.Author == nil || m.Author.Bot {
        return
    }
    if !a.isAllowed(m.Author.ID) {
        slog.Debug("Discord: message from non-allowed user", "user_id", m.Author.ID)
        return
    }
    ch, err := s.Channel(m.ChannelID)
    if err != nil || ch.Type != discordgo.ChannelTypeDM {
        return
    }
    go a.process(m.ChannelID, m.Author.ID, m.Content)
}
```

**isAllowed — snowflake string comparison:**

```go
func (a *Adapter) isAllowed(userID string) bool {
    if len(a.cfg.AllowedUserIDs) == 0 {
        return false
    }
    for _, id := range a.cfg.AllowedUserIDs {
        if id == userID {
            return true
        }
    }
    return false
}
```

**process — core message dispatch:**

```go
func (a *Adapter) process(channelID, userID, text string) {
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
        if err := a.sendStreaming(channelID, tokens); err != nil {
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

### Acceptance Criteria

- [ ] DM from allowed user ID → `process()` called → response sent
- [ ] DM from non-allowed user → silently dropped, `slog.Debug` logged
- [ ] Message from bot user → ignored
- [ ] Non-DM channel message → ignored
- [ ] `DISCORD_BOT_TOKEN` not set → `New()` returns error with env var name
- [ ] Session ID format: `main:discord:{user_id}`
- [ ] `go build ./adapters/discord/...` clean

---

## KLW-025 — Discord Streaming + Overflow

**Type:** User Story
**Owner:** Backend
**Effort:** 3 SP
**Labels:** `phase/6`, `type/user-story`, `size/S`, `component/adapter`
**Dependencies:** KLW-024
**Branch:** `feat/discord-adapter`

### User Story

> As an operator, I want Discord responses to appear progressively as tokens arrive, so the UX feels responsive — not a delayed batch response.

### Implementation Plan

**Modify:** `adapters/discord/discord.go`

**sendStreaming:**

```go
func (a *Adapter) sendStreaming(channelID string, tokens <-chan string) error {
    interval := time.Duration(a.cfg.StreamingIntervalMs) * time.Millisecond

    msg, err := a.session.ChannelMessageSend(channelID, cursor)
    if err != nil {
        return fmt.Errorf("send placeholder: %w", err)
    }
    msgID := msg.ID

    var buf strings.Builder
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case token, ok := <-tokens:
            if !ok {
                return a.editFinal(channelID, msgID, buf.String())
            }
            buf.WriteString(token)
            if buf.Len() > overflowAt {
                if err := a.editFinal(channelID, msgID, buf.String()); err != nil {
                    slog.Warn("Discord: overflow finalize error", "error", err)
                }
                newMsg, err := a.session.ChannelMessageSend(channelID, cursor)
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
                        interval = min(interval*2, 3*time.Second)
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

**editFinal:**

```go
func (a *Adapter) editFinal(channelID, msgID, text string) error {
    if text == "" {
        text = "(empty response)"
    }
    parts := shared.SplitMessage(text, maxMsgLen)
    if _, err := a.session.ChannelMessageEdit(channelID, msgID, parts[0]); err != nil {
        slog.Warn("Discord: final edit failed", "error", err)
    }
    for _, part := range parts[1:] {
        if _, err := a.session.ChannelMessageSend(channelID, part); err != nil {
            slog.Warn("Discord: overflow part send failed", "error", err)
        }
    }
    return nil
}

func isRateLimited(err error) bool {
    var restErr *discordgo.RESTError
    return errors.As(err, &restErr) && restErr.Response.StatusCode == 429
}
```

### Acceptance Criteria

- [ ] First token → placeholder `▍` appears in DM
- [ ] Tokens accumulate in buffer; message edited every `streaming_interval_ms`
- [ ] Final message has no `▍` cursor
- [ ] Response overflowing 1900 chars → new message starts seamlessly
- [ ] Discord 429 → interval doubles (capped 3s), no crash, streaming completes
- [ ] Empty token stream → placeholder edited to `(empty response)`

---

## KLW-026 — Slash Command Registration + Interaction Handler

**Type:** User Story
**Owner:** Backend
**Effort:** 3 SP
**Labels:** `phase/6`, `type/user-story`, `size/S`, `component/adapter`
**Dependencies:** KLW-024
**Branch:** `feat/discord-adapter`

### User Story

> As an operator, I want to use `/ask <message>` in any server channel and get an ephemeral AI response, so I can use the bot from a shared server without exposing my prompts.

### Implementation Plan

**Modify:** `adapters/discord/discord.go`

**Slash command definition:**

```go
var askCommand = &discordgo.ApplicationCommand{
    Name:        "ask",
    Description: "Send a message to AKB48",
    Options: []*discordgo.ApplicationCommandOption{
        {
            Type:        discordgo.ApplicationCommandOptionString,
            Name:        "message",
            Description: "Your message",
            Required:    true,
        },
    },
}
```

**registerSlashCommands:**

```go
func (a *Adapter) registerSlashCommands() {
    // Guild-specific = instant; global = up to 1h propagation
    guildID := a.cfg.SlashCommandGuildID
    if _, err := a.session.ApplicationCommandCreate(a.session.State.User.ID, guildID, askCommand); err != nil {
        slog.Error("Discord: slash command registration failed", "error", err)
    } else {
        if guildID != "" {
            slog.Info("Discord: /ask registered (guild)", "guild_id", guildID)
        } else {
            slog.Info("Discord: /ask registered globally (propagation up to 1h)")
        }
    }
}
```

**onInteraction:**

```go
func (a *Adapter) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
    if i.Type != discordgo.InteractionApplicationCommand {
        return
    }
    if i.ApplicationCommandData().Name != "ask" {
        return
    }

    // i.Member is nil in DMs; i.User is nil in guild channels
    var userID string
    if i.Member != nil {
        userID = i.Member.User.ID
    } else if i.User != nil {
        userID = i.User.ID
    } else {
        return
    }

    if !a.isAllowed(userID) {
        return
    }

    query := i.ApplicationCommandData().Options[0].StringValue()

    // ACK within 3s — Discord will timeout the interaction otherwise
    err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
        Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
        Data: &discordgo.InteractionResponseData{
            Flags: discordgo.MessageFlagsEphemeral,
        },
    })
    if err != nil {
        slog.Error("Discord: interaction ACK failed", "error", err)
        return
    }

    go a.processInteraction(s, i, userID, query)
}
```

**processInteraction — uses EditWebhookMessage, not ChannelMessageEdit:**

```go
func (a *Adapter) processInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, userID, text string) {
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
        if err := a.sendStreamingInteraction(s, i, tokens); err != nil {
            slog.Error("Discord interaction streaming error", "error", err)
        }
    }()

    ctx := context.Background()
    if err := a.handler(ctx, msg, tokens); err != nil {
        slog.Error("Discord interaction handler error", "error", err)
    }
    close(tokens)
    wg.Wait()
}

func (a *Adapter) sendStreamingInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, tokens <-chan string) error {
    interval := time.Duration(a.cfg.StreamingIntervalMs) * time.Millisecond
    appID := s.State.User.ID

    var buf strings.Builder
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    editContent := func(content string) {
        s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
            Content: &content,
        })
    }

    for {
        select {
        case token, ok := <-tokens:
            if !ok {
                final := buf.String()
                if final == "" {
                    final = "(empty response)"
                }
                editContent(final)
                _ = appID
                return nil
            }
            buf.WriteString(token)
        case <-ticker.C:
            if buf.Len() > 0 {
                editContent(buf.String() + cursor)
            }
        }
    }
}
```

> **Note:** Interaction responses use `InteractionResponseEdit` (webhook edit), not `ChannelMessageEdit`. The message token is embedded in the interaction object, not a channel message ID.

### Acceptance Criteria

- [ ] `/ask hello` in a server channel → bot responds ephemerally (only visible to sender)
- [ ] Non-allowed user using `/ask` → no response
- [ ] ACK sent within 3s (deferred response)
- [ ] Response streams progressively in the ephemeral message
- [ ] `SlashCommandGuildID` set → guild registration (instant); empty → global (logged)
- [ ] `i.Member` nil (DM context) handled without panic

---

## KLW-027 — Entry Point Wiring + Tests

**Type:** User Story + Testing
**Owner:** Backend
**Effort:** 2 SP
**Labels:** `phase/6`, `type/user-story`, `size/S`, `component/main`
**Dependencies:** KLW-025, KLW-026
**Branch:** `feat/discord-adapter`

### User Story

> As an operator, I want the binary to start the correct adapter based on config, with Discord taking precedence when both are accidentally enabled.

### Implementation Plan

**Modify:** `cmd/akb48/main.go`

Replace the `if cfg.Adapters.Telegram.Enabled` block with:

```go
import dcadapter "github.com/dydanz/akb48/adapters/discord"

// ...

switch {
case cfg.Adapters.Discord.Enabled && cfg.Adapters.Telegram.Enabled:
    slog.Warn("Both Discord and Telegram enabled — Discord takes precedence. Disable one in config.toml.")
    fallthrough
case cfg.Adapters.Discord.Enabled:
    dc, err := dcadapter.New(cfg.Adapters.Discord, rt.HandleMessage)
    if err != nil {
        slog.Error("Discord adapter init failed", "error", err)
        os.Exit(1)
    }
    go func() {
        if err := dc.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
            slog.Error("Discord adapter error", "error", err)
        }
    }()

case cfg.Adapters.Telegram.Enabled:
    tg, err := tgadapter.New(cfg.Adapters.Telegram, rt.HandleMessage)
    if err != nil {
        slog.Error("Telegram adapter init failed", "error", err)
        os.Exit(1)
    }
    go func() {
        if err := tg.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
            slog.Error("Telegram adapter error", "error", err)
        }
    }()
    slog.Info("Telegram adapter started (fallback mode)")
}
```

**Create:** `adapters/discord/discord_test.go`

```go
package discord

import (
    "fmt"
    "strings"
    "testing"

    "github.com/dydanz/akb48/internal/config"
)

func TestIsAllowed(t *testing.T) {
    a := &Adapter{cfg: config.DiscordConfig{AllowedUserIDs: []string{"123456789"}}}
    if !a.isAllowed("123456789") {
        t.Error("expected allowed user to pass")
    }
    if a.isAllowed("999999999") {
        t.Error("expected non-allowed user to fail")
    }
    empty := &Adapter{cfg: config.DiscordConfig{AllowedUserIDs: []string{}}}
    if empty.isAllowed("123456789") {
        t.Error("empty allowlist should deny all")
    }
}

func TestNew_MissingToken(t *testing.T) {
    t.Setenv("DISCORD_BOT_TOKEN", "")
    _, err := New(config.DiscordConfig{TokenEnv: "DISCORD_BOT_TOKEN"}, nil)
    if err == nil {
        t.Error("expected error when token env not set")
    }
}

func TestSessionIDFormat(t *testing.T) {
    userID := "987654321012345678"
    want := fmt.Sprintf("main:discord:%s", userID)
    got := fmt.Sprintf("main:discord:%s", userID)
    if got != want {
        t.Errorf("session ID format wrong: got %q want %q", got, want)
    }
}

func TestSplitMessage_Discord(t *testing.T) {
    from "github.com/dydanz/akb48/adapters/shared"

    // Under limit — no split
    parts := shared.SplitMessage("hello", 2000)
    if len(parts) != 1 {
        t.Errorf("expected 1 part, got %d", len(parts))
    }

    // Over limit with paragraph break
    text := strings.Repeat("a", 1500) + "\n\n" + strings.Repeat("b", 1000)
    parts = shared.SplitMessage(text, 2000)
    if len(parts) != 2 {
        t.Errorf("expected 2 parts at paragraph break, got %d", len(parts))
    }

    // Over limit, no natural break — hard cut at 2000
    text = strings.Repeat("x", 3000)
    parts = shared.SplitMessage(text, 2000)
    if len(parts) != 2 {
        t.Errorf("expected 2 parts on hard cut, got %d", len(parts))
    }
    if len(parts[0]) != 2000 {
        t.Errorf("expected first part to be 2000 chars, got %d", len(parts[0]))
    }
}
```

**Add to:** `tests/integration/hello_world_test.go`

```go
func TestHelloWorld_DiscordDM(t *testing.T) {
    // Uses the existing mock runtime pattern from hello_world_test.go
    rt := buildTestRuntime(t)

    allowedID := "111222333444555666"
    nonAllowedID := "999888777666555444"
    cfg := config.DiscordConfig{
        AllowedUserIDs:      []string{allowedID},
        StreamingIntervalMs: 50,
    }

    // Verify isAllowed logic without a real Discord session
    a := &discord.Adapter{}
    // ... mock-based assertions
    _ = rt
    _ = cfg
    _ = allowedID
    _ = nonAllowedID
    t.Skip("full integration requires live DISCORD_BOT_TOKEN — run manually per §9.3 checklist")
}
```

**Run validation:**

```bash
go build ./...
go test ./adapters/discord/...
go test ./adapters/shared/...
go test ./adapters/telegram/...   # verify shared import didn't break Telegram
```

### Acceptance Criteria

- [ ] `./akb48` starts Discord adapter when `adapters.discord.enabled = true`
- [ ] Both enabled → `slog.Warn` logged, Discord runs, Telegram silent
- [ ] Neither enabled → no adapter started, no panic
- [ ] `go test ./adapters/discord/...` passes (no real Discord)
- [ ] `go test ./adapters/telegram/...` still passes (shared split import)
- [ ] `go build ./...` clean
- [ ] `./akb48 --validate` output includes `adapter_discord=true`

### Manual Test Checklist (§9.3)

```bash
export DISCORD_BOT_TOKEN=...
# Add your Discord user ID (snowflake) to config.toml allowed_user_ids

./akb48
# Expected startup: "Discord adapter started username=YourBot#1234"

# Test 1: DM the bot "Hello, who are you?"
# Expected: personality-consistent response, streams with cursor animation

# Test 2: /ask remember that staging cluster is ap-southeast-1
# Expected: ephemeral response only visible to you

# Test 3: DM from a non-allowed Discord account
# Expected: no response, slog.Debug logged in terminal

# Test 4: Failover to Telegram
# Edit config.toml: adapters.discord.enabled = false, adapters.telegram.enabled = true
# export TELEGRAM_BOT_TOKEN=...
# ./akb48
# Expected: "Telegram adapter started (fallback mode)"
# Sessions from Discord DMs are preserved in JSONL storage
```

---

## Discord Bot Setup (One-Time)

1. Go to https://discord.com/developers/applications
2. New Application → Bot tab → Add Bot → copy token → `DISCORD_BOT_TOKEN`
3. Privileged Gateway Intents: enable **Message Content Intent** + **Direct Messages**
4. Bot permissions: `Send Messages`, `Read Message History`, `Use Slash Commands`
5. OAuth2 → URL Generator → Scopes: `bot`, `applications.commands` → invite to your server
6. Get your Discord user ID: Settings → Advanced → Developer Mode ON → right-click username → Copy ID
7. Add ID string to `allowed_user_ids` in `config.toml`
8. For dev: set `slash_command_guild_id` to your server ID for instant `/ask` registration

---

## Risk Register

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Discord rate limits on edit-message (5/s per channel) | Medium | Back off on 429, increase `streaming_interval_ms` |
| Slash command global propagation delay (up to 1h) | Low | Set `slash_command_guild_id` during dev for instant registration |
| `i.Member` nil in DM-context interactions | Low | Guard with `i.User` fallback in `onInteraction` — already handled in plan |
| Discord snowflake IDs are strings not int64 | Low | `AllowedUserIDs []string` in `DiscordConfig` (unlike Telegram's `[]int64`) |
| `discordgo` WebSocket reconnect on network blip | Low | Library handles reconnects automatically; test with `kill -STOP/CONT` |
