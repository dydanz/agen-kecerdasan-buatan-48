package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// fakeServer builds a shell command that acts as a minimal MCP server.
// It reads JSON-RPC requests line-by-line and echoes a scripted response.
// echoServerCommand builds a Python MCP server that serves scripted responses then
// echoes `{}` for any extra requests, staying alive until stdin closes.
func echoServerCommand(responses []string) (string, []string) {
	script := `
import sys, json
responses = ` + marshalStrings(responses) + `
idx = 0
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        req = json.loads(line)
        rid = req.get("id", 0)
        if idx < len(responses):
            payload = responses[idx]; idx += 1
            sys.stdout.write(json.dumps({"jsonrpc":"2.0","id":rid,"result":json.loads(payload)}) + "\n")
        else:
            sys.stdout.write(json.dumps({"jsonrpc":"2.0","id":rid,"result":{}}) + "\n")
        sys.stdout.flush()
    except Exception as e:
        sys.stdout.write(json.dumps({"jsonrpc":"2.0","id":0,"error":{"code":-1,"message":str(e)}}) + "\n")
        sys.stdout.flush()
`
	return "python3", []string{"-c", script}
}

func marshalStrings(ss []string) string {
	b, _ := json.Marshal(ss)
	return string(b)
}

func skipIfNoPython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not in PATH")
	}
}

func TestMCPClient_CallAfterStop(t *testing.T) {
	skipIfNoPython(t)
	cmd, args := echoServerCommand([]string{`{"ok":true}`})
	client := NewMCPClient(cmd, args, "")

	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := client.Stop(); err != nil && !errors.Is(err, context.Canceled) {
		// Wait is acceptable to fail on fast exit
		_ = err
	}

	// Wait for disconnection
	time.Sleep(50 * time.Millisecond)

	_, err := client.call(ctx, "test", nil)
	if !errors.Is(err, ErrDisconnected) {
		t.Errorf("expected ErrDisconnected, got %v", err)
	}
}

func TestMCPClient_SingleCall(t *testing.T) {
	skipIfNoPython(t)
	cmd, args := echoServerCommand([]string{`{"answer":42}`})
	client := NewMCPClient(cmd, args, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Stop()

	result, err := client.call(ctx, "test/method", nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}

	var got map[string]int
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["answer"] != 42 {
		t.Errorf("expected answer=42, got %v", got)
	}
}

func TestMCPClient_ConcurrentCalls(t *testing.T) {
	skipIfNoPython(t)

	// Build 10 scripted responses
	responses := make([]string, 10)
	for i := range 10 {
		responses[i] = fmt.Sprintf(`{"index":%d}`, i)
	}

	cmd, args := echoServerCommand(responses)
	client := NewMCPClient(cmd, args, "")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Stop()

	results := make([]json.RawMessage, 10)
	errs := make([]error, 10)

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := client.call(ctx, "test", nil)
			results[i] = r
			errs[i] = e
		}()
	}
	wg.Wait()

	// All 10 should have received a non-nil, non-error result
	for i, e := range errs {
		if e != nil {
			t.Errorf("call[%d] error: %v", i, e)
		}
		if results[i] == nil {
			t.Errorf("call[%d] got nil result", i)
		}
	}
}

func TestMCPClient_ContextCancel(t *testing.T) {
	skipIfNoPython(t)

	// Server that never responds
	neverScript := `import sys, time
for line in sys.stdin:
    time.sleep(9999)`
	client := NewMCPClient("python3", []string{"-c", neverScript}, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Stop()

	callCtx, callCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer callCancel()

	start := time.Now()
	_, err := client.call(callCtx, "test", nil)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("call did not respect context timeout: took %v", time.Since(start))
	}
}

func TestMCPClient_NonJSONLine(t *testing.T) {
	skipIfNoPython(t)

	// Server writes a non-JSON line before the valid response, then stays alive.
	script := `import sys, json
for line in sys.stdin:
    line = line.strip()
    if not line: continue
    req = json.loads(line)
    rid = req.get("id", 0)
    sys.stdout.write("this is not json\n")
    sys.stdout.flush()
    sys.stdout.write(json.dumps({"jsonrpc":"2.0","id":rid,"result":{"ok":True}}) + "\n")
    sys.stdout.flush()`

	client := NewMCPClient("python3", []string{"-c", script}, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Stop()

	result, err := client.call(ctx, "test", nil)
	if err != nil {
		t.Fatalf("expected success despite non-JSON line, got: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestMCPClient_ListTools(t *testing.T) {
	skipIfNoPython(t)

	toolsResp := `{"tools":[{"name":"search","description":"Search brain","inputSchema":{"type":"object"}},{"name":"put","description":"Store entity","inputSchema":{"type":"object"}}]}`
	cmd, args := echoServerCommand([]string{toolsResp})
	client := NewMCPClient(cmd, args, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Stop()

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Name != "search" {
		t.Errorf("expected first tool 'search', got %q", tools[0].Name)
	}
}

func TestMCPClient_IsConnected(t *testing.T) {
	skipIfNoPython(t)
	cmd, args := echoServerCommand(nil)
	client := NewMCPClient(cmd, args, "")

	if client.IsConnected() {
		t.Error("should not be connected before Start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client.Start(ctx)
	if !client.IsConnected() {
		t.Error("should be connected after Start")
	}

	client.Stop()
	time.Sleep(50 * time.Millisecond)
	if client.IsConnected() {
		t.Error("should not be connected after Stop")
	}
}

// Integration test — requires gbrain in PATH
func TestMCPClient_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: requires gbrain in PATH")
	}
	if _, err := exec.LookPath("gbrain"); err != nil {
		t.Skip("gbrain not in PATH")
	}

	client := NewMCPClient("gbrain", []string{"serve"}, "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Stop()

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) < 1 {
		t.Errorf("expected at least 1 tool, got %d", len(tools))
	}
	t.Logf("GBrain tools: %v", func() []string {
		names := make([]string, len(tools))
		for i, s := range tools {
			names[i] = s.Name
		}
		return names
	}())
}
