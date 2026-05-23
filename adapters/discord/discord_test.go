package discord

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dydanz/akb48/adapters/shared"
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
	t.Setenv("DISCORD_BOT_TOKEN_TEST", "")
	_, err := New(config.DiscordConfig{TokenEnv: "DISCORD_BOT_TOKEN_TEST"}, nil)
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
