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
	Type    string  // "text" | "done" | "error"
	Content string  // non-empty for "text"
	CostUSD float64 // non-zero for "done"
	Err     error   // non-nil for "error"
}

// CLIExecutor spawns a claude subprocess per turn and streams CLIEvents.
// New returns (nil, error) when the binary is absent — callers treat nil as non-fatal.
type CLIExecutor interface {
	Execute(ctx context.Context, req CLIRequest) (<-chan CLIEvent, error)
}

type executor struct {
	binPath string
	apiKey  string // ANTHROPIC_API_KEY; injected into subprocess env if non-empty
}

// New returns a CLIExecutor. Returns error if claude is not in PATH.
// Callers must treat (nil, err) as non-fatal: api-mode continues without CLI.
func New(apiKey string) (CLIExecutor, error) {
	path, err := exec.LookPath("claude")
	if err != nil {
		return nil, fmt.Errorf("claude binary not found in PATH: %w", err)
	}
	return &executor{binPath: path, apiKey: apiKey}, nil
}

func (e *executor) Execute(ctx context.Context, req CLIRequest) (<-chan CLIEvent, error) {
	args := buildArgs(req)
	cmd := exec.CommandContext(ctx, e.binPath, args...) // #nosec G204 — binPath from LookPath
	cmd.Env = os.Environ()
	if e.apiKey != "" {
		cmd.Env = append(cmd.Env, "ANTHROPIC_API_KEY="+e.apiKey)
	}
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
			evt, ok := parseStreamLine(scanner.Bytes(), &lastMsgID, &lastTextLen)
			if ok {
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

type cliMessage struct {
	ID      string `json:"id"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func parseStreamLine(line []byte, lastMsgID *string, lastTextLen *int) (CLIEvent, bool) {
	var raw struct {
		Type    string      `json:"type"`
		Subtype string      `json:"subtype"`
		Message *cliMessage `json:"message"`
		CostUSD float64     `json:"cost_usd"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return CLIEvent{}, false
	}

	switch raw.Type {
	case "assistant":
		if raw.Message == nil {
			return CLIEvent{}, false
		}
		var full strings.Builder
		for _, block := range raw.Message.Content {
			if block.Type == "text" {
				full.WriteString(block.Text)
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

		if delta != "" {
			return CLIEvent{Type: "text", Content: delta}, true
		}

	case "result":
		switch raw.Subtype {
		case "success":
			return CLIEvent{Type: "done", CostUSD: raw.CostUSD}, true
		default:
			return CLIEvent{Type: "error", Err: fmt.Errorf("claude result: %s", raw.Subtype)}, true
		}
	}

	return CLIEvent{}, false // system:init, tool_use, other types silently ignored
}
