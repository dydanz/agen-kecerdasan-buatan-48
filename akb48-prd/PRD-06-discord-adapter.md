# PRD-06: Discord Adapter — Default Integration Channel

**Status:** Draft v1.0
**Parent:** PRD-00 (AKB48 Master PRD), PRD-02 (Channel Adapters)
**Author:** Dandi
**Created:** 2026-05-23
**Dependencies:** PRD-01 (Core Runtime), PRD-02 (Channel Adapters — existing interface)
**Estimated Effort:** 5–7 days

---

## 1. Problem

The current default chat interface is Telegram. Discord is increasingly the primary communication hub for technical operators — it offers richer message formatting (Markdown, code blocks, embeds), persistent channels with searchable history, slash command discoverability, and deeper integration into existing engineering workflows (GitHub notifications, deploy alerts, CI feeds).

AKB48 needs Discord as its **default** integration channel while preserving Telegram as a fallback alternative. Only one adapter runs at a time — if Discord is unavailable or the operator prefers Telegram in a given context, they flip the config and restart. The two adapters are not meant to run concurrently; they are hot-swap alternatives for each other.

---

## 2. Goals

- **G1:** Discord is the new default adapter — enabled by default in `config.toml`
- **G2:** Telegram remains fully supported as a fallback — operator switches by toggling `enabled` in config and restarting
- **G3:** Discord DMs from allowlisted user IDs trigger the agent (same security model as Telegram)
- **G4:** Responses stream progressively using Discord's edit-message API
- **G5:** Slash command `/ask` provides discoverable entry point in addition to DM messages
- **G6:** Discord adapter implements the same `runtime.MessageHandler` interface — zero runtime changes
- **G7:** Long code/text responses use Discord embeds or code blocks for readability

---

## 3. Non-Goals

- Running Discord and Telegram simultaneously — one adapter at a time; they are failover alternatives
- Server (guild) channel monitoring — DMs only in Phase 1
- Role-based access control for multi-user servers
- Voice channel integration
- Reaction-based feedback collection
- Discord thread creation for long conversations
- Webhook inbound (gateway WebSocket only in Phase 1)
- Bot status page / rich presence
- Automatic failover (switching adapters requires config change + restart)

---

## 4. User Stories

