package brain

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
	"sync/atomic"
)

// ErrDisconnected is returned when calling a disconnected MCPClient.
var ErrDisconnected = errors.New("MCP client is not connected")

// ToolSpec describes a single tool exposed by the MCP server.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCPClient manages a stdio subprocess running an MCP server.
type MCPClient struct {
	command string
	args    []string
	workDir string

	cmd       *exec.Cmd
	stdin     io.WriteCloser
	responses sync.Map   // map[int64]chan json.RawMessage
	nextID    atomic.Int64
	mu        sync.Mutex // guards connected + stdin writes
	connected bool
}

// NewMCPClient creates an MCPClient. Call Start to launch the subprocess.
func NewMCPClient(command string, args []string, workDir string) *MCPClient {
	return &MCPClient{
		command: command,
		args:    args,
		workDir: workDir,
	}
}

// Start launches the subprocess and begins reading its stdout.
func (c *MCPClient) Start(ctx context.Context) error {
	c.cmd = exec.CommandContext(ctx, c.command, c.args...)
	if c.workDir != "" {
		c.cmd.Dir = c.workDir
	}

	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("start subprocess: %w", err)
	}

	c.stdin = stdin
	go c.readLoop(stdout)

	c.mu.Lock()
	c.connected = true
	c.mu.Unlock()

	return nil
}

// Stop closes stdin and waits for the subprocess to exit.
func (c *MCPClient) Stop() error {
	c.mu.Lock()
	c.connected = false
	stdin := c.stdin
	c.mu.Unlock()

	if stdin != nil {
		stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		return c.cmd.Wait()
	}
	return nil
}

// IsConnected reports whether the client is currently connected.
func (c *MCPClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// readLoop reads newline-delimited JSON-RPC responses and routes them to callers.
func (c *MCPClient) readLoop(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB max line
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			slog.Warn("MCP: non-JSON line on stdout", "line", string(line))
			continue
		}

		if ch, ok := c.responses.LoadAndDelete(resp.ID); ok {
			if resp.Error != nil {
				// Send nil to signal error; caller checks via separate path.
				// For simplicity: encode error as JSON and send as result.
				errPayload, _ := json.Marshal(map[string]string{"_mcp_error": resp.Error.Message})
				ch.(chan json.RawMessage) <- errPayload
			} else {
				ch.(chan json.RawMessage) <- resp.Result
			}
		}
	}

	// readLoop exited — mark disconnected
	c.mu.Lock()
	c.connected = false
	c.mu.Unlock()

	// Drain any pending waiters
	c.responses.Range(func(k, v any) bool {
		v.(chan json.RawMessage) <- nil
		c.responses.Delete(k)
		return true
	})
}

// call makes a JSON-RPC 2.0 request and waits for the response.
func (c *MCPClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	ch := make(chan json.RawMessage, 1)
	c.responses.Store(id, ch)
	defer c.responses.Delete(id)

	req, err := json.Marshal(rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return nil, ErrDisconnected
	}
	_, writeErr := fmt.Fprintf(c.stdin, "%s\n", req)
	c.mu.Unlock()
	if writeErr != nil {
		return nil, writeErr
	}

	select {
	case result := <-ch:
		if result == nil {
			return nil, ErrDisconnected
		}
		// Check if it's an encoded MCP error
		var errCheck map[string]string
		if json.Unmarshal(result, &errCheck) == nil {
			if msg, ok := errCheck["_mcp_error"]; ok {
				return nil, fmt.Errorf("MCP error: %s", msg)
			}
		}
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ListTools calls tools/list and returns all available tool definitions.
func (c *MCPClient) ListTools(ctx context.Context) ([]ToolSpec, error) {
	result, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Tools []ToolSpec `json:"tools"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		// Some MCP servers return the array directly
		var tools []ToolSpec
		if err2 := json.Unmarshal(result, &tools); err2 != nil {
			return nil, fmt.Errorf("parse tools/list response: %w", err)
		}
		return tools, nil
	}
	return resp.Tools, nil
}

// CallTool calls tools/call with the given tool name and JSON arguments.
func (c *MCPClient) CallTool(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	params := map[string]any{
		"name":      name,
		"arguments": args,
	}
	result, err := c.call(ctx, "tools/call", params)
	if err != nil {
		return nil, err
	}
	return result, nil
}
