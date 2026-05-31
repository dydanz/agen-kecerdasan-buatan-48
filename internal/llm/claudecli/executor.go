package claudecli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// CLIRequest is the input for a claude CLI agent turn.
// The runtime owns conversation context: history injected via AppendSystemPrompt.
// Never use --resume; each invocation is stateless.
type CLIRequest struct {
	Prompt             string
	SystemPrompt       string
	AppendSystemPrompt string   // session history; injected every turn
	Model              string   // bare model name, e.g. "claude-sonnet-4-6"
	MaxTurns           int      // 0 = claude default
	AllowedTools       []string // claude built-in tools; nil = no built-in tools
	MCPConfig          string   // JSON for --mcp-config; empty = no MCP
}

// CLIEvent is one streamed output from the claude subprocess.
type CLIEvent struct {
	Type     string  // "text" | "tool_use" | "tool_result" | "done" | "error"
	Content  string  // non-empty for "text"
	ToolName string  // tool name for "tool_use" and "tool_result"
	CostUSD  float64 // non-zero for "done"
	Err      error   // non-nil for "error"
}

// CLIExecutor spawns a claude subprocess per turn and streams CLIEvents.
// New returns (nil, error) when the binary is absent — callers treat nil as non-fatal.
type CLIExecutor interface {
	Execute(ctx context.Context, req CLIRequest) (<-chan CLIEvent, error)
}

type executor struct {
	binPath string
	// Auth via CLAUDE_CODE_OAUTH_TOKEN env var (set in .env) or ~/.claude/ credentials.
	// No API key injection — avoids conflicts with ANTHROPIC_API_KEY in the parent env.
}

// New returns a CLIExecutor. Returns error if claude is not in PATH.
// Callers must treat (nil, err) as non-fatal: api-mode continues without CLI.
func New() (CLIExecutor, error) {
	path, err := exec.LookPath("claude")
	if err != nil {
		return nil, fmt.Errorf("claude binary not found in PATH: %w", err)
	}
	return &executor{binPath: path}, nil
}

func (e *executor) Execute(ctx context.Context, req CLIRequest) (<-chan CLIEvent, error) {
	args := buildArgs(req)
	cmd := exec.CommandContext(ctx, e.binPath, args...) // #nosec G204 — binPath from LookPath
	// Strip ANTHROPIC_API_KEY — prevents conflict when CLAUDE_CODE_OAUTH_TOKEN is set.
	// CLAUDE_CODE_OAUTH_TOKEN is inherited naturally from os.Environ().
	base := os.Environ()
	filtered := make([]string, 0, len(base))
	for _, v := range base {
		if !strings.HasPrefix(v, "ANTHROPIC_API_KEY=") {
			filtered = append(filtered, v)
		}
	}
	cmd.Env = filtered
	cmd.Dir = os.TempDir() // prevents subprocess reading local CLAUDE.md / hooks

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start claude: %w", err)
	}

	ch := make(chan CLIEvent, 64)
	go func() {
		defer close(ch)
		var lastMsgID string
		var lastTextLen int
		var doneEmitted bool

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1<<20), 1<<20) // 1MB — default 64KB silently truncates

		for scanner.Scan() {
			for _, evt := range parseStreamLine(scanner.Bytes(), &lastMsgID, &lastTextLen) {
				if evt.Type == "done" || evt.Type == "error" {
					doneEmitted = true
				}
				select {
				case ch <- evt:
				case <-ctx.Done():
					_ = cmd.Wait()
					return
				}
			}
		}

		if err := cmd.Wait(); err != nil && !doneEmitted {
			exitCode := -1
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			}
			select {
			case ch <- CLIEvent{
				Type: "error",
				Err:  fmt.Errorf("claude exited %d: %s", exitCode, strings.TrimSpace(stderrBuf.String())),
			}:
			case <-ctx.Done():
			}
		}
	}()
	return ch, nil
}

func buildArgs(req CLIRequest) []string {
	args := []string{
		"--print",
		"--verbose",
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--permission-mode", "bypassPermissions",
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.SystemPrompt != "" {
		args = append(args, "--system-prompt", req.SystemPrompt)
	}
	if req.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(req.MaxTurns))
	}
	if len(req.AllowedTools) > 0 {
		args = append(args, "--tools", strings.Join(req.AllowedTools, ","))
	} else {
		args = append(args, "--tools", "") // explicit empty prevents CLI defaulting to built-in tools
	}
	if req.MCPConfig != "" {
		args = append(args, "--mcp-config", req.MCPConfig)
	}
	if req.AppendSystemPrompt != "" {
		args = append(args, "--append-system-prompt", req.AppendSystemPrompt)
	}
	args = append(args, "--", req.Prompt) // "--" separates flags from prompt
	return args
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Name string `json:"name"` // populated for tool_use blocks
}

type cliMessage struct {
	ID      string         `json:"id"`
	Content []contentBlock `json:"content"`
}

// parseStreamLine parses one JSON line from the claude subprocess stream.
// Returns zero or more CLIEvents — one line may produce both a text delta and a tool_use event.
func parseStreamLine(line []byte, lastMsgID *string, lastTextLen *int) []CLIEvent {
	var raw struct {
		Type    string      `json:"type"`
		Subtype string      `json:"subtype"`
		Message *cliMessage `json:"message"`
		CostUSD float64     `json:"cost_usd"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil
	}

	switch raw.Type {
	case "assistant":
		if raw.Message == nil {
			return nil
		}
		var full strings.Builder
		var toolEvents []CLIEvent

		for _, block := range raw.Message.Content {
			switch block.Type {
			case "text":
				full.WriteString(block.Text)
			case "tool_use":
				toolEvents = append(toolEvents, CLIEvent{Type: "tool_use", ToolName: block.Name})
			}
		}
		fullText := full.String()

		if raw.Message.ID != *lastMsgID {
			*lastMsgID = raw.Message.ID
			*lastTextLen = 0
		}
		// text shrinks when a tool_use block replaces a partial text block
		if *lastTextLen > len(fullText) {
			*lastTextLen = 0
		}
		delta := fullText[*lastTextLen:]
		*lastTextLen = len(fullText)

		var evts []CLIEvent
		if delta != "" {
			evts = append(evts, CLIEvent{Type: "text", Content: delta})
		}
		evts = append(evts, toolEvents...)
		return evts

	case "tool":
		// Tool result messages from the subprocess — surface as tool_result events.
		if raw.Message == nil {
			return nil
		}
		var evts []CLIEvent
		for _, block := range raw.Message.Content {
			if block.Type == "tool_result" {
				evts = append(evts, CLIEvent{Type: "tool_result"})
			}
		}
		return evts

	case "result":
		switch raw.Subtype {
		case "success":
			return []CLIEvent{{Type: "done", CostUSD: raw.CostUSD}}
		default:
			return []CLIEvent{{Type: "error", Err: fmt.Errorf("claude result: %s", raw.Subtype)}}
		}
	}

	return nil // system:init and other types silently ignored
}
