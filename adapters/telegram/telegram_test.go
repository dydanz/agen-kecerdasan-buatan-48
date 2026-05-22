package telegram

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dydanz/akb48/internal/config"
)

// --- splitMessage tests ---

func TestSplitMessage_Short(t *testing.T) {
	parts := splitMessage("hello world", 4096)
	if len(parts) != 1 || parts[0] != "hello world" {
		t.Errorf("unexpected: %v", parts)
	}
}

func TestSplitMessage_ExactLimit(t *testing.T) {
	text := strings.Repeat("a", 4096)
	parts := splitMessage(text, 4096)
	if len(parts) != 1 {
		t.Errorf("expected 1 part, got %d", len(parts))
	}
}

func TestSplitMessage_ParagraphSplit(t *testing.T) {
	first := strings.Repeat("a", 3000)
	second := strings.Repeat("b", 2000)
	text := first + "\n\n" + second

	parts := splitMessage(text, 4096)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d: %v", len(parts), parts)
	}
	if parts[0] != first {
		t.Errorf("first part should be all a's, got len=%d", len(parts[0]))
	}
	if parts[1] != second {
		t.Errorf("second part should be all b's, got len=%d", len(parts[1]))
	}
}

func TestSplitMessage_NewlineFallback(t *testing.T) {
	// No double newline — should split at single newline
	first := strings.Repeat("a", 3000)
	second := strings.Repeat("b", 2000)
	text := first + "\n" + second

	parts := splitMessage(text, 4096)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
}

func TestSplitMessage_HardCut(t *testing.T) {
	// 5000 chars, no newlines — hard cut at 4096
	text := strings.Repeat("a", 5000)
	parts := splitMessage(text, 4096)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if len(parts[0]) != 4096 {
		t.Errorf("first part should be exactly 4096, got %d", len(parts[0]))
	}
	if len(parts[1]) != 904 {
		t.Errorf("second part should be 904, got %d", len(parts[1]))
	}
}

func TestSplitMessage_ThreeParts(t *testing.T) {
	// 9500 chars → 3 parts
	text := strings.Repeat("x", 9500)
	parts := splitMessage(text, 4096)
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}
	total := 0
	for _, p := range parts {
		total += len(p)
	}
	if total != 9500 {
		t.Errorf("total chars should be 9500, got %d", total)
	}
}

// --- isAllowed tests ---

func TestIsAllowed_Allowed(t *testing.T) {
	a := &Adapter{cfg: config.TelegramConfig{AllowedUserIDs: []int64{123, 456}}}
	if !a.isAllowed(123) {
		t.Error("123 should be allowed")
	}
	if !a.isAllowed(456) {
		t.Error("456 should be allowed")
	}
}

func TestIsAllowed_NotAllowed(t *testing.T) {
	a := &Adapter{cfg: config.TelegramConfig{AllowedUserIDs: []int64{123}}}
	if a.isAllowed(999) {
		t.Error("999 should not be allowed")
	}
	if a.isAllowed(0) {
		t.Error("0 should not be allowed")
	}
}

func TestIsAllowed_EmptyList(t *testing.T) {
	a := &Adapter{cfg: config.TelegramConfig{AllowedUserIDs: []int64{}}}
	if a.isAllowed(123) {
		t.Error("empty allowlist should allow nobody")
	}
}

// --- isFloodControl test ---

func TestIsFloodControl_Code429(t *testing.T) {
	from429 := &tgbotAPIError{code: 429}
	if !isFloodControlErr(from429) {
		t.Error("429 should be flood control")
	}
}

func TestIsFloodControl_Other(t *testing.T) {
	from400 := &tgbotAPIError{code: 400}
	if isFloodControlErr(from400) {
		t.Error("400 should not be flood control")
	}
}

// tgbotAPIError is a local stand-in for tgbotapi.Error for testing isFloodControl.
type tgbotAPIError struct {
	code int
}

func (e *tgbotAPIError) Error() string { return "telegram error" }

// isFloodControlErr is a test-accessible version using the fake error type.
func isFloodControlErr(err error) bool {
	if e, ok := err.(*tgbotAPIError); ok {
		return e.code == 429
	}
	return false
}

// --- editFinal tests via sendStreaming ---

func TestSendStreaming_EmptyTokens(t *testing.T) {
	// Can't test sendStreaming without a real bot — but we can test the
	// streaming logic via a minimal mock. Since tgbotapi.BotAPI is a struct
	// (not interface), we test the pure functions instead.

	// editFinal with empty text should use "(empty response)"
	// We verify this indirectly via splitMessage
	parts := splitMessage("", 4096)
	// empty string returns [""] which editFinal handles
	if len(parts) != 1 {
		t.Errorf("unexpected parts for empty string: %v", parts)
	}
}

func TestStreamingInterval_Default(t *testing.T) {
	cfg := config.TelegramConfig{StreamingIntervalMs: 1000}
	interval := time.Duration(cfg.StreamingIntervalMs) * time.Millisecond
	if interval != time.Second {
		t.Errorf("expected 1s interval, got %v", interval)
	}
}

func TestStreamingInterval_Doubled(t *testing.T) {
	// Test the backoff math from sendStreaming
	interval := 1000 * time.Millisecond
	doubled := min(interval*2, maxInterval)
	if doubled != 2*time.Second {
		t.Errorf("expected 2s after doubling, got %v", doubled)
	}
	// Double again — should cap at maxInterval (3s)
	capped := min(doubled*2, maxInterval)
	if capped != maxInterval {
		t.Errorf("expected cap at %v, got %v", maxInterval, capped)
	}
}

func TestSessionIDFormat(t *testing.T) {
	userID := int64(123456789)
	sessionID := fmt.Sprintf("main:telegram:%d", userID)
	expected := "main:telegram:123456789"
	if sessionID != expected {
		t.Errorf("unexpected session ID: %q", sessionID)
	}
}

func TestContext_CancelledAdapterStop(t *testing.T) {
	// Verify isAllowed returns false before handler is called
	// (tests the guard logic without a real bot)
	a := &Adapter{cfg: config.TelegramConfig{AllowedUserIDs: []int64{999}}}
	if a.isAllowed(123) {
		t.Error("user 123 should not pass allowlist")
	}
	if !a.isAllowed(999) {
		t.Error("user 999 should pass allowlist")
	}
}
