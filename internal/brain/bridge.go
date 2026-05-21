package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dydanz/akb48/internal/config"
	"github.com/dydanz/akb48/internal/tools"
)

// ErrBrainUnavailable is returned by gbrain_* handlers when brain is down.
var ErrBrainUnavailable = errors.New("Brain is disconnected")

// GBrainBridge wraps MCPClient, registers GBrain tools in ToolRegistry,
// monitors health, and auto-restarts on failure.
type GBrainBridge struct {
	client    *MCPClient
	registry  *tools.Registry
	cfg       config.BrainConfig
	available atomic.Bool
	restarts  atomic.Int32
}

// NewGBrainBridge creates a bridge. Call Start to connect.
func NewGBrainBridge(cfg config.BrainConfig, registry *tools.Registry) *GBrainBridge {
	client := NewMCPClient(cfg.GBrainCommand, cfg.GBrainArgs, cfg.GBrainWorkingDir)
	return &GBrainBridge{
		client:   client,
		registry: registry,
		cfg:      cfg,
	}
}

// Start connects to GBrain, discovers tools, and registers them in the registry.
func (b *GBrainBridge) Start(ctx context.Context) error {
	if err := b.client.Start(ctx); err != nil {
		return fmt.Errorf("start MCP client: %w", err)
	}

	specs, err := b.client.ListTools(ctx)
	if err != nil {
		b.client.Stop()
		return fmt.Errorf("list MCP tools: %w", err)
	}

	for _, spec := range specs {
		toolName := b.cfg.ToolPrefix + "_" + spec.Name
		b.registry.Register(
			tools.ToolDefinition{
				Name:        toolName,
				Description: spec.Description,
				InputSchema: spec.InputSchema,
			},
			b.makeHandler(spec.Name),
		)
	}

	b.available.Store(true)
	slog.Info("GBrain connected", "tools", len(specs))
	go b.healthMonitor(ctx)
	return nil
}

// Stop shuts down the MCP client.
func (b *GBrainBridge) Stop() {
	b.available.Store(false)
	b.client.Stop()
}

// Available reports whether GBrain is currently reachable.
func (b *GBrainBridge) Available() bool {
	return b.available.Load()
}

// SearchEntities satisfies session.BrainSearcher for the cold opener.
// Returns "" on unavailability — never errors.
func (b *GBrainBridge) SearchEntities(ctx context.Context, query string, limit int) (string, error) {
	if !b.available.Load() {
		return "", nil
	}

	args, _ := json.Marshal(map[string]any{
		"query":        query,
		"limit":        limit,
		"entity_types": []string{"person", "project", "decision", "product", "policy"},
	})

	result, err := b.client.CallTool(ctx, "search", args)
	if err != nil {
		slog.Debug("GBrain search failed", "query", query, "error", err)
		return "", nil
	}

	return formatSearchResults(result), nil
}

// makeHandler creates a ToolHandler closure for a given GBrain tool name.
func (b *GBrainBridge) makeHandler(toolName string) tools.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if !b.available.Load() {
			return "", ErrBrainUnavailable
		}
		result, err := b.client.CallTool(ctx, toolName, input)
		if err != nil {
			return "", err
		}
		return string(result), nil
	}
}

// healthMonitor polls GBrain and triggers restart on failure.
func (b *GBrainBridge) healthMonitor(ctx context.Context) {
	checkInterval := time.Duration(b.cfg.HealthCheckIntervalS) * time.Second
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	warnTicker := time.NewTicker(60 * time.Second)
	defer warnTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := b.client.ListTools(ctx); err != nil {
				if b.available.Load() {
					slog.Warn("GBrain health check failed — entering degraded mode", "error", err)
					b.available.Store(false)
					go b.attemptRestart(ctx)
				}
			}
		case <-warnTicker.C:
			if !b.available.Load() {
				slog.Warn("GBrain still unavailable — brain features degraded")
			}
		}
	}
}

// attemptRestart tries to reconnect with exponential backoff.
func (b *GBrainBridge) attemptRestart(ctx context.Context) {
	maxAttempts := b.cfg.MaxRestartAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		backoff := time.Duration(1<<uint(attempt-1)) * time.Second // 1s, 2s, 4s
		slog.Info("GBrain restart attempt", "attempt", attempt, "backoff", backoff)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		b.client.Stop()
		newClient := NewMCPClient(b.cfg.GBrainCommand, b.cfg.GBrainArgs, b.cfg.GBrainWorkingDir)
		b.client = newClient

		if err := b.client.Start(ctx); err != nil {
			slog.Error("GBrain restart failed", "attempt", attempt, "error", err)
			continue
		}

		if _, err := b.client.ListTools(ctx); err != nil {
			slog.Error("GBrain restart: tools/list failed", "attempt", attempt, "error", err)
			b.client.Stop()
			continue
		}

		b.restarts.Add(1)
		b.available.Store(true)
		slog.Info("GBrain reconnected", "total_restarts", b.restarts.Load())
		return
	}

	slog.Error("GBrain max restart attempts exhausted — brain permanently degraded",
		"max_attempts", maxAttempts)
}

// formatSearchResults formats raw GBrain search results as a bullet list.
func formatSearchResults(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try array of objects with title/type/created_at
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err == nil && len(items) > 0 {
		var lines []string
		for _, item := range items {
			title, _ := item["title"].(string)
			entityType, _ := item["type"].(string)
			createdAt, _ := item["created_at"].(string)
			if title == "" {
				continue
			}
			line := "- " + title
			if entityType != "" {
				line += " (" + entityType
				if createdAt != "" && len(createdAt) >= 10 {
					line += ", " + createdAt[:10]
				}
				line += ")"
			}
			lines = append(lines, line)
		}
		if len(lines) > 0 {
			return strings.Join(lines, "\n")
		}
	}

	// Fallback: return raw as string
	s := strings.TrimSpace(string(raw))
	if s == "null" || s == "[]" || s == "{}" {
		return ""
	}
	return s
}
