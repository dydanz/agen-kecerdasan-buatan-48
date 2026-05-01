package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/dydanz/klawmbing/internal/types"
	"github.com/google/uuid"
)

// ToolHandler is the function signature for all registered tool implementations.
type ToolHandler func(ctx context.Context, input json.RawMessage) (string, error)

// ToolDefinition describes a tool's name, purpose, and input schema for the LLM.
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// Registry holds registered tools and dispatches calls with idempotency.
type Registry struct {
	mu          sync.RWMutex
	handlers    map[string]ToolHandler
	definitions map[string]ToolDefinition
	idempotency sync.Map // map[string]string: idempotency_key -> output
}

func NewRegistry() *Registry {
	return &Registry{
		handlers:    make(map[string]ToolHandler),
		definitions: make(map[string]ToolDefinition),
	}
}

// Register adds a tool to the registry. Safe to call concurrently.
func (r *Registry) Register(def ToolDefinition, handler ToolHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[def.Name] = handler
	r.definitions[def.Name] = def
}

// Definitions returns all registered tool definitions for the LLM tool list.
func (r *Registry) Definitions() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]ToolDefinition, 0, len(r.definitions))
	for _, d := range r.definitions {
		defs = append(defs, d)
	}
	return defs
}

// Execute dispatches a tool call. idempotencyKey prevents double-execution on retries.
// Pass an empty string to generate a random key (no deduplication).
func (r *Registry) Execute(ctx context.Context, call types.ToolCall, idempotencyKey string) types.ToolResult {
	if idempotencyKey == "" {
		idempotencyKey = uuid.New().String()
	}

	if cached, ok := r.idempotency.Load(idempotencyKey); ok {
		return types.ToolResult{ToolCallID: call.ID, Output: cached.(string)}
	}

	r.mu.RLock()
	handler, ok := r.handlers[call.Name]
	r.mu.RUnlock()

	if !ok {
		return types.ToolResult{
			ToolCallID: call.ID,
			Output:     fmt.Sprintf("unknown tool: %s", call.Name),
			IsError:    true,
		}
	}

	start := time.Now()
	output, err := handler(ctx, call.Input)
	dur := time.Since(start).Milliseconds()

	if err != nil {
		return types.ToolResult{
			ToolCallID: call.ID,
			Output:     err.Error(),
			IsError:    true,
			DurationMs: dur,
		}
	}

	r.idempotency.Store(idempotencyKey, output)
	return types.ToolResult{
		ToolCallID: call.ID,
		Output:     output,
		DurationMs: dur,
	}
}
