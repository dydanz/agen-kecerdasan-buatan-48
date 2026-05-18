package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dydanz/akb48/internal/types"
)

func makeTurn(id, userMsg, assistantMsg string) SessionTurn {
	return SessionTurn{
		TurnID:            id,
		Timestamp:         time.Now().UTC(),
		UserMessage:       userMsg,
		AssistantResponse: assistantMsg,
		TokenUsage:        types.TokenUsage{InputTokens: 10, OutputTokens: 20},
		LatencyMs:         100,
	}
}

func TestSanitizeID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"main:cli:local", "main_cli_local"},
		{"main:telegram:123456789", "main_telegram_123456789"},
		{"no-colons", "no-colons"},
	}
	for _, c := range cases {
		got := SanitizeID(c.in)
		if got != c.want {
			t.Errorf("SanitizeID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNewSession(t *testing.T) {
	s := NewSession("main:cli:local")
	if s.SessionID != "main:cli:local" {
		t.Errorf("unexpected session ID: %q", s.SessionID)
	}
	if s.TurnCount() != 0 {
		t.Errorf("new session should have 0 turns")
	}
}

func TestAddTurn(t *testing.T) {
	s := NewSession("test")
	s.AddTurn(makeTurn("t1", "hello", "world"))
	s.AddTurn(makeTurn("t2", "ping", "pong"))
	if s.TurnCount() != 2 {
		t.Errorf("expected 2 turns, got %d", s.TurnCount())
	}
}

func TestIsCold(t *testing.T) {
	threshold := 30 * time.Minute

	// New session always cold
	s := NewSession("test")
	if !s.IsCold(threshold) {
		t.Error("new session should be cold")
	}

	// After adding turn: not cold
	s.AddTurn(makeTurn("t1", "hello", "world"))
	if s.IsCold(threshold) {
		t.Error("session with recent turn should not be cold")
	}
}

func TestPersistAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main_cli_local.jsonl")

	s := NewSession("main:cli:local")
	if err := PersistCreated(path, s); err != nil {
		t.Fatalf("PersistCreated: %v", err)
	}

	turns := []SessionTurn{
		makeTurn("t1", "hello", "world"),
		makeTurn("t2", "what is 2+2?", "4"),
		makeTurn("t3", "remember staging=ap-southeast-1", "Stored."),
	}

	for _, turn := range turns {
		if err := Persist(path, turn); err != nil {
			t.Fatalf("Persist: %v", err)
		}
	}

	loaded, err := LoadFromDisk(path)
	if err != nil {
		t.Fatalf("LoadFromDisk: %v", err)
	}

	if loaded.SessionID != "main:cli:local" {
		t.Errorf("unexpected session ID: %q", loaded.SessionID)
	}
	if loaded.TurnCount() != 3 {
		t.Errorf("expected 3 turns, got %d", loaded.TurnCount())
	}
	if loaded.Turns[0].TurnID != "t1" {
		t.Errorf("unexpected first turn ID: %q", loaded.Turns[0].TurnID)
	}
	if loaded.Turns[2].UserMessage != "remember staging=ap-southeast-1" {
		t.Errorf("unexpected last user message: %q", loaded.Turns[2].UserMessage)
	}
}

func TestLoadFromDisk_CorruptLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.jsonl")

	// Write: valid turn, corrupt line, valid turn
	turn1, _ := json.Marshal(makeTurn("t1", "hello", "world"))
	turn3, _ := json.Marshal(makeTurn("t3", "ping", "pong"))

	content := strings.Join([]string{
		string(turn1),
		"not valid json {{{",
		string(turn3),
	}, "\n") + "\n"

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadFromDisk(path)
	if err != nil {
		t.Fatalf("LoadFromDisk: %v", err)
	}

	// Corrupt line skipped, 2 valid turns loaded
	if loaded.TurnCount() != 2 {
		t.Errorf("expected 2 turns (corrupt line skipped), got %d", loaded.TurnCount())
	}
	if loaded.Turns[0].TurnID != "t1" {
		t.Errorf("expected first turn ID t1, got %q", loaded.Turns[0].TurnID)
	}
	if loaded.Turns[1].TurnID != "t3" {
		t.Errorf("expected second turn ID t3, got %q", loaded.Turns[1].TurnID)
	}
}

func TestLoadFromDisk_FileNotFound(t *testing.T) {
	_, err := LoadFromDisk("/tmp/does-not-exist-akb48-test.jsonl")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestFilePath_SanitizedSessionID(t *testing.T) {
	dir := t.TempDir()
	sessionID := "main:cli:local"
	sanitized := SanitizeID(sessionID)
	path := filepath.Join(dir, sanitized+".jsonl")

	s := NewSession(sessionID)
	if err := PersistCreated(path, s); err != nil {
		t.Fatal(err)
	}

	if !strings.HasSuffix(path, "main_cli_local.jsonl") {
		t.Errorf("path should end with main_cli_local.jsonl, got %q", path)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("file should exist at sanitized path: %v", err)
	}
}
