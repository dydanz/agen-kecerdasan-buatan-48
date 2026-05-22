package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"

	"github.com/dydanz/akb48/internal/brain"
	"github.com/dydanz/akb48/internal/config"
	"github.com/dydanz/akb48/internal/identity"
	"github.com/dydanz/akb48/internal/llm"
	"github.com/dydanz/akb48/internal/session"
	"github.com/dydanz/akb48/internal/skills"
	"github.com/dydanz/akb48/internal/tools"
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
	llmCaller      *llm.Caller
	registry       *tools.Registry
	sessionManager *session.SessionManager
	assembler      ContextAssembler
	coldOpener     *session.ColdOpener
	bridge         *brain.GBrainBridge
	hooks          []session.HookFunc
}

// New creates and wires all runtime components.
func New(cfg *config.Config) (*AKB48Runtime, error) {
	registry := tools.NewRegistry()
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

	rt := &AKB48Runtime{
		cfg:            cfg,
		llmCaller:      caller,
		registry:       registry,
		sessionManager: sessionMgr,
		assembler:      assembler,
		coldOpener:     coldOpener,
		bridge:         brainBridge,
	}

	metricsPath := cfg.Session.StorageDir + "/tool-calls.jsonl"
	rt.hooks = []session.HookFunc{
		session.PersistSessionHook(sessionMgr),
		session.LogMetricsHook(metricsPath),
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

	// Cold opener: brain recall context injected for cold sessions.
	coldContext := ""
	if r.coldOpener != nil {
		coldContext = r.coldOpener.BuildColdContext(ctx, sess, msg.Text)
	}

	systemPrompt, messages, err := r.assembler.Build(sess, r.sessionManager, msg.Text, coldContext)
	if err != nil {
		return fmt.Errorf("build context: %w", err)
	}

	messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(msg.Text)))

	turnID := uuid.NewString()

	result, err := r.llmCaller.Call(ctx, llm.CallParams{
		System:   systemPrompt,
		Messages: messages,
		Tokens:   tokens,
		TurnID:   turnID,
	})
	if err != nil {
		return fmt.Errorf("llm call: %w", err)
	}

	turn := session.SessionTurn{
		TurnID:            turnID,
		Timestamp:         time.Now().UTC(),
		UserMessage:       msg.Text,
		AssistantResponse: result.Text,
		TokenUsage:        result.TokenUsage,
		LatencyMs:         time.Since(start).Milliseconds(),
	}

	sess.AddTurn(turn)
	session.RunHooks(r.hooks, sess, turn)

	return nil
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
