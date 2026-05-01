package types

import (
	"encoding/json"
	"time"
)

// Role represents the speaker in a conversation turn.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type TokenUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	CacheRead    int64 `json:"cache_read"`
	CacheWrite   int64 `json:"cache_write"`
}

type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Output     string `json:"output"`
	IsError    bool   `json:"is_error"`
	DurationMs int64  `json:"duration_ms"`
}

type Message struct {
	SessionID string    `json:"session_id"`
	Text      string    `json:"text"`
	UserID    string    `json:"user_id"`
	Timestamp time.Time `json:"timestamp"`
}

type Response struct {
	Text       string     `json:"text"`
	TokenUsage TokenUsage `json:"token_usage"`
	LatencyMs  int64      `json:"latency_ms"`
}

// LLMMessage is a single turn in conversation history passed to the LLM caller.
type LLMMessage struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}
