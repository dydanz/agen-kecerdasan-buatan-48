package brain

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dydanz/akb48/internal/config"
	"github.com/dydanz/akb48/internal/tools"
	"github.com/dydanz/akb48/internal/types"
)

func testBrainCfg() config.BrainConfig {
	return config.BrainConfig{
		ToolPrefix:           "gbrain",
		HealthCheckIntervalS: 30,
		MaxRestartAttempts:   3,
	}
}


func TestGBrainBridge_ToolRegistration(t *testing.T) {
	skipIfNoPython(t)

	toolsResp := `{"tools":[
		{"name":"search","description":"Search entities","inputSchema":{"type":"object"}},
		{"name":"put","description":"Store entity","inputSchema":{"type":"object"}},
		{"name":"get","description":"Get entity","inputSchema":{"type":"object"}}
	]}`

	reg := tools.NewRegistry()
	cfg := testBrainCfg()
	bridge := NewGBrainBridge(cfg, reg)
	bridge.client = NewMCPClient("python3", []string{"-c",
		`import sys, json
for line in sys.stdin:
    line = line.strip()
    if not line: continue
    req = json.loads(line)
    rid = req.get("id", 0)
    sys.stdout.write(json.dumps({"jsonrpc":"2.0","id":rid,"result":` + toolsResp + `}) + "\n")
    sys.stdout.flush()`}, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := bridge.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer bridge.Stop()

	defs := reg.Definitions()
	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
	}

	for _, want := range []string{"gbrain_search", "gbrain_put", "gbrain_get"} {
		if !names[want] {
			t.Errorf("expected tool %q registered, got: %v", want, defs)
		}
	}
}

func TestGBrainBridge_AvailableAfterStart(t *testing.T) {
	skipIfNoPython(t)

	reg := tools.NewRegistry()
	cfg := testBrainCfg()
	bridge := NewGBrainBridge(cfg, reg)
	bridge.client = NewMCPClient("python3", []string{"-c",
		`import sys, json
for line in sys.stdin:
    line = line.strip()
    if not line: continue
    req = json.loads(line)
    rid = req.get("id", 0)
    sys.stdout.write(json.dumps({"jsonrpc":"2.0","id":rid,"result":{"tools":[]}}) + "\n")
    sys.stdout.flush()`}, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if bridge.Available() {
		t.Error("should not be available before Start")
	}

	if err := bridge.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer bridge.Stop()

	if !bridge.Available() {
		t.Error("should be available after Start")
	}
}

func TestGBrainBridge_HandlerWhenUnavailable(t *testing.T) {
	reg := tools.NewRegistry()
	cfg := testBrainCfg()
	// Don't start — just register a fake tool manually via a fake bridge
	bridge := &GBrainBridge{
		registry: reg,
		cfg:      cfg,
		client:   NewMCPClient("nonexistent", nil, ""),
	}
	bridge.available.Store(false)

	// Register a handler manually
	reg.Register(tools.ToolDefinition{
		Name:        "gbrain_search",
		Description: "search",
		InputSchema: json.RawMessage(`{}`),
	}, bridge.makeHandler("search"))

	ctx := context.Background()
	toolResult := reg.Execute(ctx, types.ToolCall{
		Name:  "gbrain_search",
		Input: json.RawMessage(`{}`),
	}, "test-key")
	if !toolResult.IsError {
		t.Error("expected error result when brain unavailable")
	}
}

func TestGBrainBridge_SearchEntities_Unavailable(t *testing.T) {
	reg := tools.NewRegistry()
	bridge := &GBrainBridge{
		registry: reg,
		cfg:      testBrainCfg(),
		client:   NewMCPClient("nonexistent", nil, ""),
	}
	bridge.available.Store(false)

	result, err := bridge.SearchEntities(context.Background(), "staging", 3)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result when unavailable, got %q", result)
	}
}

func TestFormatSearchResults_Array(t *testing.T) {
	raw := json.RawMessage(`[
		{"title":"Chose PostgreSQL","type":"decision","created_at":"2025-03-15T10:00:00Z"},
		{"title":"GBrain startup sequence","type":"project","created_at":"2025-04-01T00:00:00Z"}
	]`)

	result := formatSearchResults(raw)
	if result == "" {
		t.Error("expected non-empty formatted result")
	}
	if len(result) < 10 {
		t.Errorf("result too short: %q", result)
	}
	// Should contain titles
	if !contains(result, "Chose PostgreSQL") {
		t.Errorf("expected title in result: %q", result)
	}
}

func TestFormatSearchResults_Empty(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`null`),
		json.RawMessage(`[]`),
		json.RawMessage(`{}`),
		nil,
	} {
		if got := formatSearchResults(raw); got != "" {
			t.Errorf("expected empty for %q, got %q", string(raw), got)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
