package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/uuid"

	"github.com/dydanz/akb48/internal/brain"
	"github.com/dydanz/akb48/internal/config"
	"github.com/dydanz/akb48/internal/env"
	"github.com/dydanz/akb48/internal/identity"
	"github.com/dydanz/akb48/internal/llm"
	"github.com/dydanz/akb48/internal/llm/claudecli"
	"github.com/dydanz/akb48/internal/session"
	"github.com/dydanz/akb48/internal/skills"
	"github.com/dydanz/akb48/internal/tools"
	githubtools "github.com/dydanz/akb48/internal/tools/github"
	searchtools "github.com/dydanz/akb48/internal/tools/search"
	"github.com/dydanz/akb48/internal/types"
)

// MessageHandler is the function signature adapters call per incoming message.
type MessageHandler func(ctx context.Context, msg types.Message, tokens chan<- string) error

// ContextAssembler builds the LLM context for a given session turn.
type ContextAssembler interface {
	Build(
		sess *session.Session,
		manager *session.SessionManager,
		userMessage string,
		coldContext string,
	) (systemPrompt string, messages []anthropic.MessageParam, err error)
}

// AKB48Runtime wires all components together.
type AKB48Runtime struct {
	cfg            *config.Config
	llmCaller      llm.CallerInterface
	cliExec        claudecli.CLIExecutor // nil when backend != "claude-cli" or binary absent
	registry       *tools.Registry
	sessionManager *session.SessionManager
	assembler      ContextAssembler
	coldOpener     *session.ColdOpener
	bridge         *brain.GBrainBridge
	hooks          []session.HookFunc
	envReader      *env.Reader
}

// New creates and wires all runtime components.
func New(cfg *config.Config) (*AKB48Runtime, error) {
	envReader := env.New(".env")
	registry := tools.NewRegistry()

	// Register built-in tools.
	ghDef, ghHandler := githubtools.NewHandler(envReader, cfg.LLM.MaxToolResultTokens)
	registry.Register(ghDef, ghHandler)
	searchDef, searchHandler := searchtools.NewHandler(envReader, cfg.LLM.MaxToolResultTokens)
	registry.Register(searchDef, searchHandler)
	caller := llm.NewCaller(cfg, registry)
	sessionMgr := session.NewSessionManager(cfg.Session)

	// Wire GBrain bridge if enabled.
	var brainBridge *brain.GBrainBridge
	brainStatus := "disabled"

	if cfg.Brain.Enabled {
		brainBridge = brain.NewGBrainBridge(cfg.Brain, registry)
		if err := brainBridge.Start(context.Background()); err != nil {
			slog.Warn("GBrain failed to start — running in degraded mode", "error", err)
			brainStatus = "degraded (unavailable)"
			brainBridge = nil
		} else {
			brainStatus = fmt.Sprintf("connected (%d tools)", len(registry.Definitions()))
		}
	}

	// Wire cold opener (requires brain).
	var coldOpener *session.ColdOpener
	if brainBridge != nil {
		coldOpener = session.NewColdOpener(brainBridge, cfg.Session.ColdResumeThresholdMinutes)
	}

	// Wire full 4-tier context assembler.
	resolver := skills.NewResolver(cfg.Skills.Dir)
	assembler := identity.New(cfg.Identity, resolver, sessionMgr)
	if err := assembler.Load(); err != nil {
		slog.Warn("Identity files not loaded — agent will use empty system prompt", "error", err)
	}

	// Wire CLI executor if backend = "claude-cli" (non-fatal if binary absent).
	var cliExec claudecli.CLIExecutor
	if cfg.LLM.Backend == "claude-cli" {
		var cliErr error
		cliExec, cliErr = claudecli.New()
		if cliErr != nil {
			slog.Warn("CLIExecutor unavailable — cli-mode messages will return errors", "error", cliErr)
		}
	}

	rt := &AKB48Runtime{
		cfg:            cfg,
		llmCaller:      caller,
		cliExec:        cliExec,
		registry:       registry,
		sessionManager: sessionMgr,
		assembler:      assembler,
		coldOpener:     coldOpener,
		bridge:         brainBridge,
		envReader:      envReader,
	}

	metricsPath := cfg.Session.StorageDir + "/tool-calls.jsonl"
	extractor := makeExtractor(cfg, cliExec)
	rt.hooks = []session.HookFunc{
		session.PersistSessionHook(sessionMgr),
		session.LogMetricsHook(metricsPath),
		session.CompactionHook(cfg.Session, sessionMgr, brainBridge, extractor),
		session.NarrationGuardHook(),
	}

	slog.Info("AKB48 initialised",
		"brain", brainStatus,
		"skills", countSkillDirs(cfg.Skills.Dir),
		"identity_dir", cfg.Identity.Dir,
	)

	return rt, nil
}

