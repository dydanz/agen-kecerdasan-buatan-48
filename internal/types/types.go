package types

import (
	"encoding/json"
	"time"
)

type TokenUsage struct {
	InputTokens  int64
	OutputTokens int64
	CacheRead    int64
	CacheWrite   int64
}

type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

type ToolResult struct {
	ToolCallID string
	Output     string
	IsError    bool
	DurationMs int64
}

type Message struct {
	SessionID string
	Text      string
	UserID    string
	Timestamp time.Time
}

type Response struct {
	Text       string
	TokenUsage TokenUsage
	LatencyMs  int64
}

// LLMMessage is a single turn in conversation history passed to the LLM caller.
type LLMMessage struct {
	Role    string // "user" or "assistant"
	Content string
}
