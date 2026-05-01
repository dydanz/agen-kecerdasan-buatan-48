package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dydanz/klawmbing/internal/tools"
	"github.com/dydanz/klawmbing/internal/types"
)

func TestRegistry_ExecuteKnownTool(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(tools.ToolDefinition{
		Name:        "echo",
		Description: "echoes input text",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
	}, func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct{ Text string }
		json.Unmarshal(input, &p)
		return p.Text, nil
	})

	call := types.ToolCall{
		ID:    "call-1",
		Name:  "echo",
		Input: json.RawMessage(`{"text":"hello"}`),
	}
	result := reg.Execute(context.Background(), call, "idem-1")

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Output)
	}
	if result.Output != "hello" {
		t.Errorf("got %q, want %q", result.Output, "hello")
	}
}

func TestRegistry_ExecuteUnknownTool(t *testing.T) {
	reg := tools.NewRegistry()
	call := types.ToolCall{ID: "call-1", Name: "nonexistent"}
	result := reg.Execute(context.Background(), call, "idem-2")

	if !result.IsError {
		t.Fatal("expected IsError=true for unknown tool")
	}
}

func TestRegistry_IdempotencyPreventsDoubleExecution(t *testing.T) {
	reg := tools.NewRegistry()
	callCount := 0
	reg.Register(tools.ToolDefinition{Name: "counter"}, func(ctx context.Context, input json.RawMessage) (string, error) {
		callCount++
		return "done", nil
	})

	call := types.ToolCall{ID: "call-1", Name: "counter"}
	reg.Execute(context.Background(), call, "same-key")
	reg.Execute(context.Background(), call, "same-key")

	if callCount != 1 {
		t.Errorf("got callCount=%d, want 1 (idempotency should prevent second execution)", callCount)
	}
}

func TestRegistry_Definitions(t *testing.T) {
	reg := tools.NewRegistry()
	noop := func(_ context.Context, _ json.RawMessage) (string, error) { return "", nil }
	reg.Register(tools.ToolDefinition{Name: "a"}, noop)
	reg.Register(tools.ToolDefinition{Name: "b"}, noop)

	defs := reg.Definitions()
	if len(defs) != 2 {
		t.Errorf("got %d definitions, want 2", len(defs))
	}
}

func TestRegistry_DurationRecorded(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(tools.ToolDefinition{Name: "fast"}, func(_ context.Context, _ json.RawMessage) (string, error) {
		return "ok", nil
	})

	call := types.ToolCall{ID: "c1", Name: "fast"}
	result := reg.Execute(context.Background(), call, "idem-dur")

	if result.DurationMs < 0 {
		t.Errorf("expected non-negative DurationMs, got %d", result.DurationMs)
	}
}
