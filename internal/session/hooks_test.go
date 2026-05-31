package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dydanz/akb48/internal/config"
	"github.com/dydanz/akb48/internal/types"
)

func makeTestTurn() SessionTurn {
	return SessionTurn{
		TurnID:            "turn-test-1",
		Timestamp:         time.Now().UTC(),
		UserMessage:       "hello",
		AssistantResponse: "world",
		TokenUsage:        types.TokenUsage{InputTokens: 10, OutputTokens: 20},
		SkillUsed:         "note-capture",
		LatencyMs:         42,
	}
}

func TestRunHooks_AllHooksRun(t *testing.T) {
	var called [2]bool
	hooks := []HookFunc{
		func(_ context.Context, _ *Session, _ SessionTurn) error {
			called[0] = true
			return nil
		},
		func(_ context.Context, _ *Session, _ SessionTurn) error {
			called[1] = true
			return nil
		},
	}

	sess := NewSession("test")
	RunHooks(hooks, sess, makeTestTurn())

	if !called[0] || !called[1] {
		t.Error("not all hooks called")
	}
}

func TestRunHooks_FailingHookDoesNotBlock(t *testing.T) {
	var okCalled bool
	hooks := []HookFunc{
		func(_ context.Context, _ *Session, _ SessionTurn) error {
			return errors.New("hook error")
		},
		func(_ context.Context, _ *Session, _ SessionTurn) error {
			okCalled = true
			return nil
		},
	}

	sess := NewSession("test")
	RunHooks(hooks, sess, makeTestTurn())

	if !okCalled {
		t.Error("second hook should run despite first hook error")
	}
}

func TestRunHooks_PanicRecovered(t *testing.T) {
	var afterPanic bool
	hooks := []HookFunc{
		func(_ context.Context, _ *Session, _ SessionTurn) error {
			panic("hook panic")
		},
		func(_ context.Context, _ *Session, _ SessionTurn) error {
			afterPanic = true
			return nil
		},
	}

	// Should not panic out
	sess := NewSession("test")
	RunHooks(hooks, sess, makeTestTurn())

	if !afterPanic {
		t.Error("second hook should run after first hook panics")
	}
}

func TestRunHooks_SlowHookTimesOut(t *testing.T) {
	hooks := []HookFunc{
		func(ctx context.Context, _ *Session, _ SessionTurn) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}

	sess := NewSession("test")
	start := time.Now()
	RunHooks(hooks, sess, makeTestTurn())
	elapsed := time.Since(start)

	if elapsed > 6*time.Second {
		t.Errorf("RunHooks took too long: %v (expected < 6s)", elapsed)
	}
}

func TestPersistSessionHook(t *testing.T) {
	dir := t.TempDir()
	cfg := config.SessionConfig{
		StorageDir:                 dir,
		MaxTurnsInContext:          50,
		MaxContextTokens:           32000,
		MaxFileSizeMB:              10,
		ColdResumeThresholdMinutes: 30,
	}
	m := NewSessionManager(cfg)
	sess, _ := m.ResolveOrCreate(context.Background(), "main:cli:local")

	hook := PersistSessionHook(m)
	turn := makeTestTurn()
	if err := hook(context.Background(), sess, turn); err != nil {
		t.Fatalf("PersistSessionHook: %v", err)
	}

	path := filepath.Join(dir, "main_cli_local.jsonl")
	loaded, err := LoadFromDisk(path)
	if err != nil {
		t.Fatalf("LoadFromDisk: %v", err)
	}
	if loaded.TurnCount() != 1 {
		t.Errorf("expected 1 turn in JSONL, got %d", loaded.TurnCount())
	}
	if loaded.Turns[0].TurnID != "turn-test-1" {
		t.Errorf("unexpected turn ID: %q", loaded.Turns[0].TurnID)
	}
}

func TestLogMetricsHook(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "tool-calls.jsonl")

	hook := LogMetricsHook(logPath)
	sess := NewSession("main:cli:local")
	turn := makeTestTurn()
	turn.ToolCalls = []ToolCallRecord{{Name: "gbrain_search"}}

	if err := hook(context.Background(), sess, turn); err != nil {
		t.Fatalf("LogMetricsHook: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read metrics file: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal metrics line: %v", err)
	}

	if m["session_id"] != "main:cli:local" {
		t.Errorf("unexpected session_id: %v", m["session_id"])
	}
	if m["turn_id"] != "turn-test-1" {
		t.Errorf("unexpected turn_id: %v", m["turn_id"])
	}
	if m["skill_used"] != "note-capture" {
		t.Errorf("unexpected skill_used: %v", m["skill_used"])
	}
	tools, _ := m["tool_names"].([]any)
	if len(tools) != 1 || tools[0] != "gbrain_search" {
		t.Errorf("unexpected tool_names: %v", tools)
	}
}

func TestRunHooks_NoHooks(t *testing.T) {
	// Should not panic with empty slice
	RunHooks(nil, NewSession("test"), makeTestTurn())
	RunHooks([]HookFunc{}, NewSession("test"), makeTestTurn())
}

func TestNarrationGuardHook_Trips(t *testing.T) {
	// Action verb + zero tool events → WARN (we just check it doesn't error)
	hook := NarrationGuardHook()
	sess := NewSession("test-guard")
	turn := SessionTurn{
		TurnID:            "t1",
		AssistantResponse: "Let me start fetching files from the repo now.",
		ToolEventCount:    0,
	}
	if err := hook(context.Background(), sess, turn); err != nil {
		t.Errorf("NarrationGuardHook must not return error, got: %v", err)
	}
}

func TestNarrationGuardHook_NoTrip_ToolsExecuted(t *testing.T) {
	hook := NarrationGuardHook()
	sess := NewSession("test-guard-tools")
	turn := SessionTurn{
		TurnID:            "t2",
		AssistantResponse: "I am fetching the repo contents.",
		ToolEventCount:    2, // tools ran → no guard trip
	}
	if err := hook(context.Background(), sess, turn); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNarrationGuardHook_NoTrip_NoVerbs(t *testing.T) {
	hook := NarrationGuardHook()
	sess := NewSession("test-guard-noverbs")
	turn := SessionTurn{
		TurnID:            "t3",
		AssistantResponse: "The answer is 42.",
		ToolEventCount:    0,
	}
	if err := hook(context.Background(), sess, turn); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNarrationGuardHook_TurnStillPersists(t *testing.T) {
	// Guardrail must never prevent turn persistence
	dir := t.TempDir()
	cfg := config.SessionConfig{StorageDir: dir, MaxTurnsInContext: 50, MaxFileSizeMB: 10}
	mgr := NewSessionManager(cfg)
	sess, _ := mgr.ResolveOrCreate(context.Background(), "guard:test")

	persistHook := PersistSessionHook(mgr)
	guardHook := NarrationGuardHook()

	turn := SessionTurn{
		TurnID:            "guard-turn",
		Timestamp:         time.Now().UTC(),
		UserMessage:       "read the repo",
		AssistantResponse: "I am fetching all files in parallel.",
		ToolEventCount:    0,
	}

	// Both hooks run; neither should error
	if err := persistHook(context.Background(), sess, turn); err != nil {
		t.Fatalf("persist hook: %v", err)
	}
	if err := guardHook(context.Background(), sess, turn); err != nil {
		t.Fatalf("guard hook: %v", err)
	}

	// Turn was persisted regardless of guard
	path := filepath.Join(dir, "guard_test.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read JSONL: %v", err)
	}
	if len(data) == 0 {
		t.Error("JSONL file is empty — turn was not persisted")
	}
}
