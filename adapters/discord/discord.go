package discord

import (
	"context"
	"errors"
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
	session *discordgo.Session
	cfg     config.DiscordConfig
	handler runtime.MessageHandler
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

// onMessage handles incoming DMs from allowed users.
func (a *Adapter) onMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot {
		return
	}
	if !a.isAllowed(m.Author.ID) {
		slog.Debug("Discord: message from non-allowed user dropped", "user_id", m.Author.ID)
		return
	}
	ch, err := s.Channel(m.ChannelID)
	if err != nil || ch.Type != discordgo.ChannelTypeDM {
		return
	}
	go a.process(m.ChannelID, m.Author.ID, m.Content)
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

// sendStreaming sends tokens as progressive edits to a placeholder DM message.
func (a *Adapter) sendStreaming(channelID string, tokens <-chan string) error {
	interval := time.Duration(a.cfg.StreamingIntervalMs) * time.Millisecond

	placeholder, err := a.session.ChannelMessageSend(channelID, cursor)
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
