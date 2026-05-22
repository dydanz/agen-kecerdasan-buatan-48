package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/dydanz/akb48/internal/config"
	"github.com/dydanz/akb48/internal/runtime"
	"github.com/dydanz/akb48/internal/types"
)

const (
	cursor      = "▍"
	maxMsgLen   = 4096
	overflowAt  = 3800
	maxInterval = 3 * time.Second
)

// Adapter handles Telegram long-polling and per-message streaming.
type Adapter struct {
	bot     *tgbotapi.BotAPI
	cfg     config.TelegramConfig
	handler runtime.MessageHandler
}

// New creates a Telegram adapter. Returns error if bot token is invalid.
func New(cfg config.TelegramConfig, handler runtime.MessageHandler) (*Adapter, error) {
	token := os.Getenv(cfg.TokenEnv)
	if token == "" {
		return nil, fmt.Errorf("env var %q not set — telegram bot token required", cfg.TokenEnv)
	}
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("telegram bot init: %w", err)
	}
	return &Adapter{bot: bot, cfg: cfg, handler: handler}, nil
}

// Start runs the long-polling loop until ctx is cancelled.
func (a *Adapter) Start(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := a.bot.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			a.bot.StopReceivingUpdates()
			return nil
		case update, ok := <-updates:
			if !ok {
				return nil
			}
			if update.Message == nil {
				continue
			}
			go a.onMessage(ctx, update)
		}
	}
}

func (a *Adapter) onMessage(ctx context.Context, update tgbotapi.Update) {
	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID

	if !a.isAllowed(userID) {
		slog.Debug("Telegram: message from non-allowed user dropped", "user_id", userID)
		return
	}

	a.bot.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping))

	msg := types.Message{
		SessionID: fmt.Sprintf("main:telegram:%d", userID),
		Text:      update.Message.Text,
		UserID:    fmt.Sprintf("%d", userID),
		Timestamp: time.Now(),
	}

	tokens := make(chan string, 64)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := a.sendStreaming(ctx, chatID, tokens); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("Telegram streaming error", "error", err)
		}
	}()

	if err := a.handler(ctx, msg, tokens); err != nil {
		slog.Error("Telegram handler error", "error", err)
		close(tokens)
		wg.Wait()
		a.send(chatID, fmt.Sprintf("Error: %v", err))
		return
	}
	close(tokens)
	wg.Wait()
}

// send sends a plain (non-streaming) message, splitting if needed.
func (a *Adapter) send(chatID int64, text string) {
	for _, part := range splitMessage(text, maxMsgLen) {
		if _, err := a.bot.Send(tgbotapi.NewMessage(chatID, part)); err != nil {
			slog.Warn("Telegram send error", "error", err)
		}
	}
}

// sendStreaming collects tokens and edits the placeholder message progressively.
func (a *Adapter) sendStreaming(ctx context.Context, chatID int64, tokens <-chan string) error {
	interval := time.Duration(a.cfg.StreamingIntervalMs) * time.Millisecond

	placeholder, err := a.bot.Send(tgbotapi.NewMessage(chatID, cursor))
	if err != nil {
		return fmt.Errorf("send placeholder: %w", err)
	}
	msgID := placeholder.MessageID

	var buf strings.Builder
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case token, ok := <-tokens:
			if !ok {
				return a.editFinal(chatID, msgID, buf.String())
			}
			buf.WriteString(token)

			if buf.Len() > overflowAt {
				if err := a.editFinal(chatID, msgID, buf.String()); err != nil {
					slog.Warn("Telegram: error finalizing overflow message", "error", err)
				}
				newMsg, err := a.bot.Send(tgbotapi.NewMessage(chatID, cursor))
				if err != nil {
					return fmt.Errorf("send overflow message: %w", err)
				}
				msgID = newMsg.MessageID
				buf.Reset()
			}

		case <-ticker.C:
			if buf.Len() > 0 {
				edit := tgbotapi.NewEditMessageText(chatID, msgID, buf.String()+cursor)
				if _, err := a.bot.Send(edit); err != nil {
					if isFloodControl(err) {
						interval = min(interval*2, maxInterval)
						ticker.Reset(interval)
						slog.Warn("Telegram flood control — backing off",
							"new_interval_ms", interval.Milliseconds())
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

func (a *Adapter) editFinal(chatID int64, msgID int, text string) error {
	if text == "" {
		text = "(empty response)"
	}
	parts := splitMessage(text, maxMsgLen)

	edit := tgbotapi.NewEditMessageText(chatID, msgID, parts[0])
	if _, err := a.bot.Send(edit); err != nil {
		slog.Warn("Telegram: final edit failed", "error", err)
	}
	for _, part := range parts[1:] {
		if _, err := a.bot.Send(tgbotapi.NewMessage(chatID, part)); err != nil {
			slog.Warn("Telegram: overflow send failed", "error", err)
		}
	}
	return nil
}

func (a *Adapter) isAllowed(userID int64) bool {
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

func isFloodControl(err error) bool {
	var tgErr *tgbotapi.Error
	return errors.As(err, &tgErr) && tgErr.Code == 429
}