| ID | Story | Acceptance Criteria |
|----|-------|---------------------|
| US-D01 | As an operator, I DM my Discord bot and get a response in the same DM | Response appears in DM within 3s of sending. Session ID is `main:discord:{user_id}`. |
| US-D02 | As an operator, I use `/ask <message>` in any server channel the bot can see | Bot responds ephemerally (only visible to me) in that channel. |
| US-D03 | As an operator, I see a "thinking..." indicator while the agent processes | `channel.typing` sent before LLM call. |
| US-D04 | As an operator, responses stream progressively (not all-at-once) | First partial response appears within 1s of first token; message edited every `streaming_interval_ms`. |
| US-D05 | As an operator, non-allowed users are silently ignored | Users not in `allowed_user_ids` receive no response. Event logged. |
| US-D06 | As an operator, code in responses renders as Discord code blocks | LLM responses with fenced code blocks (` ``` `) are passed through unmodified — Discord renders them natively. |
| US-D07 | As an operator, responses > 2000 chars split into multiple messages | Discord's limit is 2000 chars. Split at paragraph/newline boundaries. |
| US-D08 | As an operator, if Discord is down I can switch to Telegram by changing one config line and restarting | `adapters.discord.enabled = false` + `adapters.telegram.enabled = true` → restart → Telegram receives messages. Sessions are preserved (same JSONL storage). |

---

## 5. Technical Design

### 5.1 Library

```
github.com/bwmarrin/discordgo v0.28+
```

`discordgo` is the standard Go Discord library. Uses Discord Gateway (WebSocket) for real-time event streaming — no polling, lower latency than Telegram long-polling.

### 5.2 Adapter Struct

**File:** `adapters/discord/discord.go`

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
    "github.com/dydanz/akb48/internal/config"
    "github.com/dydanz/akb48/internal/runtime"
    "github.com/dydanz/akb48/internal/types"
)

const (
    maxMsgLen   = 2000  // Discord hard limit
    overflowAt  = 1900  // leave room for cursor
    cursor      = "▍"
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

### 5.3 Start / Stop

```go
func (a *Adapter) Start(ctx context.Context) error {
    a.session.AddHandler(a.onMessage)
    a.session.AddHandler(a.onInteraction) // slash commands

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

### 5.4 Message Handler

```go
func (a *Adapter) onMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
    if m.Author == nil || m.Author.Bot { return }
    if !a.isAllowed(m.Author.ID) {
        slog.Debug("Discord: message from non-allowed user", "user_id", m.Author.ID)
        return
    }
    // DMs only (channel type check)
    ch, err := s.Channel(m.ChannelID)
    if err != nil || ch.Type != discordgo.ChannelTypeDM {
        return
    }
    go a.process(m.ChannelID, m.Author.ID, m.Content)
}
```

### 5.5 Slash Command Handler

```go
func (a *Adapter) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
    if i.Type != discordgo.InteractionApplicationCommand { return }
    if i.ApplicationCommandData().Name != "ask" { return }
    if !a.isAllowed(i.Member.User.ID) { return }

    query := i.ApplicationCommandData().Options[0].StringValue()
    // ACK immediately (Discord requires response within 3s)
    s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
        Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
        Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
    })
    go a.processInteraction(i, query)
}
```

### 5.6 Streaming via Edit Message

Discord streaming follows the same pattern as Telegram but uses `ChannelMessageEdit`:

```go
func (a *Adapter) sendStreaming(channelID string, tokens <-chan string) error {
    interval := time.Duration(a.cfg.StreamingIntervalMs) * time.Millisecond

    // Send placeholder to get message ID
    msg, err := a.session.ChannelMessageSend(channelID, cursor)
    if err != nil { return err }
    msgID := msg.ID

    var buf strings.Builder
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case token, ok := <-tokens:
            if !ok {
                // Final edit without cursor
                return a.editFinal(channelID, msgID, buf.String())
            }
            buf.WriteString(token)
            if buf.Len() > overflowAt {
                a.editFinal(channelID, msgID, buf.String())
                msg, _ = a.session.ChannelMessageSend(channelID, cursor)
                msgID = msg.ID
                buf.Reset()
            }
        case <-ticker.C:
            if buf.Len() > 0 {
                a.session.ChannelMessageEdit(channelID, msgID, buf.String()+cursor)
            }
        }
    }
}
```

### 5.7 Message Splitting

```go
// splitMessage splits at paragraph → newline → hard cut.
// Discord limit is 2000 chars (vs Telegram's 4096).
func splitMessage(text string, maxLen int) []string
```

Same logic as `adapters/telegram/split.go` — extract to `adapters/shared/split.go` and import from both.

### 5.8 Session ID Format

| Context | Session ID | Permissions |
|---------|-----------|-------------|
| Discord DM | `main:discord:{user_id}` | Full |
| Discord slash command (server) | `main:discord:{user_id}` | Full (ephemeral response) |

Session namespace `main:discord:` is distinct from `main:telegram:` — conversations are isolated per channel by default.

### 5.9 Slash Command Registration

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

Registered globally on `Start()` if `slash_commands = true` in config. Global registration takes up to 1 hour to propagate; guild-specific registration is instant (set `slash_command_guild_id`).

---

## 6. Configuration

### 6.1 New `DiscordConfig` struct

```go
type DiscordConfig struct {
    Enabled             bool     `toml:"enabled"`
    TokenEnv            string   `toml:"token_env"`
    AllowedUserIDs      []string `toml:"allowed_user_ids"` // Discord IDs are strings (snowflakes)
    StreamingIntervalMs int      `toml:"streaming_interval_ms"`
    SlashCommands       bool     `toml:"slash_commands"`
    SlashCommandGuildID string   `toml:"slash_command_guild_id"` // empty = global
}
```

### 6.2 `config.toml` defaults (Discord as default, Telegram as fallback)

Only one adapter should be enabled at a time. To switch, set the active one to `true` and the other to `false`, then restart.

```toml
# Discord is the default. Enable this, disable Telegram.
[adapters.discord]
enabled = true
token_env = "DISCORD_BOT_TOKEN"
allowed_user_ids = []           # Your Discord user ID (snowflake string)
streaming_interval_ms = 1000
slash_commands = true
slash_command_guild_id = ""     # Set for instant slash command registration during dev

# Telegram fallback. Flip to enabled = true when Discord is unavailable.
# Set adapters.discord.enabled = false when using Telegram.
[adapters.telegram]
enabled = false
token_env = "TELEGRAM_BOT_TOKEN"
allowed_user_ids = []
streaming_interval_ms = 1000
```

### 6.3 `.env.example` additions

```bash
DISCORD_BOT_TOKEN=...           # From Discord Developer Portal
TELEGRAM_BOT_TOKEN=...          # Optional — only if telegram adapter enabled
```

---

## 7. Entry Point Wiring

**Modify:** `cmd/akb48/main.go`

Only one chat adapter starts at runtime. If both are enabled in config, Discord takes precedence and a warning is logged.

```go
switch {
case cfg.Adapters.Discord.Enabled && cfg.Adapters.Telegram.Enabled:
    slog.Warn("Both Discord and Telegram enabled — Discord takes precedence. Disable one.")
    fallthrough
case cfg.Adapters.Discord.Enabled:
    dc, err := discord.New(cfg.Adapters.Discord, rt.HandleMessage)
    if err != nil {
        slog.Error("Discord adapter init failed", "error", err)
        os.Exit(1)
    }
    go func() {
        if err := dc.Start(ctx); err != nil && err != context.Canceled {
            slog.Error("Discord adapter error", "error", err)
        }
    }()
    slog.Info("Discord adapter started")

case cfg.Adapters.Telegram.Enabled:
    tg, err := telegram.New(cfg.Adapters.Telegram, rt.HandleMessage)
    if err != nil {
        slog.Error("Telegram adapter init failed", "error", err)
        os.Exit(1)
    }
    go func() {
        if err := tg.Start(ctx); err != nil && err != context.Canceled {
            slog.Error("Telegram adapter error", "error", err)
        }
    }()
    slog.Info("Telegram adapter started (fallback mode)")
}
```

**Switching adapters (failover):**
```bash
# Discord down → switch to Telegram
# Edit config.toml:
#   adapters.discord.enabled = false
#   adapters.telegram.enabled = true
# Then:
make restart
# Sessions persist — JSONL storage is channel-agnostic
```

---

## 8. Refactor: Shared Split Utility

`adapters/telegram/split.go` → move to `adapters/shared/split.go`, export `SplitMessage`. Both Discord and Telegram import from shared.

**Before:**
```
adapters/telegram/split.go  (private to telegram package)
```

**After:**
```
adapters/shared/split.go    (exported SplitMessage, used by both)
adapters/telegram/          (imports shared.SplitMessage, maxLen=4096)
adapters/discord/           (imports shared.SplitMessage, maxLen=2000)
```

---

## 9. Testing Plan

### 9.1 Unit tests (`adapters/discord/discord_test.go`)

| Test | What it verifies |
|------|-----------------|
| `TestIsAllowed` | Allowlist enforcement (string snowflake IDs) |
| `TestSplitMessage_Discord` | 2000-char limit, paragraph split |
| `TestStreamingInterval` | Default + backoff math |
| `TestSessionIDFormat` | `main:discord:{user_id}` format |
| `TestNew_MissingToken` | Returns error when `DISCORD_BOT_TOKEN` not set |

### 9.2 Integration test addition (`tests/integration/hello_world_test.go`)

```go
func TestHelloWorld_DiscordDM(t *testing.T) {
    // Build runtime with mock Discord adapter
    // Send DM from allowed user → assert response
    // Send DM from non-allowed user → assert no response
}
```

### 9.3 Manual test checklist

```bash
# Set env vars
export DISCORD_BOT_TOKEN=...
# Add Discord user ID to config.toml allowed_user_ids

./akb48
# In Discord: DM the bot "Hello, who are you?"
# Expected: personality-consistent response, streaming
# In Discord: "/ask remember that staging cluster is ap-southeast-1"
# Expected: ephemeral "Stored." response
```

---

## 10. Discord Bot Setup Instructions

1. Go to https://discord.com/developers/applications
2. Create New Application → Bot tab → Add Bot
3. Copy token → `DISCORD_BOT_TOKEN`
4. Enable: **Message Content Intent** + **Direct Messages** under Privileged Gateway Intents
5. Bot permissions needed: `Send Messages`, `Read Message History`, `Use Slash Commands`
6. OAuth2 → URL Generator → Scopes: `bot`, `applications.commands` → invite bot to your server
7. Get your Discord user ID: Settings → Advanced → Developer Mode ON → right-click your username → Copy ID
8. Add ID to `allowed_user_ids` in `config.toml`

---

## 11. Acceptance Criteria

- [ ] `./akb48` starts with Discord adapter by default (`adapters.discord.enabled = true`)
- [ ] Discord DM from allowed user → response streamed progressively
- [ ] Discord DM from non-allowed user → silently ignored, logged
- [ ] `/ask <message>` slash command → ephemeral streaming response
- [ ] Response > 2000 chars → split at paragraph boundary, all parts delivered
- [ ] Telegram adapter works as fallback: `adapters.discord.enabled = false` + `adapters.telegram.enabled = true` → Telegram receives messages, sessions preserved
- [ ] Both enabled simultaneously → Discord takes precedence, `slog.Warn` logged, Telegram silent
- [ ] `DISCORD_BOT_TOKEN` not set → startup error with clear message
- [ ] `go test ./adapters/discord/...` passes (no real Discord needed)
- [ ] `go build ./...` clean
- [ ] Startup banner: `"Discord adapter started username=YourBot#1234"`

---

## 12. Implementation Tickets

| Ticket | Description | SP |
|--------|-------------|-----|
| KLW-022 | `adapters/shared/split.go` — extract SplitMessage, update Telegram import | 1 |
| KLW-023 | `DiscordConfig` struct + config.toml defaults + applyDefaults | 2 |
| KLW-024 | Discord adapter core — New, Start, onMessage, isAllowed, process | 5 |
| KLW-025 | Discord streaming — sendStreaming, editFinal, overflow | 3 |
| KLW-026 | Slash command registration + onInteraction handler | 3 |
| KLW-027 | Entry point wiring + integration test | 2 |

**Total:** 16 SP · ~1 week

---

## 13. Risks

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Discord rate limits on edit-message (5/s per channel) | Medium | Back off on 429, increase `streaming_interval_ms` |
| Slash command global propagation delay (up to 1h) | Low | Use `slash_command_guild_id` during development for instant registration |
| `discordgo` WebSocket reconnect behavior | Low | Library handles reconnects automatically; test with network interruption |
| Discord snowflake IDs are strings not int64 | Low | `AllowedUserIDs []string` in config (unlike Telegram's `[]int64`) |
