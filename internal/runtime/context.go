package runtime

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/dydanz/akb48/internal/session"
)

const (
	maxHistoryTurns   = 20
	maxAssistantChars = 500
	maxAppendBytes    = 4 * 1024 // 4KB safety cap
)

// buildAppendContext formats recent session history for --append-system-prompt.
// Returns empty string when no history exists.
func buildAppendContext(turns []session.SessionTurn) string {
	if len(turns) == 0 {
		return ""
	}
	start := len(turns) - maxHistoryTurns
	if start < 0 {
		start = 0
	}

	var sb strings.Builder
	sb.WriteString("## Conversation history\n\n")
	for _, t := range turns[start:] {
		assistant := t.AssistantResponse
		if len(assistant) > maxAssistantChars {
			assistant = assistant[:maxAssistantChars] + "…"
		}
		fmt.Fprintf(&sb, "[user] %s\n[assistant] %s\n\n", t.UserMessage, assistant)
	}
	result := strings.TrimRight(sb.String(), "\n")

	if len(result) > maxAppendBytes {
		slog.Warn("buildAppendContext: history exceeds 4KB — truncating to 10 turns")
		return buildAppendContext(turns[len(turns)-10:])
	}
	return result
}
