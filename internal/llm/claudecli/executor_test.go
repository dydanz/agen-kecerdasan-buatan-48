package claudecli

import (
	"strings"
	"testing"
)

func TestBuildArgs_NoTools(t *testing.T) {
	args := buildArgs(CLIRequest{Prompt: "hello"})
	found := false
	for i, a := range args {
		if a == "--tools" {
			found = true
			if i+1 < len(args) && args[i+1] != "" {
				t.Errorf("expected empty --tools value, got %q", args[i+1])
			}
		}
	}
	if !found {
		t.Error("expected --tools flag when AllowedTools nil")
	}
}

func TestBuildArgs_WithTools(t *testing.T) {
	args := buildArgs(CLIRequest{Prompt: "hi", AllowedTools: []string{"Bash", "Read"}})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--tools Bash,Read") {
		t.Errorf("expected --tools Bash,Read in args, got: %s", joined)
	}
}

func TestBuildArgs_AppendSystemPrompt(t *testing.T) {
	args := buildArgs(CLIRequest{Prompt: "hi", AppendSystemPrompt: "## History\n\n[user] test"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--append-system-prompt") {
		t.Error("expected --append-system-prompt flag")
	}
}

func TestBuildArgs_NoResume(t *testing.T) {
	cases := []CLIRequest{
		{Prompt: "a"},
		{Prompt: "b", AllowedTools: []string{"Bash"}},
		{Prompt: "c", AppendSystemPrompt: "history"},
		{Prompt: "d", MCPConfig: `{"servers":{}}`},
	}
	for _, req := range cases {
		for _, arg := range buildArgs(req) {
			if arg == "--resume" {
				t.Errorf("--resume found in args for request %+v", req)
			}
		}
	}
}

func TestBuildArgs_PromptAfterDoubleDash(t *testing.T) {
	args := buildArgs(CLIRequest{Prompt: "--weird-prompt"})
	foundSep := false
	for i, a := range args {
		if a == "--" {
			foundSep = true
			if i+1 >= len(args) || args[i+1] != "--weird-prompt" {
				t.Error("prompt not immediately after --")
			}
		}
	}
	if !foundSep {
		t.Error("-- separator not found in args")
	}
}

func firstEvt(evts []CLIEvent) (CLIEvent, bool) {
	if len(evts) == 0 {
		return CLIEvent{}, false
	}
	return evts[0], true
}

func hasEventType(evts []CLIEvent, typ string) bool {
	for _, e := range evts {
		if e.Type == typ {
			return true
		}
	}
	return false
}

func TestParseStreamLine_TextDelta(t *testing.T) {
	var lastMsgID string
	var lastTextLen int

	line1 := []byte(`{"type":"assistant","message":{"id":"msg_1","content":[{"type":"text","text":"Hello"}]}}`)
	evt, ok := firstEvt(parseStreamLine(line1, &lastMsgID, &lastTextLen))
	if !ok || evt.Type != "text" || evt.Content != "Hello" {
		t.Errorf("event 1: got %+v ok=%v", evt, ok)
	}

	line2 := []byte(`{"type":"assistant","message":{"id":"msg_1","content":[{"type":"text","text":"Hello World"}]}}`)
	evt, ok = firstEvt(parseStreamLine(line2, &lastMsgID, &lastTextLen))
	if !ok || evt.Type != "text" || evt.Content != " World" {
		t.Errorf("event 2: expected delta \" World\", got %+v ok=%v", evt, ok)
	}
}

func TestParseStreamLine_Shrink(t *testing.T) {
	var lastMsgID string
	var lastTextLen int

	line1 := []byte(`{"type":"assistant","message":{"id":"msg_1","content":[{"type":"text","text":"Hello"}]}}`)
	parseStreamLine(line1, &lastMsgID, &lastTextLen)

	// text block replaced by tool_use — no text delta, but tool_use event emitted
	line2 := []byte(`{"type":"assistant","message":{"id":"msg_1","content":[{"type":"tool_use","name":"Bash"}]}}`)
	evts := parseStreamLine(line2, &lastMsgID, &lastTextLen)
	for _, e := range evts {
		if e.Type == "text" && e.Content != "" {
			t.Errorf("expected no text delta after shrink, got %q", e.Content)
		}
	}
	if !hasEventType(evts, "tool_use") {
		t.Error("expected tool_use event after shrink")
	}
	if lastTextLen != 0 {
		t.Errorf("expected lastTextLen=0 after shrink, got %d", lastTextLen)
	}
}

func TestParseStreamLine_ToolUse(t *testing.T) {
	var lastMsgID string
	var lastTextLen int
	line := []byte(`{"type":"assistant","message":{"id":"msg_1","content":[{"type":"text","text":"Running:"},{"type":"tool_use","name":"Bash"}]}}`)
	evts := parseStreamLine(line, &lastMsgID, &lastTextLen)
	if !hasEventType(evts, "text") {
		t.Error("expected text event")
	}
	if !hasEventType(evts, "tool_use") {
		t.Error("expected tool_use event")
	}
	for _, e := range evts {
		if e.Type == "tool_use" && e.ToolName != "Bash" {
			t.Errorf("expected ToolName=Bash, got %q", e.ToolName)
		}
	}
}

func TestParseStreamLine_NewMessageID(t *testing.T) {
	var lastMsgID string
	var lastTextLen int

	line1 := []byte(`{"type":"assistant","message":{"id":"msg_1","content":[{"type":"text","text":"Hello"}]}}`)
	parseStreamLine(line1, &lastMsgID, &lastTextLen)

	line2 := []byte(`{"type":"assistant","message":{"id":"msg_2","content":[{"type":"text","text":"Hi"}]}}`)
	evt, ok := firstEvt(parseStreamLine(line2, &lastMsgID, &lastTextLen))
	if !ok || evt.Content != "Hi" {
		t.Errorf("expected full text on new message ID, got %+v ok=%v", evt, ok)
	}
}

func TestParseStreamLine_ResultSuccess(t *testing.T) {
	var lastMsgID string
	var lastTextLen int
	line := []byte(`{"type":"result","subtype":"success","cost_usd":0.0042}`)
	evt, ok := firstEvt(parseStreamLine(line, &lastMsgID, &lastTextLen))
	if !ok || evt.Type != "done" || evt.CostUSD != 0.0042 {
		t.Errorf("expected done event with cost, got %+v ok=%v", evt, ok)
	}
}

func TestParseStreamLine_ResultError(t *testing.T) {
	var lastMsgID string
	var lastTextLen int
	line := []byte(`{"type":"result","subtype":"error_during_execution"}`)
	evt, ok := firstEvt(parseStreamLine(line, &lastMsgID, &lastTextLen))
	if !ok || evt.Type != "error" || evt.Err == nil {
		t.Errorf("expected error event, got %+v ok=%v", evt, ok)
	}
}

func TestParseStreamLine_UnknownType(t *testing.T) {
	var lastMsgID string
	var lastTextLen int
	evts := parseStreamLine([]byte(`{"type":"system","subtype":"init"}`), &lastMsgID, &lastTextLen)
	if len(evts) != 0 {
		t.Errorf("expected no events for unknown type, got %v", evts)
	}
}

func TestParseStreamLine_MalformedJSON(t *testing.T) {
	var lastMsgID string
	var lastTextLen int
	evts := parseStreamLine([]byte(`not json`), &lastMsgID, &lastTextLen)
	if len(evts) != 0 {
		t.Errorf("expected no events for malformed JSON, got %v", evts)
	}
}