// HandleMessage processes one incoming message end-to-end.
func (r *AKB48Runtime) HandleMessage(ctx context.Context, msg types.Message, tokens chan<- string) error {
	start := time.Now()

	sess, err := r.sessionManager.ResolveOrCreate(ctx, msg.SessionID)
	if err != nil {
		return fmt.Errorf("resolve session: %w", err)
	}

	turnID := uuid.NewString()
	var finalText string
	var tokenUsage types.TokenUsage
	var toolEventCount int

	switch r.cfg.LLM.Backend {
	case "claude-cli":
		finalText, toolEventCount, err = r.handleCLI(ctx, sess, msg.Text, tokens, turnID)
	default:
		finalText, tokenUsage, toolEventCount, err = r.handleAPI(ctx, sess, msg.Text, tokens, turnID)
	}
	if err != nil {
		return err
	}

	turn := session.SessionTurn{
		TurnID:            turnID,
		Timestamp:         time.Now().UTC(),
		UserMessage:       msg.Text,
		AssistantResponse: finalText,
		TokenUsage:        tokenUsage,
		LatencyMs:         time.Since(start).Milliseconds(),
		ToolEventCount:    toolEventCount,
	}
	sess.AddTurn(turn)
	session.RunHooks(r.hooks, sess, turn)
	return nil
}

func (r *AKB48Runtime) handleAPI(ctx context.Context, sess *session.Session, text string, tokens chan<- string, turnID string) (string, types.TokenUsage, int, error) {
	apiTools := r.registry.Definitions()
	toolNames := make([]string, len(apiTools))
	for i, d := range apiTools {
		toolNames[i] = d.Name
	}
	slog.Info("agent toolset", "backend", "api", "tools", toolNames)

	coldContext := ""
	if r.coldOpener != nil {
		coldContext = r.coldOpener.BuildColdContext(ctx, sess, text)
	}
	systemPrompt, messages, err := r.assembler.Build(sess, r.sessionManager, text, coldContext)
	if err != nil {
		return "", types.TokenUsage{}, 0, fmt.Errorf("build context: %w", err)
	}
	messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(text)))
	result, err := r.llmCaller.Call(ctx, llm.CallParams{
		System:   systemPrompt,
		Messages: messages,
		Tokens:   tokens,
		TurnID:   turnID,
	})
	if err != nil {
		return "", types.TokenUsage{}, 0, fmt.Errorf("llm call: %w", err)
	}
	return result.Text, result.TokenUsage, result.ToolCalls, nil
}

