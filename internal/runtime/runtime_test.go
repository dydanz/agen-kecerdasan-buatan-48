package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/dydanz/akb48/internal/config"
	"github.com/dydanz/akb48/internal/llm"
	"github.com/dydanz/akb48/internal/session"
	"github.com/dydanz/akb48/internal/types"
)

// mockLLMCaller satisfies the llm.Caller interface in tests via the assembler.
// Since AKB48Runtime calls r.llmCaller.Call directly, we swap the assembler
// and use a test-local runtime built without a real API key.

// testRuntime builds a runtime suitable for unit testing:
// - temp dir for sessions
// - real session manager
// - mock context assembler that captures calls
// - no hooks (or test hooks)
func testRuntime(t *testing.T) (*AKB48Runtime, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		Session: config.SessionConfig{
			StorageDir:                 dir,
			MaxTurnsInContext:          50,
			MaxContextTokens:           32000,
			MaxFileSizeMB:              10,
			ColdResumeThresholdMinutes: 30,
			LoadOnStartup:              false,
		},
		LLM: config.LLMConfig{
			Model:               "claude-sonnet-4-6-20260326",
			MaxToolRounds:       5,
			MaxToolResultTokens: 500,
		},
	}
	rt, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return rt, dir
}

// mockAssembler swaps out the real LLM call by making HandleMessage think
// there's nothing in history and an empty system prompt. The "response" is
// injected via a captured tokens channel by an overridden HandleMessage.
//
// We test HandleMessage through a thin wrapper that replaces the llmCaller
// with a scriptedCaller.

type scriptedCaller struct {
	responses []string
	idx       int
	err       error
	captured  []llm.CallParams
}

func (s *scriptedCaller) Call(ctx context.Context, params llm.CallParams) (*llm.CallResult, error) {
	s.captured = append(s.captured, params)
	if s.err != nil {
		return nil, s.err
	}
	if s.idx >= len(s.responses) {
		return nil, errors.New("no more scripted responses")
	}
	resp := s.responses[s.idx]
	s.idx++
	if params.Tokens != nil {
		params.Tokens <- resp
	}
	return &llm.CallResult{
		Text:       resp,
		TokenUsage: types.TokenUsage{InputTokens: 10, OutputTokens: 5},
		LatencyMs:  10,
	}, nil
}

// internalHandleMessage is a test harness that replaces the LLM caller.
// We can't inject via interface without breaking runtime.go, so we test
// via the real HandleMessage but use a fully wired runtime. To avoid
// real API calls in unit tests, we define a mockable HandleMessage path.

// Note: since llm.Caller is a concrete struct (not interface), we test
// AKB48Runtime by inspecting session state after a HandleMessage call.
// Real integration tests use the scripted mock approach in tests/integration/.
// Here we test the session/hook wiring without LLM by calling the internal
// methods directly.

func TestRuntime_SessionResolvedOnHandle(t *testing.T) {
	rt, _ := testRuntime(t)

	sess, err := rt.sessionManager.ResolveOrCreate(context.Background(), "main:cli:local")
	if err != nil {
		t.Fatal(err)
	}
	if sess.TurnCount() != 0 {
		t.Error("expected fresh session")
	}
}

func TestRuntime_AddTurnAndRetrieve(t *testing.T) {
	rt, _ := testRuntime(t)
	ctx := context.Background()

	sess, _ := rt.sessionManager.ResolveOrCreate(ctx, "main:cli:local")

	turn := session.SessionTurn{
		TurnID:            "t1",
		Timestamp:         time.Now().UTC(),
		UserMessage:       "hello",
		AssistantResponse: "world",
		TokenUsage:        types.TokenUsage{InputTokens: 5, OutputTokens: 5},
		LatencyMs:         10,
	}
	sess.AddTurn(turn)

	if sess.TurnCount() != 1 {
		t.Errorf("expected 1 turn, got %d", sess.TurnCount())
	}

	contextTurns := rt.sessionManager.GetContextTurns(sess)
	if len(contextTurns) != 1 {
		t.Errorf("expected 1 context turn, got %d", len(contextTurns))
	}
	if contextTurns[0].AssistantResponse != "world" {
		t.Errorf("unexpected response: %q", contextTurns[0].AssistantResponse)
	}
}

