package llm_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dydanz/klawmbing/internal/config"
	"github.com/dydanz/klawmbing/internal/llm"
	"github.com/dydanz/klawmbing/internal/tools"
	"github.com/dydanz/klawmbing/internal/types"
)

func TestIdempotencyKey_Deterministic(t *testing.T) {
	k1 := llm.IdempotencyKey("turn-1", "my_tool", json.RawMessage(`{"x":1}`))
	k2 := llm.IdempotencyKey("turn-1", "my_tool", json.RawMessage(`{"x":1}`))
	if k1 != k2 {
		t.Errorf("same inputs produced different keys: %q vs %q", k1, k2)
	}
}

func TestIdempotencyKey_VariesByTurnID(t *testing.T) {
	input := json.RawMessage(`{"x":1}`)
	k1 := llm.IdempotencyKey("turn-1", "my_tool", input)
	k2 := llm.IdempotencyKey("turn-2", "my_tool", input)
	if k1 == k2 {
		t.Error("different turn IDs should produce different keys")
	}
}

func TestIdempotencyKey_VariesByInput(t *testing.T) {
	k1 := llm.IdempotencyKey("turn-1", "my_tool", json.RawMessage(`{"x":1}`))
	k2 := llm.IdempotencyKey("turn-1", "my_tool", json.RawMessage(`{"x":2}`))
	if k1 == k2 {
		t.Error("different inputs should produce different keys")
	}
}

func TestTruncateToolResult_ShortInput(t *testing.T) {
	out := llm.TruncateToolResult("hello", 500)
	if out != "hello" {
		t.Errorf("short input should be unchanged, got %q", out)
	}
}

func TestTruncateToolResult_LongInput(t *testing.T) {
	long := strings.Repeat("a", 3000)
	out := llm.TruncateToolResult(long, 500)
	if len(out) > 2000+len(" [truncated]") {
		t.Errorf("result not truncated: len=%d", len(out))
	}
	if !strings.HasSuffix(out, " [truncated]") {
		t.Errorf("expected [truncated] suffix, got: %q", out[len(out)-20:])
	}
}

func TestCall_ReturnsText(t *testing.T) {
	t.Skip("integration: requires ANTHROPIC_API_KEY")

	cfg := &config.Config{
		LLM: config.LLMConfig{
			Model:               "claude-haiku-4-5-20251001",
			MaxTokens:           256,
			MaxToolRounds:       3,
			MaxToolResultTokens: 500,
		},
	}
	reg := tools.NewRegistry()
	caller := llm.NewCaller(cfg, reg)

	result, err := caller.Call(context.Background(), llm.CallParams{
		System:   "Reply with exactly the word: pong",
		Messages: []types.LLMMessage{{Role: types.RoleUser, Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text == "" {
		t.Error("expected non-empty response text")
	}
}

func TestCall_ToolLoop(t *testing.T) {
	t.Skip("integration: requires ANTHROPIC_API_KEY")

	cfg := &config.Config{
		LLM: config.LLMConfig{
			Model:               "claude-haiku-4-5-20251001",
			MaxTokens:           256,
			MaxToolRounds:       3,
			MaxToolResultTokens: 500,
		},
	}
	reg := tools.NewRegistry()
	reg.Register(tools.ToolDefinition{
		Name:        "get_time",
		Description: "Returns the current time as an ISO 8601 string",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	}, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "2026-05-01T10:00:00Z", nil
	})

	caller := llm.NewCaller(cfg, reg)
	result, err := caller.Call(context.Background(), llm.CallParams{
		System:   "Use the get_time tool to answer questions about the current time.",
		Messages: []types.LLMMessage{{Role: types.RoleUser, Content: "What time is it right now?"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text == "" {
		t.Error("expected non-empty response after tool loop")
	}
}
