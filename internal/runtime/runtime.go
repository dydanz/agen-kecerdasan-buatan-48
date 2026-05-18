package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/dydanz/akb48/internal/config"
	"github.com/dydanz/akb48/internal/llm"
	"github.com/dydanz/akb48/internal/session"
	"github.com/dydanz/akb48/internal/tools"
	"github.com/dydanz/akb48/internal/types"
)

// MessageHandler is the function signature adapters call per incoming message.
type MessageHandler func(ctx context.Context, msg types.Message, tokens chan<- string) error

// ContextAssembler builds the LLM context for a given session turn.
// Phase 1: stubAssembler. Phase 2: full 4-tier implementation.
type ContextAssembler interface {
	Build(sess *session.Session, manager *session.SessionManager, userMessage string, coldContext string) (systemPrompt string, messages []types.LLMMessage, err error)
}

// AKB48Runtime wires all Phase 1 components together.
type AKB48Runtime struct {
	cfg            *config.Config
	llmCaller      *llm.Caller
	registry       *tools.Registry
	sessionManager *session.SessionManager
	assembler      ContextAssembler
	hooks          []session.HookFunc
}

// New creates and wires all runtime components.
func New(cfg *config.Config) (*AKB48Runtime, error) {
	registry := tools.NewRegistry()
	caller := llm.NewCaller(cfg, registry)
	sessionMgr := session.NewSessionManager(cfg.Session)

	assembler := &stubAssembler{}

	rt := &AKB48Runtime{
		cfg:            cfg,
		llmCaller:      caller,
		registry:       registry,
		sessionManager: sessionMgr,
		assembler:      assembler,
	}

	metricsPath := cfg.Session.StorageDir + "/tool-calls.jsonl"
	rt.hooks = []session.HookFunc{
		session.PersistSessionHook(sessionMgr),
		session.LogMetricsHook(metricsPath),
	}

	return rt, nil
}

// HandleMessage processes one incoming message end-to-end.
// Called by channel adapters; tokens chan receives streamed output.
func (r *AKB48Runtime) HandleMessage(ctx context.Context, msg types.Message, tokens chan<- string) error {
	start := time.Now()

	// 1. Resolve session
	sess, err := r.sessionManager.ResolveOrCreate(ctx, msg.SessionID)
	if err != nil {
		return fmt.Errorf("resolve session: %w", err)
	}

	// 2. Build context
	systemPrompt, messages, err := r.assembler.Build(sess, r.sessionManager, msg.Text, "")
	if err != nil {
		return fmt.Errorf("build context: %w", err)
	}

	// 3. Append current user message
	messages = append(messages, types.LLMMessage{Role: types.RoleUser, Content: msg.Text})

	// 4. Stable turn ID for idempotency
	turnID := uuid.NewString()

	// 5. Call LLM
	result, err := r.llmCaller.Call(ctx, llm.CallParams{
		System:   systemPrompt,
		Messages: messages,
		Tokens:   tokens,
		TurnID:   turnID,
	})
	if err != nil {
		return fmt.Errorf("llm call: %w", err)
	}

	// 6. Build turn record
	turn := session.SessionTurn{
		TurnID:            turnID,
		Timestamp:         time.Now().UTC(),
		UserMessage:       msg.Text,
		AssistantResponse: result.Text,
		TokenUsage:        result.TokenUsage,
		LatencyMs:         time.Since(start).Milliseconds(),
	}

	// 7. Commit to in-memory session
	sess.AddTurn(turn)

	// 8. Fire-and-forget hooks (persist + metrics)
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

// Stop flushes file handles.
func (r *AKB48Runtime) Stop() error {
	return r.sessionManager.Shutdown()
}

// SetAssembler replaces the context assembler (used in Phase 2 and tests).
func (r *AKB48Runtime) SetAssembler(a ContextAssembler) {
	r.assembler = a
}

// SetHooks replaces hooks (used in tests).
func (r *AKB48Runtime) SetHooks(hooks []session.HookFunc) {
	r.hooks = hooks
}

// SessionManager exposes the session manager (used in tests and adapters).
func (r *AKB48Runtime) SessionManager() *session.SessionManager {
	return r.sessionManager
}

// --- Phase 1 stub assembler ---

// stubAssembler returns empty system prompt + session history only.
// Replaced by full 4-tier assembler in Phase 2 (KLW-011).
type stubAssembler struct{}

func (s *stubAssembler) Build(
	sess *session.Session,
	manager *session.SessionManager,
	userMessage string,
	coldContext string,
) (string, []types.LLMMessage, error) {
	turns := manager.GetContextTurns(sess)
	msgs := make([]types.LLMMessage, 0, len(turns)*2)
	for _, t := range turns {
		msgs = append(msgs,
			types.LLMMessage{Role: types.RoleUser, Content: t.UserMessage},
			types.LLMMessage{Role: types.RoleAssistant, Content: t.AssistantResponse},
		)
	}
	return "", msgs, nil
}
