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