func (r *AKB48Runtime) handleCLI(ctx context.Context, sess *session.Session, text string, tokens chan<- string, turnID string) (string, int, error) {
	if r.cliExec == nil {
		return "", 0, fmt.Errorf("cli backend unavailable (claude binary missing or not authenticated)")
	}
	// Get system prompt from assembler; discard messages[] — history goes via --append-system-prompt.
	systemPrompt, _, err := r.assembler.Build(sess, r.sessionManager, text, "")
	if err != nil {
		return "", 0, fmt.Errorf("build system prompt: %w", err)
	}
	history := r.sessionManager.GetContextTurns(sess)
	appendCtx := buildAppendContext(history)

	// Wire brain as MCP server so claude subprocess can call gbrain_* tools.
	mcpConfigPath := ""
	if r.bridge != nil && r.bridge.Available() {
		mcpConfigPath, err = writeMCPConfigFile(r.bridge, r.cfg.ExtraMCPServers, r.envReader)
		if err != nil {
			slog.Warn("failed to write MCP config — brain tools unavailable this turn", "error", err)
		} else {
			defer os.Remove(mcpConfigPath)
		}
	}

	// Resolve tool allowlist: config override takes precedence; empty → compiled default.
	// defaultAllowedTools always includes built-ins so brain-down never yields zero tools.
	allowedTools := r.cfg.LLM.AllowedTools
	if len(allowedTools) == 0 {
		allowedTools = defaultAllowedTools(mcpConfigPath != "")
	} else if mcpConfigPath != "" {
		hasToolSearch := false
		for _, t := range allowedTools {
			if t == "ToolSearch" {
				hasToolSearch = true
				break
			}
		}
		if !hasToolSearch {
			allowedTools = append(allowedTools, "ToolSearch")
		}
	}
	slog.Info("agent toolset", "backend", "claude-cli", "tools", allowedTools)

	ch, err := r.cliExec.Execute(ctx, claudecli.CLIRequest{
		Prompt:             text,
		SystemPrompt:       systemPrompt,
		AppendSystemPrompt: appendCtx,
		Model:              r.cfg.LLM.Model,
		MCPConfig:          mcpConfigPath,
		AllowedTools:       allowedTools,
	})
	if err != nil {
		return "", 0, fmt.Errorf("cli execute: %w", err)
	}

	var sb strings.Builder
	var toolEventCount int
	toolCounts := map[string]int{} // tool name → call count this turn

	for evt := range ch {
		switch evt.Type {
		case "text":
			sb.WriteString(evt.Content)
			select {
			case tokens <- evt.Content:
			case <-ctx.Done():
				return sb.String(), toolEventCount, ctx.Err()
			}
		case "tool_use":
			toolEventCount++
			toolCounts[evt.ToolName]++ // count silently; summary emitted after loop
		case "error":
			return sb.String(), toolEventCount, evt.Err
		}
	}

	// Emit one compact summary line instead of per-call indicators.
	if len(toolCounts) > 0 {
		names := make([]string, 0, len(toolCounts))
		for n := range toolCounts {
			names = append(names, n)
		}
		sort.Strings(names)
		var parts []string
		for _, n := range names {
			c := toolCounts[n]
			if c == 1 {
				parts = append(parts, n)
			} else {
				parts = append(parts, fmt.Sprintf("%s ×%d", n, c))
			}
		}
		summary := "\n⚙ Ran: " + strings.Join(parts, ", ") + "\n"
		select {
		case tokens <- summary:
		default:
		}
	}

	return sb.String(), toolEventCount, nil
}

// Start loads sessions and prepares the runtime.
func (r *AKB48Runtime) Start(ctx context.Context) error {
	if r.cfg.Session.LoadOnStartup {
		if err := r.sessionManager.LoadAll(ctx); err != nil {
			return fmt.Errorf("load sessions: %w", err)
		}
	}
	slog.Info("AKB48 started", "model", r.cfg.LLM.Model)
	return nil
}

// Stop shuts down all components gracefully.
func (r *AKB48Runtime) Stop() error {
	if r.bridge != nil {
		r.bridge.Stop()
	}
	return r.sessionManager.Shutdown()
}

// SetAssembler replaces the context assembler (tests).
func (r *AKB48Runtime) SetAssembler(a ContextAssembler) {
	r.assembler = a
}

// SetLLMCaller replaces the LLM caller (tests).
func (r *AKB48Runtime) SetLLMCaller(c llm.CallerInterface) {
	r.llmCaller = c
}

// SetColdOpener replaces the cold opener (tests).
func (r *AKB48Runtime) SetColdOpener(c *session.ColdOpener) {
	r.coldOpener = c
}

// SetHooks replaces hooks (tests).
func (r *AKB48Runtime) SetHooks(hooks []session.HookFunc) {
	r.hooks = hooks
}

// SessionManager exposes the session manager (adapters and tests).
func (r *AKB48Runtime) SessionManager() *session.SessionManager {
	return r.sessionManager
}

// BrainAvailable reports whether GBrain is connected.
func (r *AKB48Runtime) BrainAvailable() bool {
	return r.bridge != nil && r.bridge.Available()
}

// EnvReader returns the gated env reader for use by tool handler factories.
func (r *AKB48Runtime) EnvReader() *env.Reader {
	return r.envReader
}

