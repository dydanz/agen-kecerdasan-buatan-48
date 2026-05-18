package session

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/dydanz/akb48/internal/types"
)

// HookFunc is the signature for all post-turn hooks.
type HookFunc func(ctx context.Context, session *Session, turn SessionTurn) error

// RunHooks runs all hooks concurrently with a 5-second deadline.
// Errors are logged; RunHooks never returns an error.
func RunHooks(hooks []HookFunc, session *Session, turn SessionTurn) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var eg errgroup.Group
	for i, h := range hooks {
		hook := h
		idx := i
		eg.Go(func() error {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("Post-turn hook panicked",
						"hook_index", idx,
						"session_id", session.SessionID,
						"turn_id", turn.TurnID,
						"panic", r)
				}
			}()
			if err := hook(ctx, session, turn); err != nil {
				slog.Error("Post-turn hook failed",
					"hook_index", idx,
					"session_id", session.SessionID,
					"turn_id", turn.TurnID,
					"error", err)
			}
			return nil // never propagate
		})
	}
	eg.Wait() //nolint:errcheck — always nil by design
}

// PersistSessionHook returns a HookFunc that appends the turn to JSONL.
func PersistSessionHook(manager *SessionManager) HookFunc {
	return func(ctx context.Context, sess *Session, turn SessionTurn) error {
		return manager.Persist(sess, turn)
	}
}

type metricsLine struct {
	Timestamp  time.Time        `json:"timestamp"`
	SessionID  string           `json:"session_id"`
	TurnID     string           `json:"turn_id"`
	TokenUsage types.TokenUsage `json:"token_usage"`
	ToolNames  []string         `json:"tool_names,omitempty"`
	SkillUsed  string           `json:"skill_used,omitempty"`
	LatencyMs  int64            `json:"latency_ms"`
}

// LogMetricsHook returns a HookFunc that appends a metrics line to logPath.
func LogMetricsHook(logPath string) HookFunc {
	return func(ctx context.Context, sess *Session, turn SessionTurn) error {
		toolNames := make([]string, 0, len(turn.ToolCalls))
		for _, tc := range turn.ToolCalls {
			toolNames = append(toolNames, tc.Name)
		}

		m := metricsLine{
			Timestamp:  time.Now().UTC(),
			SessionID:  sess.SessionID,
			TurnID:     turn.TurnID,
			TokenUsage: turn.TokenUsage,
			ToolNames:  toolNames,
			SkillUsed:  turn.SkillUsed,
			LatencyMs:  turn.LatencyMs,
		}

		line, err := json.Marshal(m)
		if err != nil {
			return err
		}
		line = append(line, '\n')

		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = f.Write(line)
		return err
	}
}