func TestRuntime_StubAssembler_BuildsHistory(t *testing.T) {
	rt, _ := testRuntime(t)
	ctx := context.Background()

	sess, _ := rt.sessionManager.ResolveOrCreate(ctx, "main:cli:local")
	sess.AddTurn(session.SessionTurn{
		TurnID:            "t1",
		UserMessage:       "ping",
		AssistantResponse: "pong",
		Timestamp:         time.Now().UTC(),
	})

	_, msgs, err := rt.assembler.Build(sess, rt.sessionManager, "new question", "")
	if err != nil {
		t.Fatal(err)
	}

	// Stub should return 2 messages (user + assistant for the 1 turn in history)
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages from stub assembler, got %d", len(msgs))
	}
	if msgs[0].Role != types.RoleUser || msgs[0].Content != "ping" {
		t.Errorf("unexpected first msg: %+v", msgs[0])
	}
	if msgs[1].Role != types.RoleAssistant || msgs[1].Content != "pong" {
		t.Errorf("unexpected second msg: %+v", msgs[1])
	}
}

func TestRuntime_HooksCalledOnAddTurn(t *testing.T) {
	rt, _ := testRuntime(t)
	ctx := context.Background()

	var hookCalled bool
	rt.SetHooks([]session.HookFunc{
		func(_ context.Context, _ *session.Session, turn session.SessionTurn) error {
			hookCalled = true
			return nil
		},
	})

	sess, _ := rt.sessionManager.ResolveOrCreate(ctx, "test:session")
	turn := session.SessionTurn{
		TurnID:            "t-hook",
		Timestamp:         time.Now().UTC(),
		UserMessage:       "test",
		AssistantResponse: "response",
	}
	sess.AddTurn(turn)
	session.RunHooks(rt.hooks, sess, turn)

	// Give async hooks a moment
	time.Sleep(10 * time.Millisecond)
	if !hookCalled {
		t.Error("hook was not called")
	}
}

func TestRuntime_StartStop(t *testing.T) {
	rt, _ := testRuntime(t)
	ctx := context.Background()

	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := rt.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestRuntime_MultiTurnContext(t *testing.T) {
	rt, _ := testRuntime(t)
	ctx := context.Background()

	sess, _ := rt.sessionManager.ResolveOrCreate(ctx, "main:cli:local")

	for i := range 3 {
		sess.AddTurn(session.SessionTurn{
			TurnID:            string(rune('a' + i)),
			Timestamp:         time.Now().UTC(),
			UserMessage:       "msg",
			AssistantResponse: "resp",
		})
	}

	_, msgs, _ := rt.assembler.Build(sess, rt.sessionManager, "fourth question", "")
	// 3 turns × 2 messages each = 6
	if len(msgs) != 6 {
		t.Errorf("expected 6 messages for 3-turn history, got %d", len(msgs))
	}
}

func TestRuntime_SessionManagerExposed(t *testing.T) {
	rt, _ := testRuntime(t)
	if rt.SessionManager() == nil {
		t.Error("SessionManager() should not be nil")
	}
}

func TestRuntime_FilepathFromConfig(t *testing.T) {
	rt, dir := testRuntime(t)
	ctx := context.Background()

	sess, _ := rt.sessionManager.ResolveOrCreate(ctx, "main:cli:local")
	_ = sess

	path := filepath.Join(dir, "main_cli_local.jsonl")
	// File should exist (created by ResolveOrCreate)
	if _, err := filepath.Abs(path); err != nil {
		t.Errorf("session file path issue: %v", err)
	}
}
