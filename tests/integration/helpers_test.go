package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dydanz/akb48/internal/config"
	"github.com/dydanz/akb48/internal/llm"
	"github.com/dydanz/akb48/internal/runtime"
	"github.com/dydanz/akb48/internal/session"
	"github.com/dydanz/akb48/internal/types"
)

// --- Test options ---

type testOption func(*testRig)

type testRig struct {
	storageDir  string
	llmCaller   llm.CallerInterface
	brainBridge *mockBrainBridge
	noBrain     bool
}

func withStorageDir(dir string) testOption {
	return func(r *testRig) { r.storageDir = dir }
}

func withLLMCaller(c llm.CallerInterface) testOption {
	return func(r *testRig) { r.llmCaller = c }
}

func withBrainBridge(b *mockBrainBridge) testOption {
	return func(r *testRig) { r.brainBridge = b }
}

func withNoBrain() testOption {
	return func(r *testRig) { r.noBrain = true }
}

// buildTestRuntime creates a fully-wired runtime with:
//   - Real session manager (temp dir storage)
//   - Real skill resolver (from skills/ dir)
//   - Real identity assembler (from identity/ dir, logs warning if missing — not fatal)
//   - Mock LLM caller
//   - Optional mock brain bridge
func buildTestRuntime(t *testing.T, opts ...testOption) *runtime.AKB48Runtime {
	t.Helper()

	rig := &testRig{
		storageDir: t.TempDir(),
	}
	for _, o := range opts {
		o(rig)
	}

	if rig.llmCaller == nil {
		rig.llmCaller = &mockLLMCaller{responses: []string{"test response"}}
	}

	cfg := &config.Config{
		Session: config.SessionConfig{
			StorageDir:                 rig.storageDir,
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
		Skills:   config.SkillsConfig{Dir: "../../skills"},
		Identity: config.IdentityConfig{Dir: "../../identity"},
		Brain:    config.BrainConfig{Enabled: false},
	}

	rt, err := runtime.New(cfg)
	if err != nil {
		t.Fatalf("buildTestRuntime: %v", err)
	}
	rt.SetLLMCaller(rig.llmCaller)

	if rig.brainBridge != nil {
		rt.SetColdOpener(session.NewColdOpener(rig.brainBridge, 30))
	}

	return rt
}

// sendMessage sends a message and collects all streamed tokens into a string.
func sendMessage(t *testing.T, rt *runtime.AKB48Runtime, sessionID, text string) string {
	t.Helper()
	tokens := make(chan string, 128)
	var wg sync.WaitGroup
	var buf strings.Builder
	wg.Add(1)
	go func() {
		defer wg.Done()
		for tok := range tokens {
			buf.WriteString(tok)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := rt.HandleMessage(ctx, types.Message{
		SessionID: sessionID,
		Text:      text,
		Timestamp: time.Now(),
	}, tokens); err != nil {
		close(tokens)
		wg.Wait()
		t.Fatalf("HandleMessage(%q): %v", text, err)
	}
	close(tokens)
	wg.Wait()
	return buf.String()
}

// --- Mock LLM caller ---

type mockLLMCaller struct {
	mu        sync.Mutex
	responses []string
	idx       int
	Calls     []llm.CallParams
	onCall    func(llm.CallParams)
}

func (m *mockLLMCaller) Call(_ context.Context, params llm.CallParams) (*llm.CallResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, params)
	if m.onCall != nil {
		m.onCall(params)
	}
	resp := "mock response"
	if m.idx < len(m.responses) {
		resp = m.responses[m.idx]
		m.idx++
	}
	if params.Tokens != nil {
		params.Tokens <- resp
	}
	return &llm.CallResult{
		Text:       resp,
		TokenUsage: types.TokenUsage{InputTokens: 10, OutputTokens: 5},
		LatencyMs:  1,
	}, nil
}

// --- Mock brain bridge ---

type mockBrainBridge struct {
	mu           sync.Mutex
	PutCalls     []json.RawMessage
	SearchCalls  []string
	SearchResult string
	SearchErr    error
}

func (m *mockBrainBridge) SearchEntities(_ context.Context, query string, _ int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SearchCalls = append(m.SearchCalls, query)
	return m.SearchResult, m.SearchErr
}
