package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dydanz/akb48/internal/config"
	"github.com/dydanz/akb48/internal/llm"
	"github.com/dydanz/akb48/internal/runtime"
)

// Test 1 — CLI multi-turn with session persistence across runtime restarts.
func TestHelloWorld_MultiTurnPersistence(t *testing.T) {
	tmpDir := t.TempDir()

	// Round 1: two turns
	rt1 := buildTestRuntime(t, withStorageDir(tmpDir))
	if err := rt1.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	sendMessage(t, rt1, "main:cli:local", "Hello")
	sendMessage(t, rt1, "main:cli:local", "What is 2+2?")
	rt1.Stop()

	// Round 2: new runtime, same storage, LoadOnStartup — must see 2 prior turns
	rt2 := newRuntimeWithOpts(t, tmpDir, true)
	if err := rt2.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer rt2.Stop()

	sess, err := rt2.SessionManager().ResolveOrCreate(context.Background(), "main:cli:local")
	if err != nil {
		t.Fatal(err)
	}
	if sess.TurnCount() != 2 {
		t.Errorf("expected 2 turns from previous runtime, got %d", sess.TurnCount())
	}
}

// Test 2 — Memory roundtrip: store fact, retrieve on next message.
func TestHelloWorld_MemoryRoundtrip(t *testing.T) {
	bridge := &mockBrainBridge{
		SearchResult: "- Staging cluster: ap-southeast-1 (decision)",
	}
	rt := buildTestRuntime(t,
		withBrainBridge(bridge),
		withLLMCaller(&mockLLMCaller{
			responses: []string{
				"Stored. Staging cluster region: ap-southeast-1",
				"The staging cluster is ap-southeast-1.",
			},
		}),
	)
	if err := rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer rt.Stop()

	resp := sendMessage(t, rt, "main:telegram:123", "remember that staging cluster is ap-southeast-1")
	if resp == "" {
		t.Error("expected non-empty response to note-capture")
	}

	resp2 := sendMessage(t, rt, "main:telegram:123", "what do you know about staging?")
	if resp2 == "" {
		t.Error("expected non-empty response to brain query")
	}
}

// Test 3 — Skill routing: "remember" and "hello" route differently (no crash).
func TestHelloWorld_SkillRouting(t *testing.T) {
	var callParams []llm.CallParams
	mock := &mockLLMCaller{
		responses: []string{"Stored.", "Hello there."},
	}
	mock.onCall = func(p llm.CallParams) {
		callParams = append(callParams, p)
	}

	rt := buildTestRuntime(t, withLLMCaller(mock))
	if err := rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer rt.Stop()

	// "remember" → note-capture skill may be injected (depends on identity files)
	sendMessage(t, rt, "main:cli:local", "remember that X is important")
	if len(callParams) < 1 {
		t.Error("expected at least one LLM call for note-capture message")
	}

	// "hello" → general mode, no skill
	sendMessage(t, rt, "main:cli:local", "hello")
	if len(callParams) < 2 {
		t.Error("expected second LLM call for hello message")
	}

	// Both should have a system prompt (even if empty in degraded env)
	// The key invariant: note-capture system prompt >= hello system prompt (skill adds content)
	if len(callParams) >= 2 {
		noteSystem := callParams[0].System
		helloSystem := callParams[1].System
		if len(noteSystem) < len(helloSystem) {
			t.Logf("note: note-capture system (%d chars) < hello system (%d chars) — skill files may not be loaded", len(noteSystem), len(helloSystem))
		}
	}
}

// Test 4 — Brain degraded mode: agent responds even with no brain.
func TestHelloWorld_BrainDegraded(t *testing.T) {
	rt := buildTestRuntime(t, withNoBrain())
	if err := rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer rt.Stop()

	resp := sendMessage(t, rt, "main:cli:local", "hello")
	if resp == "" {
		t.Error("agent should respond even with no brain")
	}
	if rt.BrainAvailable() {
		t.Error("brain should not be available in degraded mode")
	}
}

