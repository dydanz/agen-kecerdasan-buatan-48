package claudecli

import (
	"context"
	"strings"
)

// CLILLMClient wraps CLIExecutor for internal single-shot calls (e.g. future extraction).
// MaxTurns = 1 — not suitable for agentic loops.
type CLILLMClient struct {
	executor CLIExecutor
}

func NewCLILLMClient(exec CLIExecutor) *CLILLMClient {
	return &CLILLMClient{executor: exec}
}

// Call runs a single prompt turn and returns the full response text.
func (c *CLILLMClient) Call(ctx context.Context, systemPrompt, userPrompt, model string) (string, error) {
	if idx := strings.Index(model, ":"); idx >= 0 {
		model = model[idx+1:] // strip "anthropic:" prefix if present
	}
	ch, err := c.executor.Execute(ctx, CLIRequest{
		Prompt:       userPrompt,
		SystemPrompt: systemPrompt,
		Model:        model,
		MaxTurns:     1,
	})
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for evt := range ch {
		switch evt.Type {
		case "text":
			sb.WriteString(evt.Content)
		case "error":
			return "", evt.Err
		}
	}
	return sb.String(), nil
}
