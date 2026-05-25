# PRD-07 — Discord @Mention Response in Server Channels

**Ticket:** KLW-028  
**Class:** `class:medium`  
**Phase:** 7  
**Status:** Draft  
**Author:** Dandi  
**Date:** 2026-05-23

---

## Problem

AKB48's Discord adapter currently responds only to DMs and `/ask` slash commands. When the bot is in a shared server, users cannot invoke it by mentioning it in a channel — the most natural Discord interaction pattern. This blocks conversational use from any server channel.

---

## Goal

Enable users to prompt AKB48 by mentioning it (`@Kabayan`) in any server channel. The bot replies in a thread on the original message, preserving conversation context and keeping channels clean.

---

## Non-Goals

- Group sessions (`group:discord:{channel_id}`) — session stays `main:discord:{user_id}` per user
- Multi-user shared sessions in server channels
- Mention detection in DMs (DMs have no `@mention` mechanic; existing path unchanged)
- Slash command changes
- Voice channels

---

## Config Change

Add one opt-in bool to `DiscordConfig`:

```toml
[adapters.discord]
enabled              = true
token_env            = "DISCORD_BOT_TOKEN"
allowed_user_ids     = ["..."]
streaming_interval_ms = 1000
slash_commands       = true
mention_response     = false   # NEW — set true to enable @mention in server channels
```

`mention_response` defaults to `false`. Operator must explicitly opt in.

---

## Behaviour Specification

### Trigger

Message in `ChannelTypeGuildText` or `ChannelTypeGuildNews` where:
1. `mention_response = true`
2. `m.Author` is not a bot
3. `m.Author.ID` is in `AllowedUserIDs`
4. `m.Mentions` contains the bot's own user ID

### Mention Detection

```go
func (a *Adapter) isMentioned(m *discordgo.MessageCreate) bool {
    for _, u := range m.Mentions {
        if u.ID == a.session.State.User.ID {
            return true
        }
    }
    return false
}
```

### Mention Stripping

Strip both standard and nickname-form mentions before sending to LLM:

```go
func (a *Adapter) stripMention(text string) string {
    botID := a.session.State.User.ID
    text = strings.ReplaceAll(text, "<@"+botID+">", "")
    text = strings.ReplaceAll(text, "<@!"+botID+">", "")
    return strings.TrimSpace(text)
}
```

Mid-sentence mentions (e.g., `hey @Kabayan what do you think?`) are accepted — the stripped remainder is sent to the LLM as-is.

### onMessage Routing

```go
func (a *Adapter) onMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
    if m.Author == nil || m.Author.Bot {
        return
    }
    ch, err := s.State.Channel(m.ChannelID)
    if err != nil {
        ch, err = s.Channel(m.ChannelID)
        // handle err
    }

    switch ch.Type {
    case discordgo.ChannelTypeDM:
        // existing DM path — no change
        if !a.isAllowed(m.Author.ID) { return }
        a.process(m.ChannelID, m.Author.ID, m.Content, nil)

    case discordgo.ChannelTypeGuildText, discordgo.ChannelTypeGuildNews:
        if !a.cfg.MentionResponse { return }
        if !a.isMentioned(m) { return }
        if !a.isAllowed(m.Author.ID) { return }
        text := a.stripMention(m.Content)
        if text == "" { return }
        ref := m.Reference()
        a.process(m.ChannelID, m.Author.ID, text, ref)
    }
}
```

### process() Signature Change

```go
// ref is nil for DM path; non-nil for guild-mention path
func (a *Adapter) process(channelID, userID, text string, ref *discordgo.MessageReference)
```

### Threaded Reply

Guild-channel responses use `ChannelMessageSendReply` for the placeholder:

```go
placeholder, err := s.ChannelMessageSendReply(channelID, cursor, ref)
```

Subsequent streaming edits use `ChannelMessageEdit` on the placeholder as usual.

Overflow messages (when response exceeds `overflowAt` chars) also use `ChannelMessageSendReply` with the same `ref`, keeping all parts visually threaded to the original mention.

### Session ID

Unchanged: `main:discord:{user_id}`. Each user's conversation history is shared across DMs, slash commands, and mentions.

---

## Data Flow

```
User types: "@Kabayan explain goroutine leaks"
        │
        ▼
onMessage → ChannelTypeGuildText + isMentioned + isAllowed
        │
        ▼
stripMention → "explain goroutine leaks"
        │
        ▼
process(channelID, userID, text, ref)
        │
        ▼
ChannelMessageSendReply(channelID, "▍", ref)   ← placeholder, threaded
        │
        ▼
rt.HandleMessage → LLM → tokens chan
        │
        ▼
sendStreaming → ChannelMessageEdit ticks
        │
        ▼
editFinal → remove cursor
```

---

## Implementation Plan

| Step | File | Change |
|------|------|--------|
| 1 | `internal/config/config.go` | Add `MentionResponse bool \`toml:"mention_response"\`` to `DiscordConfig` |
| 2 | `deploy/config.docker.toml` | Add `mention_response = false` |
| 3 | `config.toml` | Add `mention_response = false` |
| 4 | `adapters/discord/discord.go` | Add `isMentioned()`, `stripMention()` helpers |
| 5 | `adapters/discord/discord.go` | Refactor `onMessage()` — channel-type switch |
| 6 | `adapters/discord/discord.go` | Add `ref` param to `process()`, `sendStreaming()` |
| 7 | `adapters/discord/discord.go` | Use `ChannelMessageSendReply` when `ref != nil` |
| 8 | `adapters/discord/discord_test.go` | Unit tests for `isMentioned`, `stripMention`, routing |

---

## Acceptance Criteria

- [ ] `mention_response = false` (default): bot ignores all `@mention` messages in server channels
- [ ] `mention_response = true`: bot responds to `@Kabayan <text>` from allowed users in guild text channels
- [ ] Response is threaded to the original mention message
- [ ] Non-allowed users mentioning the bot get no response (silent ignore — no auth error in server channels)
- [ ] Mid-sentence mentions stripped correctly: `hey @Kabayan what time is it` → LLM receives `hey what time is it`
- [ ] Nickname form `<@!BOT_ID>` stripped as well as standard `<@BOT_ID>`
- [ ] Empty text after stripping (bare `@Kabayan` with no other content) → silent ignore
- [ ] DM path unchanged — existing DM tests still pass
- [ ] Slash command path unchanged
- [ ] `go test ./...` passes

---

## Open Questions

None blocking implementation. Two resolved:
- **Q1 (overflow threading):** Overflow messages also use `ChannelMessageSendReply` with same `ref` — all parts thread to original mention.
- **Q2 (mid-sentence mentions):** Accepted — strip mention, send remainder to LLM.

---

## KLW Ticket

**KLW-028** — Discord: @mention response in server channels  
**Label:** `class:medium`  
**Branch:** `feature/KLW-028-discord-mention`
