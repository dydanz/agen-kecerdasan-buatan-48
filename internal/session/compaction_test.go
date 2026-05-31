package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dydanz/akb48/internal/config"
)

// stubBrainWriter records calls for assertion.
type stubBrainWriter struct {
	available bool
	calls     int
	lastInput json.RawMessage
}

func (s *stubBrainWriter) CreateEntities(_ context.Context, in json.RawMessage) error {
	s.calls++
	s.lastInput = in
	return nil
}
func (s *stubBrainWriter) Available() bool { return s.available }

// stubExtractor returns a fixed entity set and summary.
func stubExtractor(_ context.Context, turns []SessionTurn) (json.RawMessage, string, error) {
	entities := json.RawMessage(`[{"name":"test-proj","entityType":"project","observations":["created"]}]`)
	return entities, "Test summary of " + string(rune('0'+len(turns))) + " turns.", nil
}

func makeCfg(maxCompact, maxContext int) config.SessionConfig {
	return config.SessionConfig{
		MaxTurnsBeforeCompaction: maxCompact,
		MaxTurnsInContext:        maxContext,
		StorageDir:               os.TempDir(),
	}
}

func makeTurns(n int) []SessionTurn {
	turns := make([]SessionTurn, n)
	for i := range turns {
		turns[i] = SessionTurn{
			TurnID:            "t" + string(rune('0'+i)),
			Timestamp:         time.Now().UTC(),
			UserMessage:       "msg",
			AssistantResponse: "resp",
		}
	}
	return turns
}

func TestCompactionHook_Disabled(t *testing.T) {
	cfg := makeCfg(0, 10) // disabled
	sess := NewSession("test-disabled")
	for _, turn := range makeTurns(20) {
		sess.AddTurn(turn)
	}
	mgr := NewSessionManager(cfg)
	brain := &stubBrainWriter{available: true}
	hook := CompactionHook(cfg, mgr, brain, stubExtractor)

	if err := hook(t.Context(), sess, SessionTurn{}); err != nil {
		t.Fatalf("hook error: %v", err)
	}
	if brain.calls > 0 {
		t.Error("brain should not be called when MaxTurnsBeforeCompaction=0")
	}
}

func TestCompactionHook_BelowThreshold(t *testing.T) {
	cfg := makeCfg(30, 10)
	sess := NewSession("test-below")
	for _, turn := range makeTurns(5) { // far below threshold
		sess.AddTurn(turn)
	}
	mgr := NewSessionManager(cfg)
	brain := &stubBrainWriter{available: true}
	hook := CompactionHook(cfg, mgr, brain, stubExtractor)

	if err := hook(t.Context(), sess, SessionTurn{}); err != nil {
		t.Fatalf("hook error: %v", err)
	}
	if brain.calls > 0 {
		t.Error("brain should not be called below threshold")
	}
}

func TestRunCompaction_Success(t *testing.T) {
	dir := t.TempDir()
	cfg := config.SessionConfig{
		MaxTurnsBeforeCompaction: 5,
		MaxTurnsInContext:        3,
		StorageDir:               dir,
		MaxFileSizeMB:            10,
	}
	mgr := NewSessionManager(cfg)
	sess := NewSession("test-compact")
	// Seed JSONL so appendCompactionRecord has a file to append to
	path := filepath.Join(dir, "test-compact.jsonl")
	os.WriteFile(path, []byte{}, 0o600) //nolint:errcheck

	for _, turn := range makeTurns(6) { // 6 turns, keep 3, compact 3
		sess.AddTurn(turn)
	}
	brain := &stubBrainWriter{available: true}

	runCompaction(t.Context(), sess, mgr, brain, stubExtractor, cfg)

	// Allow goroutine to finish (runCompaction called directly here — synchronous)
	time.Sleep(10 * time.Millisecond)

	// In-memory: 1 summary turn + 3 kept turns = 4
	sess.mu.RLock()
	count := len(sess.Turns)
	firstTurn := sess.Turns[0]
	sess.mu.RUnlock()

	if count != 4 {
		t.Errorf("expected 4 turns after compaction (1 summary + 3 kept), got %d", count)
	}
	if firstTurn.UserMessage != "[compacted]" {
		t.Errorf("expected first turn to be summary, got: %q", firstTurn.UserMessage)
	}
	if !strings.Contains(firstTurn.AssistantResponse, "summary") {
		t.Errorf("expected summary in AssistantResponse, got: %q", firstTurn.AssistantResponse)
	}

	// Brain should have been called
	if brain.calls != 1 {
		t.Errorf("expected brain.CreateEntities called once, got %d", brain.calls)
	}

	// JSONL record appended
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read JSONL: %v", err)
	}
	if !strings.Contains(string(data), `"type":"compaction"`) {
		t.Errorf("compaction record not found in JSONL:\n%s", string(data))
	}
}

