package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"

	"github.com/dydanz/klawmbing/internal/config"
	"github.com/dydanz/klawmbing/internal/tools"
	"github.com/dydanz/klawmbing/internal/types"
)

// CallParams holds input for a single Call invocation.
type CallParams struct {
	System   string
	Messages []types.LLMMessage
	// Tokens is an optional channel to which streamed text tokens are sent.
	// If nil, streaming is disabled and the response is returned in one shot.
	Tokens chan<- string
}

// CallResult is the output of a successful Call.
type CallResult struct {
	Text       string
	TokenUsage types.TokenUsage
	LatencyMs  int64
}

// Caller wraps the Anthropic SDK and executes the tool loop.
type Caller struct {
	cfg      *config.Config
	registry *tools.Registry
	client   anthropic.Client
}

// NewCaller creates a Caller. The API key is read from config at call time.
func NewCaller(cfg *config.Config, registry *tools.Registry) *Caller {
	client := anthropic.NewClient(
		option.WithAPIKey(cfg.AnthropicAPIKey()),
	)
	return &Caller{
		cfg:      cfg,
		registry: registry,
		client:   client,
	}
}

// TruncateToolResult caps tool output at maxTokens*4 characters (rough token estimate).
// If truncated, " [truncated]" is appended.
func TruncateToolResult(output string, maxTokens int) string {
	maxChars := maxTokens * 4
	if len(output) <= maxChars {
		return output
	}
	return output[:maxChars] + " [truncated]"
}

