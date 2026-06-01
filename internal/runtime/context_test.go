package runtime

import (
	"strings"
	"testing"

	"github.com/dydanz/akb48/internal/session"
)

func TestBuildAppendContext_Empty(t *testing.T) {
	if got := buildAppendContext(nil); got != "" {
		t.Errorf("expected empty string for nil turns, got %q", got)
	}
	if got := buildAppendContext([]session.SessionTurn{}); got != "" {
		t.Errorf("expected empty string for empty turns, got %q", got)
	}
}

func TestBuildAppendContext_Format(t *testing.T) {
	turns := []session.SessionTurn{
		{UserMessage: "hello", AssistantResponse: "world"},
		{UserMessage: "foo", AssistantResponse: "bar"},
	}
	got := buildAppendContext(turns)
	if !strings.Contains(got, "## Conversation history") {
		t.Error("expected header")
	}
	if !strings.Contains(got, "[user] hello") {
		t.Error("expected user turn")
	}
	if !strings.Contains(got, "[assistant] world") {
		t.Error("expected assistant turn")
	}
}

func TestBuildAppendContext_TruncatesAssistant(t *testing.T) {
	long := strings.Repeat("x", 600)
	turns := []session.SessionTurn{
		{UserMessage: "q", AssistantResponse: long},
	}
	got := buildAppendContext(turns)
	if strings.Contains(got, long) {
		t.Error("expected assistant response to be truncated at 500 chars")
	}
	if !strings.Contains(got, "…") {
		t.Error("expected ellipsis after truncated assistant text")
	}
}

func TestBuildAppendContext_LimitsTo20Turns(t *testing.T) {
	turns := make([]session.SessionTurn, 30)
	for i := range turns {
		turns[i] = session.SessionTurn{UserMessage: "q", AssistantResponse: "a"}
	}
	got := buildAppendContext(turns)
	count := strings.Count(got, "[user]")
	if count > 20 {
		t.Errorf("expected max 20 turns, got %d", count)
	}
}

// Regression: 10 turns each with 500-char responses exceed 4KB.
// Old code recursed with same input → infinite loop → container crash.
// New code hard-truncates without recursing.
func TestBuildAppendContext_NoInfiniteRecursionOn4KBOverflow(t *testing.T) {
	// Each turn: 500 char assistant response × 10 turns ≈ 5KB — exceeds 4KB cap.
	bigResponse := strings.Repeat("x", 500)
	turns := make([]session.SessionTurn, 10)
	for i := range turns {
		turns[i] = session.SessionTurn{
			UserMessage:       "what is the battery count?",
			AssistantResponse: bigResponse,
		}
	}
	// Must not hang, panic, or recurse.
	got := buildAppendContext(turns)
	if len(got) > maxAppendBytes+1 {
		t.Errorf("result exceeds maxAppendBytes: got %d bytes", len(got))
	}
	// Must return something (not empty).
	if got == "" {
		t.Error("expected non-empty result")
	}
}
