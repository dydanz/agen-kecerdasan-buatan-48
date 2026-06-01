package discord

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/dydanz/akb48/adapters/shared"
	"github.com/dydanz/akb48/internal/config"
	"github.com/dydanz/akb48/internal/runtime"
	"github.com/dydanz/akb48/internal/types"
)

// processedMsgTTL is how long a processed message ID is remembered to deduplicate
// duplicate Gateway events (e.g., two container instances sharing the same bot token).
const processedMsgTTL = 5 * time.Minute

// handlerTimeout is the maximum time the LLM handler may take per turn.
// Prevents the placeholder from staying as ▍ forever when the backend hangs.
const handlerTimeout = 90 * time.Second

const (
	maxMsgLen  = 2000
	overflowAt = 1900
	cursor     = "▍"
	maxBackoff = 3 * time.Second
)

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

// Adapter handles Discord Gateway events and per-message streaming.
type Adapter struct {
	session       *discordgo.Session
	cfg           config.DiscordConfig
	handler       runtime.MessageHandler
	started       atomic.Bool
	processedMsgs sync.Map // map[string]time.Time — message ID deduplication
}

// New creates a Discord adapter. Returns error if bot token is missing.
func New(cfg config.DiscordConfig, handler runtime.MessageHandler) (*Adapter, error) {
	token := os.Getenv(cfg.TokenEnv)
	if token == "" {
		return nil, fmt.Errorf("env var %q not set — Discord bot token required", cfg.TokenEnv)
	}
	s, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("discord session: %w", err)
	}
	s.Identify.Intents = discordgo.IntentsDirectMessages | discordgo.IntentsGuildMessages | discordgo.IntentsMessageContent
	return &Adapter{session: s, cfg: cfg, handler: handler}, nil
}

// Start opens the Gateway connection and blocks until ctx is cancelled.
// Safe to call only once — returns error on a second call to prevent duplicate handlers.
func (a *Adapter) Start(ctx context.Context) error {
	if !a.started.CompareAndSwap(false, true) {
		return fmt.Errorf("discord adapter already started — double Start() would register duplicate handlers")
	}

	a.session.AddHandler(a.onMessage)
	a.session.AddHandler(a.onInteraction)
	a.session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		slog.Info("Discord adapter started", "username", r.User.Username)
		if a.cfg.SlashCommands {
			a.registerSlashCommands()
		}
	})

	if err := a.session.Open(); err != nil {
		return fmt.Errorf("discord open: %w", err)
	}

	<-ctx.Done()
	return a.session.Close()
}

// onMessage routes DMs to the existing path and guild-channel @mentions (opt-in) to
// the threaded reply path.
func (a *Adapter) onMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot {
		return
	}

	// Deduplicate: drop duplicate Gateway events for the same message ID.
	// Protects against two container instances sharing the same bot token,
	// or any other scenario where onMessage fires twice for one Discord event.
	if _, seen := a.processedMsgs.LoadOrStore(m.ID, time.Now()); seen {
		slog.Debug("Discord: duplicate message event dropped", "message_id", m.ID)
		return
	}
	// Purge stale entries to prevent unbounded growth.
	a.processedMsgs.Range(func(k, v any) bool {
		if time.Since(v.(time.Time)) > processedMsgTTL {
			a.processedMsgs.Delete(k)
		}
		return true
	})

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
			return
		}
		text := a.stripMention(m.Content)
		if text == "" {
			return
		}
		go a.process(m.ChannelID, m.Author.ID, text, m.Reference())
	}
}

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

// onInteraction handles /ask slash commands.
func (a *Adapter) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}
	if i.ApplicationCommandData().Name != "ask" {
		return
	}

	// i.Member is nil in DM interactions; i.User is nil in guild channels.
	var userID string
	if i.Member != nil {
		userID = i.Member.User.ID
	} else if i.User != nil {
		userID = i.User.ID
	} else {
		return
	}

	if !a.isAllowed(userID) {
		// Must ACK — returning without responding causes Discord to show "interaction failed".
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Not authorized.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	query := i.ApplicationCommandData().Options[0].StringValue()

	// ACK within 3s — Discord times out unacknowledged interactions.
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

func (a *Adapter) process(channelID, userID, text string, ref *discordgo.MessageReference) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			a.session.ChannelTyping(channelID)
			select {
			case <-ctx.Done():
				return
			case <-time.After(8 * time.Second):
			}
		}
	}()

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

	// Enforce a hard timeout so the placeholder never stays as ▍ forever.
	hCtx, hCancel := context.WithTimeout(ctx, handlerTimeout)
	defer hCancel()

	if err := a.handler(hCtx, msg, tokens); err != nil {
		slog.Error("Discord handler error", "error", err)
		cancel()     // stop ChannelTyping immediately — don't wait for sendStreaming
		close(tokens)
		waitWithTimeout(&wg)
		a.session.ChannelMessageSend(channelID, fmt.Sprintf("Error: %v", err))
		return
	}
	cancel()     // stop ChannelTyping immediately — don't wait for sendStreaming
	close(tokens)
	waitWithTimeout(&wg)
}