// writeMCPConfigFile writes the combined MCP config (brain + extra SSE servers) to a temp file.
// Caller must os.Remove the file when done.
func writeMCPConfigFile(b *brain.GBrainBridge, extras []config.MCPServerConfig, er *env.Reader) (string, error) {
	// Start with the brain server config.
	var mcpCfg map[string]any
	if err := json.Unmarshal(func() []byte {
		d, _ := b.CLIMCPConfig()
		return d
	}(), &mcpCfg); err != nil {
		return "", fmt.Errorf("parse brain MCP config: %w", err)
	}

	servers, _ := mcpCfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
		mcpCfg["mcpServers"] = servers
	}

	// Merge extra SSE/HTTP MCP servers from config.
	for _, s := range extras {
		url := er.Get(s.URLEnv)
		if url == "" {
			slog.Warn("extra MCP server skipped — URL env var not set or not declared in .env",
				"name", s.Name, "url_env", s.URLEnv)
			continue
		}
		srv := map[string]any{"url": url}
		if s.TokenEnv != "" {
			if tok := er.Get(s.TokenEnv); tok != "" {
				srv["headers"] = map[string]string{
					"Authorization": "Bearer " + tok,
				}
			}
		}
		servers[s.Name] = srv
		slog.Info("extra MCP server wired", "name", s.Name)
	}

	data, err := json.Marshal(mcpCfg)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "akb48-mcp-*.json")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	f.Close()
	return f.Name(), nil
}

// makeExtractor returns a session.Extractor that calls haiku for entity extraction + summarisation.
// Falls back gracefully: missing API key → empty entities + turn count summary.
func makeExtractor(cfg *config.Config, cliExec claudecli.CLIExecutor) session.Extractor {
	model := cfg.LLM.ExtractionModel
	if model == "" {
		model = cfg.LLM.Model
	}

	return func(ctx context.Context, turns []session.SessionTurn) (json.RawMessage, string, error) {
		var sb strings.Builder
		for _, t := range turns {
			sb.WriteString("USER: " + t.UserMessage + "\n")
			sb.WriteString("ASSISTANT: " + t.AssistantResponse + "\n\n")
		}
		body := sb.String()

		extractPrompt := "Extract named entities from the conversation. Return a JSON array only — no prose. " +
			"Each element: {\"name\":\"...\",\"entityType\":\"person|project|decision|product|policy|infra\",\"observations\":[\"one-line fact\"]}. " +
			"Only facts worth remembering weeks from now.\n\n" + body

		summaryPrompt := "Summarise these conversation turns in 2-3 sentences. " +
			"Focus on decisions made, tasks completed, and open questions. Be specific.\n\n" + body

		fallbackSummary := fmt.Sprintf("Compacted %d turns.", len(turns))

		// cli backend: use CLILLMClient (single-shot subprocess)
		if cfg.LLM.Backend == "claude-cli" && cliExec != nil {
			client := claudecli.NewCLILLMClient(cliExec)
			rawEntities, err := client.Call(ctx, "Return only valid JSON.", extractPrompt, model)
			var entitiesJSON json.RawMessage
			if err == nil {
				entitiesJSON = session.ParseEntitiesJSON(rawEntities)
			}
			summary, err := client.Call(ctx, "You are a concise technical summariser.", summaryPrompt, model)
			if err != nil || summary == "" {
				summary = fallbackSummary
			}
			return entitiesJSON, summary, nil
		}

		// api backend: Anthropic SDK single-shot (no streaming, no tool loop)
		apiKey := os.Getenv(cfg.LLM.APIKeyEnv)
		if apiKey == "" {
			return nil, fallbackSummary, nil
		}
		client := anthropic.NewClient(option.WithAPIKey(apiKey))

		var entitiesJSON json.RawMessage
		resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(model),
			MaxTokens: 1024,
			Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(extractPrompt))},
		})
		if err == nil && len(resp.Content) > 0 {
			entitiesJSON = session.ParseEntitiesJSON(resp.Content[0].Text)
		}

		summary := fallbackSummary
		resp2, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(model),
			MaxTokens: 256,
			Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(summaryPrompt))},
		})
		if err == nil && len(resp2.Content) > 0 && resp2.Content[0].Text != "" {
			summary = resp2.Content[0].Text
		}

		return entitiesJSON, summary, nil
	}
}

// defaultAllowedTools returns the compiled default cli-mode toolset.
// Built-in tools are always present so brain-down never yields zero tools.
// ToolSearch is added only when MCP config is present (brain connected).
func defaultAllowedTools(mcpPresent bool) []string {
	base := []string{"Bash", "Read", "Write", "Glob", "Grep", "WebFetch"}
	if mcpPresent {
		return append(base, "ToolSearch")
	}
	return base
}

func countSkillDirs(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			n++
		}
	}
	return n
}
