package session

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dydanz/akb48/internal/config"
	"github.com/dydanz/akb48/internal/types"
)

func testCfg(dir string) config.SessionConfig {
	return config.SessionConfig{
		StorageDir:                 dir,
		MaxTurnsInContext:          50,
		MaxContextTokens:           32000,
		MaxFileSizeMB:              10,
		ColdResumeThresholdMinutes: 30,
	}
}

func makeTurnN(id int) SessionTurn {
	return SessionTurn{
		TurnID:            string(rune('a' + id)),
		Timestamp:         time.Now().UTC(),
		UserMessage:       "msg",
		AssistantResponse: "resp",
		TokenUsage:        types.TokenUsage{InputTokens: 50, OutputTokens: 50},
		LatencyMs:         10,
	}
}

func TestSessionManager_ResolveOrCreate_New(t *testing.T) {
	dir := t.TempDir()
	m := NewSessionManager(testCfg(dir))

	sess, err := m.ResolveOrCreate(context.Background(), "main:cli:local")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess.SessionID != "main:cli:local" {
		t.Errorf("unexpected session ID: %q", sess.SessionID)
	}
	if sess.TurnCount() != 0 {
		t.Errorf("expected empty session")
	}

	// File should exist
	path := filepath.Join(dir, "main_cli_local.jsonl")
	if _, err := filepath.Glob(path); err != nil {
		t.Errorf("session file not created")
	}
}

func TestSessionManager_ResolveOrCreate_Concurrent(t *testing.T) {
	dir := t.TempDir()
	m := NewSessionManager(testCfg(dir))

	var wg sync.WaitGroup
	results := make([]*Session, 10)

	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sess, err := m.ResolveOrCreate(context.Background(), "main:cli:local")
			if err != nil {
				t.Errorf("goroutine %d error: %v", i, err)
				return
			}
			results[i] = sess
		}()
	}
	wg.Wait()

	// All pointers must point to same session
	for i, s := range results {
		if s == nil {
			t.Errorf("result[%d] is nil", i)
			continue
		}
		if s != results[0] {
			t.Errorf("result[%d] is different pointer — race condition", i)
		}
	}
}

func TestSessionManager_ResolveOrCreate_LoadsFromDisk(t *testing.T) {
	dir := t.TempDir()
	m1 := NewSessionManager(testCfg(dir))

	// Create and persist 2 turns
	sess, _ := m1.ResolveOrCreate(context.Background(), "main:cli:local")
	turn1 := makeTurnN(0)
	turn2 := makeTurnN(1)
	sess.AddTurn(turn1)
	sess.AddTurn(turn2)
	if err := m1.Persist(sess, turn1); err != nil {
		t.Fatal(err)
	}
	if err := m1.Persist(sess, turn2); err != nil {
		t.Fatal(err)
	}
	m1.Shutdown()

	// New manager loads from disk
	m2 := NewSessionManager(testCfg(dir))
	loaded, err := m2.ResolveOrCreate(context.Background(), "main:cli:local")
	if err != nil {
		t.Fatalf("load from disk: %v", err)
	}
	if loaded.TurnCount() != 2 {
		t.Errorf("expected 2 turns loaded from disk, got %d", loaded.TurnCount())
	}
}

func TestSessionManager_GetContextTurns_TurnLimit(t *testing.T) {
	dir := t.TempDir()
	cfg := testCfg(dir)
	cfg.MaxTurnsInContext = 50
	cfg.MaxContextTokens = 0 // disable token budget
	m := NewSessionManager(cfg)

	sess := NewSession("test")
	for i := range 60 {
		sess.AddTurn(makeTurnN(i % 26))
	}

	turns := m.GetContextTurns(sess)
	if len(turns) != 50 {
		t.Errorf("expected 50 turns (newest), got %d", len(turns))
	}
}

func TestSessionManager_GetContextTurns_TokenBudget(t *testing.T) {
	dir := t.TempDir()
	cfg := testCfg(dir)
	cfg.MaxTurnsInContext = 1000
	cfg.MaxContextTokens = 10 // very small budget — forces cutoff
	m := NewSessionManager(cfg)

	sess := NewSession("test")
	for i := range 20 {
		sess.AddTurn(makeTurnN(i % 26))
	}

	turns := m.GetContextTurns(sess)
	// With 10 token budget, should return far fewer than 20
	if len(turns) >= 20 {
		t.Errorf("expected token budget to cut turns, got all %d", len(turns))
	}
}

func TestSessionManager_LoadAll(t *testing.T) {
	dir := t.TempDir()
	m1 := NewSessionManager(testCfg(dir))

	// Create 2 sessions with turns
	for _, sid := range []string{"main:cli:local", "main:telegram:12345"} {
		sess, _ := m1.ResolveOrCreate(context.Background(), sid)
		turn := makeTurnN(0)
		sess.AddTurn(turn)
		m1.Persist(sess, turn)
	}
	m1.Shutdown()

	// New manager LoadAll
	m2 := NewSessionManager(testCfg(dir))
	if err := m2.LoadAll(context.Background()); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	count := 0
	m2.sessions.Range(func(_, _ any) bool { count++; return true })
	if count != 2 {
		t.Errorf("expected 2 sessions loaded, got %d", count)
	}
}

func TestSessionManager_Shutdown_ClosesHandles(t *testing.T) {
	dir := t.TempDir()
	m := NewSessionManager(testCfg(dir))

	sess, _ := m.ResolveOrCreate(context.Background(), "main:cli:local")
	turn := makeTurnN(0)
	sess.AddTurn(turn)
	m.Persist(sess, turn)

	if err := m.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// After shutdown, file should still be readable
	path := filepath.Join(dir, "main_cli_local.jsonl")
	loaded, err := LoadFromDisk(path)
	if err != nil {
		t.Fatalf("LoadFromDisk after shutdown: %v", err)
	}
	if loaded.TurnCount() != 1 {
		t.Errorf("expected 1 turn after shutdown, got %d", loaded.TurnCount())
	}
}

func TestSessionManager_IsCold(t *testing.T) {
	dir := t.TempDir()
	cfg := testCfg(dir)
	cfg.ColdResumeThresholdMinutes = 30
	m := NewSessionManager(cfg)

	sess := NewSession("test")
	if !m.IsCold(sess) {
		t.Error("new session should be cold")
	}

	sess.AddTurn(makeTurnN(0))
	if m.IsCold(sess) {
		t.Error("session with recent turn should not be cold")
	}
}