// waitWithTimeout waits for wg with a 15s hard cap.
// Prevents process() from blocking forever if sendStreaming is stuck on a
// rate-limited Discord API call (editFinal → ChannelMessageEdit → backoff).
func waitWithTimeout(wg *sync.WaitGroup) {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		slog.Warn("sendStreaming did not finish within 15s — leaking goroutine")
	}
}

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
		close(tokens)
		wg.Wait()
		errMsg := fmt.Sprintf("Error: %v", err)
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &errMsg})
		return
	}
	close(tokens)
	wg.Wait()
}

// sendStreaming sends tokens as progressive edits to a placeholder message.
// ref is non-nil for guild @mention replies — uses ChannelMessageSendReply for threading.
//
// Overflow handling: when buf exceeds overflowAt the current message is finalised and
// msgID is cleared. The ticker creates the NEXT message only when it has actual content —
// preventing a second bare ▍ placeholder from appearing while the stream is still running.
func (a *Adapter) sendStreaming(channelID string, tokens <-chan string, ref *discordgo.MessageReference) error {
	interval := time.Duration(a.cfg.StreamingIntervalMs) * time.Millisecond

	// sendMsg creates a new Discord message (reply or plain) with the given text.
	sendMsg := func(text string) (*discordgo.Message, error) {
		if ref != nil {
			return a.session.ChannelMessageSendReply(channelID, text, ref)
		}
		return a.session.ChannelMessageSend(channelID, text)
	}

	// Send the initial cursor placeholder so the user sees an immediate response.
	placeholder, err := sendMsg(cursor)
	if err != nil {
		return fmt.Errorf("send placeholder: %w", err)
	}
	msgID := placeholder.ID // empty string signals "need a new message before next edit"

	var buf strings.Builder
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case token, ok := <-tokens:
			if !ok {
				// Stream ended.
				if msgID == "" {
					// Overflow happened but next message not created yet — send remaining content.
					if buf.Len() > 0 {
						if _, err := sendMsg(buf.String()); err != nil {
							slog.Warn("Discord: deferred overflow send failed", "error", err)
						}
					}
					return nil
				}
				return a.editFinal(channelID, msgID, buf.String(), ref)
			}
			buf.WriteString(token)

			if buf.Len() > overflowAt && msgID != "" {
				// Finalise current message, then clear msgID.
				// Do NOT create a new placeholder yet — ticker will create the next message
				// only when it has content, avoiding a second bare ▍.
				if err := a.editFinal(channelID, msgID, buf.String(), ref); err != nil {
					slog.Warn("Discord: overflow finalise error", "error", err)
				}
				msgID = ""
				buf.Reset()
			}

		case <-ticker.C:
			if buf.Len() == 0 {
				continue
			}
			if msgID == "" {
				// Post-overflow: create next message now that we have real content.
				newMsg, err := sendMsg(buf.String() + cursor)
				if err != nil {
					slog.Warn("Discord: overflow continuation send failed", "error", err)
				} else {
					msgID = newMsg.ID
				}
			} else {
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

// sendStreamingInteraction streams tokens into an ephemeral interaction response.
func (a *Adapter) sendStreamingInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, tokens <-chan string) error {
	interval := time.Duration(a.cfg.StreamingIntervalMs) * time.Millisecond

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

func (a *Adapter) registerSlashCommands() {
	guildID := a.cfg.SlashCommandGuildID
	if _, err := a.session.ApplicationCommandCreate(a.session.State.User.ID, guildID, askCommand); err != nil {
		slog.Error("Discord: slash command registration failed", "error", err)
	} else if guildID != "" {
		slog.Info("Discord: /ask registered (guild)", "guild_id", guildID)
	} else {
		slog.Info("Discord: /ask registered globally (propagation up to 1h)")
	}
}

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

func isRateLimited(err error) bool {
	var restErr *discordgo.RESTError
	return errors.As(err, &restErr) && restErr.Response != nil && restErr.Response.StatusCode == 429
}