// Call runs the LLM + tool loop and returns the final text result.
func (c *Caller) Call(ctx context.Context, params CallParams) (*CallResult, error) {
	start := time.Now()

	// Build the system prompt blocks.
	var systemBlocks []anthropic.TextBlockParam
	if params.System != "" {
		systemBlocks = []anthropic.TextBlockParam{{Text: params.System}}
	}

	// Convert conversation history to SDK MessageParams.
	messages := make([]anthropic.MessageParam, 0, len(params.Messages))
	for _, m := range params.Messages {
		block := anthropic.NewTextBlock(m.Content)
		switch m.Role {
		case types.RoleAssistant:
			messages = append(messages, anthropic.NewAssistantMessage(block))
		default:
			messages = append(messages, anthropic.NewUserMessage(block))
		}
	}

	// Build tool list from registry.
	toolDefs := c.registry.Definitions()
	sdkTools := make([]anthropic.ToolUnionParam, 0, len(toolDefs))
	for _, def := range toolDefs {
		// Parse the input schema JSON into ToolInputSchemaParam.
		var schemaMap map[string]any
		if err := json.Unmarshal(def.InputSchema, &schemaMap); err != nil {
			return nil, fmt.Errorf("invalid input schema for tool %q: %w", def.Name, err)
		}
		schema := anthropic.ToolInputSchemaParam{
			Properties: schemaMap["properties"],
		}
		if required, ok := schemaMap["required"].([]interface{}); ok {
			strs := make([]string, 0, len(required))
			for _, r := range required {
				if s, ok := r.(string); ok {
					strs = append(strs, s)
				}
			}
			schema.Required = strs
		}
		toolParam := anthropic.ToolUnionParamOfTool(schema, def.Name)
		if def.Description != "" {
			toolParam.OfTool.Description = anthropic.String(def.Description)
		}
		sdkTools = append(sdkTools, toolParam)
	}

	var totalUsage types.TokenUsage
	var finalText string

	maxRounds := c.cfg.LLM.MaxToolRounds
	if maxRounds <= 0 {
		maxRounds = 5
	}

	for round := 0; round < maxRounds; round++ {
		reqParams := anthropic.MessageNewParams{
			Model:     anthropic.Model(c.cfg.LLM.Model),
			MaxTokens: int64(c.cfg.LLM.MaxTokens),
			System:    systemBlocks,
			Messages:  messages,
		}
		if len(sdkTools) > 0 {
			reqParams.Tools = sdkTools
		}

		var stopReason anthropic.StopReason
		var responseContent []anthropic.ContentBlockUnion
		var roundUsage types.TokenUsage

		if params.Tokens != nil {
			// Streaming mode.
			stream := c.client.Messages.NewStreaming(ctx, reqParams)
			var accumulated anthropic.Message
			for stream.Next() {
				event := stream.Current()
				accumulated.Accumulate(event) //nolint:errcheck

				// Stream text tokens to the channel.
				if event.Type == "content_block_delta" {
					if event.Delta.Type == "text_delta" {
						select {
						case params.Tokens <- event.Delta.Text:
						case <-ctx.Done():
							return nil, ctx.Err()
						}
					}
				}

				// Capture usage from message_delta event.
				// InputTokens, CacheRead, and CacheWrite come only from message_start.
				if event.Type == "message_delta" {
					msgDelta := event.AsMessageDelta()
					roundUsage.OutputTokens += msgDelta.Usage.OutputTokens
					stopReason = msgDelta.Delta.StopReason
				}

				// Capture input tokens from message_start.
				if event.Type == "message_start" {
					msgStart := event.AsMessageStart()
					roundUsage.InputTokens += msgStart.Message.Usage.InputTokens
					roundUsage.CacheRead += msgStart.Message.Usage.CacheReadInputTokens
					roundUsage.CacheWrite += msgStart.Message.Usage.CacheCreationInputTokens
				}
			}
			if err := stream.Err(); err != nil {
				return nil, fmt.Errorf("streaming error: %w", err)
			}
			responseContent = accumulated.Content
			if stopReason == "" {
				stopReason = accumulated.StopReason
			}
			if stopReason == "" {
				slog.Warn("stream completed with empty stop reason", "model", c.cfg.LLM.Model)
			}
		} else {
			// Non-streaming mode.
			resp, err := c.client.Messages.New(ctx, reqParams)
			if err != nil {
				return nil, fmt.Errorf("anthropic API error: %w", err)
			}
			responseContent = resp.Content
			stopReason = resp.StopReason
			roundUsage.InputTokens = resp.Usage.InputTokens
			roundUsage.OutputTokens = resp.Usage.OutputTokens
			roundUsage.CacheRead = resp.Usage.CacheReadInputTokens
			roundUsage.CacheWrite = resp.Usage.CacheCreationInputTokens
		}

		// Accumulate token usage across rounds.
		totalUsage.InputTokens += roundUsage.InputTokens
		totalUsage.OutputTokens += roundUsage.OutputTokens
		totalUsage.CacheRead += roundUsage.CacheRead
		totalUsage.CacheWrite += roundUsage.CacheWrite

		// Extract text from response content blocks.
		var roundText string
		for _, block := range responseContent {
			if block.Type == "text" {
				roundText += block.Text
			}
		}
		if roundText != "" {
			finalText = roundText
		}

		// Check stop reason.
		if stopReason == anthropic.StopReasonEndTurn ||
			stopReason == anthropic.StopReasonMaxTokens ||
			stopReason == anthropic.StopReasonStopSequence {
			break
		}

		if stopReason != anthropic.StopReasonToolUse {
			// Unexpected stop reason — treat as done.
			break
		}

		// Build the assistant message with all response blocks.
		assistantBlocks := make([]anthropic.ContentBlockParamUnion, 0, len(responseContent))
		for _, block := range responseContent {
			assistantBlocks = append(assistantBlocks, block.ToParam())
		}
		messages = append(messages, anthropic.NewAssistantMessage(assistantBlocks...))

		// Execute each tool_use block and collect results.
		toolResultBlocks := make([]anthropic.ContentBlockParamUnion, 0)
		for _, block := range responseContent {
			if block.Type != "tool_use" {
				continue
			}
			inputJSON, err := json.Marshal(block.Input)
			if err != nil {
				inputJSON = json.RawMessage(`{}`)
			}
			toolCall := types.ToolCall{
				ID:    block.ID,
				Name:  block.Name,
				Input: inputJSON,
			}
			result := c.registry.Execute(ctx, toolCall, toolCall.ID)
			output := TruncateToolResult(result.Output, c.cfg.LLM.MaxToolResultTokens)
			toolResultBlocks = append(toolResultBlocks,
				anthropic.NewToolResultBlock(block.ID, output, result.IsError),
			)
		}

		// Append tool results as a user message.
		messages = append(messages, anthropic.NewUserMessage(toolResultBlocks...))
	}

	latencyMs := time.Since(start).Milliseconds()
	return &CallResult{
		Text:       finalText,
		TokenUsage: totalUsage,
		LatencyMs:  latencyMs,
	}, nil
}

// Ensure ssestream import is used.
var _ *ssestream.Stream[anthropic.MessageStreamEventUnion]