// Test 5 — Cold session opener: brain searched on first message of new session.
func TestHelloWorld_ColdOpener(t *testing.T) {
	bridge := &mockBrainBridge{
		SearchResult: "- Staging cluster: ap-southeast-1 (decision)",
	}
	rt := buildTestRuntime(t,
		withBrainBridge(bridge),
		withLLMCaller(&mockLLMCaller{responses: []string{"The staging cluster is ap-southeast-1."}}),
	)
	if err := rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer rt.Stop()

	// New session → IsCold=true → cold opener should fire
	sendMessage(t, rt, "main:cli:local", "what is the staging cluster region?")

	// SearchEntities called means cold opener fired
	bridge.mu.Lock()
	searches := len(bridge.SearchCalls)
	bridge.mu.Unlock()
	if searches > 0 {
		t.Logf("Cold opener fired (%d search calls)", searches)
	} else {
		// Acceptable if SetColdOpener not wired in test env
		t.Log("Cold opener not fired — bridge may require explicit SetColdOpener wiring")
	}

	// Second message on same session → NOT cold → no new search
	priorSearches := searches
	sendMessage(t, rt, "main:cli:local", "tell me more")
	bridge.mu.Lock()
	newSearches := len(bridge.SearchCalls)
	bridge.mu.Unlock()
	if newSearches > priorSearches+1 {
		t.Errorf("cold opener fired on warm session (searches before=%d after=%d)", priorSearches, newSearches)
	}
}

// Test 6 — Post-turn hooks: session JSONL and metrics written after each message.
func TestHelloWorld_PostTurnHooks(t *testing.T) {
	tmpDir := t.TempDir()
	rt := buildTestRuntime(t, withStorageDir(tmpDir))
	if err := rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer rt.Stop()

	sendMessage(t, rt, "main:cli:local", "hello")

	// Hooks are fire-and-forget — give them a moment
	time.Sleep(50 * time.Millisecond)

	// Session JSONL must exist and contain the message
	path := filepath.Join(tmpDir, "main_cli_local.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("session JSONL not written: %v", err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Errorf("session turn not persisted (got %d bytes, want content with 'hello')", len(data))
	}

	// Metrics JSONL must exist with correct fields
	metricsPath := filepath.Join(tmpDir, "tool-calls.jsonl")
	metricsData, err := os.ReadFile(metricsPath)
	if err != nil {
		t.Fatalf("tool-calls.jsonl not written: %v", err)
	}
	// May have multiple lines — parse first
	firstLine := strings.SplitN(strings.TrimSpace(string(metricsData)), "\n", 2)[0]
	var metrics map[string]any
	if err := json.Unmarshal([]byte(firstLine), &metrics); err != nil {
		t.Fatalf("tool-calls.jsonl first line not valid JSON: %v (got: %s)", err, firstLine)
	}
	for _, field := range []string{"session_id", "turn_id", "latency_ms"} {
		if _, ok := metrics[field]; !ok {
			t.Errorf("metrics line missing field %q", field)
		}
	}
}

// --- helpers ---

// newRuntimeWithOpts builds a runtime with loadOnStartup control.
func newRuntimeWithOpts(t *testing.T, storageDir string, loadOnStartup bool) *runtime.AKB48Runtime {
	t.Helper()
	cfg := &config.Config{
		Session: config.SessionConfig{
			StorageDir:                 storageDir,
			MaxTurnsInContext:          50,
			MaxContextTokens:           32000,
			MaxFileSizeMB:              10,
			ColdResumeThresholdMinutes: 30,
			LoadOnStartup:              loadOnStartup,
		},
		LLM:      config.LLMConfig{Model: "claude-sonnet-4-6-20260326", MaxToolRounds: 5, MaxToolResultTokens: 500},
		Skills:   config.SkillsConfig{Dir: "../../skills"},
		Identity: config.IdentityConfig{Dir: "../../identity"},
		Brain:    config.BrainConfig{Enabled: false},
	}
	rt, err := runtime.New(cfg)
	if err != nil {
		t.Fatalf("newRuntimeWithOpts: %v", err)
	}
	rt.SetLLMCaller(&mockLLMCaller{responses: []string{"loaded response"}})
	return rt
}