func TestRunCompaction_BrainDown(t *testing.T) {
	dir := t.TempDir()
	cfg := config.SessionConfig{
		MaxTurnsBeforeCompaction: 5,
		MaxTurnsInContext:        3,
		StorageDir:               dir,
		MaxFileSizeMB:            10,
	}
	mgr := NewSessionManager(cfg)
	sess := NewSession("test-braindown")
	path := filepath.Join(dir, "test-braindown.jsonl")
	os.WriteFile(path, []byte{}, 0o600) //nolint:errcheck

	for _, turn := range makeTurns(6) {
		sess.AddTurn(turn)
	}
	brain := &stubBrainWriter{available: false} // brain down

	runCompaction(t.Context(), sess, mgr, brain, stubExtractor, cfg)
	time.Sleep(10 * time.Millisecond)

	// Session should still be compacted in-memory
	sess.mu.RLock()
	count := len(sess.Turns)
	sess.mu.RUnlock()
	if count != 4 {
		t.Errorf("expected 4 turns even with brain down, got %d", count)
	}

	// Brain must NOT have been called
	if brain.calls != 0 {
		t.Errorf("brain.CreateEntities must not be called when unavailable, got %d calls", brain.calls)
	}
}

func TestLoadFromDisk_WithCompactionRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "round-trip.jsonl")

	// Write: header + 5 turns + compaction record (replaces 3) + 2 new turns
	lines := []string{
		`{"event":"session_created","session_id":"round-trip","created_at":"2026-01-01T00:00:00Z"}`,
		`{"turn_id":"t0","timestamp":"2026-01-01T00:00:01Z","user_message":"a","assistant_response":"b"}`,
		`{"turn_id":"t1","timestamp":"2026-01-01T00:00:02Z","user_message":"c","assistant_response":"d"}`,
		`{"turn_id":"t2","timestamp":"2026-01-01T00:00:03Z","user_message":"e","assistant_response":"f"}`,
		`{"turn_id":"t3","timestamp":"2026-01-01T00:00:04Z","user_message":"g","assistant_response":"h"}`,
		`{"turn_id":"t4","timestamp":"2026-01-01T00:00:05Z","user_message":"i","assistant_response":"j"}`,
		`{"type":"compaction","ts":"2026-01-01T00:01:00Z","turns_replaced":3,"entity_count":2,"summary":"Historical context."}`,
		`{"turn_id":"t5","timestamp":"2026-01-01T00:02:00Z","user_message":"k","assistant_response":"l"}`,
		`{"turn_id":"t6","timestamp":"2026-01-01T00:02:01Z","user_message":"m","assistant_response":"n"}`,
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	sess, err := LoadFromDisk(path)
	if err != nil {
		t.Fatalf("LoadFromDisk: %v", err)
	}

	// Expected: [summary, t3, t4, t5, t6] — compaction replaced t0..t2, kept t3+t4, then t5+t6 appended
	if len(sess.Turns) != 5 {
		t.Errorf("expected 5 turns after reload (1 summary + 4 kept), got %d", len(sess.Turns))
		for i, tt := range sess.Turns {
			t.Logf("  [%d] turn_id=%s user=%s", i, tt.TurnID, tt.UserMessage)
		}
	}
	if sess.Turns[0].UserMessage != "[compacted]" {
		t.Errorf("expected first turn to be summary, got: %q", sess.Turns[0].UserMessage)
	}
	if sess.Turns[0].AssistantResponse != "Historical context." {
		t.Errorf("wrong summary text: %q", sess.Turns[0].AssistantResponse)
	}
	if sess.Turns[1].TurnID != "t3" {
		t.Errorf("expected t3 after summary, got %q", sess.Turns[1].TurnID)
	}
}

func TestParseEntitiesJSON(t *testing.T) {
	cases := []struct {
		input string
		valid bool
	}{
		{`[{"name":"x"}]`, true},
		{`some text [{"name":"x"}] more text`, true},
		{`no array here`, false},
		{`[invalid json`, false},
		{``, false},
	}
	for _, c := range cases {
		got := ParseEntitiesJSON(c.input)
		if c.valid && got == nil {
			t.Errorf("ParseEntitiesJSON(%q) = nil, want non-nil", c.input)
		}
		if !c.valid && got != nil {
			t.Errorf("ParseEntitiesJSON(%q) = %s, want nil", c.input, got)
		}
	}
}
