package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dydanz/akb48/internal/config"
)

// BrainWriter stores extracted entities in the memory brain.
// Defined here to avoid import cycles — brain imports tools; session imports neither.
type BrainWriter interface {
	CreateEntities(ctx context.Context, entitiesJSON json.RawMessage) error
	Available() bool
}

// Extractor extracts entities and generates a summary from a block of old turns.
// Returns entities as raw JSON array, a summary string, and any error.
// Implementations must be tolerant: on failure return empty entities + fallback summary.
type Extractor func(ctx context.Context, turns []SessionTurn) (entitiesJSON json.RawMessage, summary string, err error)

// compactionRecord is the JSONL line appended on every compaction.
type compactionRecord struct {
	Type          string    `json:"type"` // always "compaction"
	Timestamp     time.Time `json:"ts"`
	TurnsReplaced int       `json:"turns_replaced"`
	EntityCount   int       `json:"entity_count"`
	Summary       string    `json:"summary"`
}

// compactionFlags tracks per-session in-progress state.
// Values are *int32 used with atomic CAS (0=idle, 1=running).
var compactionFlags syncMap

type syncMap struct {
	m atomic.Value // stores map[string]*int32
}

func (s *syncMap) flag(sessionID string) *int32 {
	// Fast path: already in map
	if m, ok := s.m.Load().(map[string]*int32); ok {
		if v, ok := m[sessionID]; ok {
			return v
		}
	}
	// Slow path: add entry
	for {
		old, _ := s.m.Load().(map[string]*int32)
		newM := make(map[string]*int32, len(old)+1)
		for k, v := range old {
			newM[k] = v
		}
		if _, exists := newM[sessionID]; !exists {
			newM[sessionID] = new(int32)
		}
		if s.m.CompareAndSwap(old, newM) {
			return newM[sessionID]
		}
	}
}

// CompactionHook returns a post-turn hook that compacts the session when it exceeds
// cfg.MaxTurnsBeforeCompaction. Runs in a goroutine — never blocks the response path.
// brain and extractor may be nil; compaction is skipped gracefully in that case.
func CompactionHook(cfg config.SessionConfig, mgr *SessionManager, brain BrainWriter, extractor Extractor) HookFunc {
	return func(ctx context.Context, sess *Session, _ SessionTurn) error {
		if cfg.MaxTurnsBeforeCompaction <= 0 {
			return nil
		}
		if sess.TurnCount() < cfg.MaxTurnsBeforeCompaction {
			return nil
		}

		flag := compactionFlags.flag(sess.SessionID)
		if !atomic.CompareAndSwapInt32(flag, 0, 1) {
			return nil // already running
		}

		go func() {
			defer atomic.StoreInt32(flag, 0)
			runCompaction(context.Background(), sess, mgr, brain, extractor, cfg)
		}()
		return nil
	}
}

func runCompaction(ctx context.Context, sess *Session, mgr *SessionManager, brain BrainWriter, extractor Extractor, cfg config.SessionConfig) {
	sess.mu.Lock()
	n := len(sess.Turns)
	keep := cfg.MaxTurnsInContext
	if keep <= 0 {
		keep = 50
	}
	if n <= keep {
		sess.mu.Unlock()
		return
	}

	oldTurns := make([]SessionTurn, n-keep)
	copy(oldTurns, sess.Turns[:n-keep])
	sess.mu.Unlock() // unlock before LLM calls

	// Extract entities + generate summary (extractor may be nil → fallback)
	var entitiesJSON json.RawMessage
	var summary string
	entityCount := 0

	if extractor != nil {
		var err error
		entitiesJSON, summary, err = extractor(ctx, oldTurns)
		if err != nil {
			slog.Warn("compaction: extraction failed", "error", err, "session", sess.SessionID)
		}
	}

	if len(entitiesJSON) > 0 {
		var arr []any
		if json.Unmarshal(entitiesJSON, &arr) == nil {
			entityCount = len(arr)
		}
	}

	// Write entities to brain (skip if brain down or no entities)
	if brain != nil && brain.Available() && entityCount > 0 {
		if err := brain.CreateEntities(ctx, entitiesJSON); err != nil {
			slog.Warn("compaction: brain write failed", "error", err, "session", sess.SessionID)
		}
	}

	if summary == "" {
		summary = fmt.Sprintf("Compacted %d turns.", len(oldTurns))
	}

	summaryTurn := SessionTurn{
		TurnID:            "compaction-" + time.Now().UTC().Format("20060102T150405Z"),
		Timestamp:         time.Now().UTC(),
		UserMessage:       "[compacted]",
		AssistantResponse: summary,
	}

	// Replace old turns in-memory
	sess.mu.Lock()
	remaining := make([]SessionTurn, len(sess.Turns[n-keep:]))
	copy(remaining, sess.Turns[n-keep:])
	sess.Turns = append([]SessionTurn{summaryTurn}, remaining...)
	sess.mu.Unlock()

	// Append compaction record to JSONL (append-only — never rewrites)
	rec := compactionRecord{
		Type:          "compaction",
		Timestamp:     time.Now().UTC(),
		TurnsReplaced: len(oldTurns),
		EntityCount:   entityCount,
		Summary:       summary,
	}
	if err := mgr.appendCompactionRecord(sess.SessionID, rec); err != nil {
		slog.Warn("compaction: JSONL append failed", "error", err)
	}

	slog.Info("session: compaction triggered",
		"session_id", sess.SessionID,
		"turns_replaced", len(oldTurns),
		"entity_count", entityCount)
}

// ParseEntitiesJSON extracts a JSON array from a raw LLM response string.
// Returns nil if no valid array is found — callers must tolerate nil.
func ParseEntitiesJSON(raw string) json.RawMessage {
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end <= start {
		return nil
	}
	candidate := []byte(raw[start : end+1])
	var arr []any
	if json.Unmarshal(candidate, &arr) != nil {
		return nil
	}
	return candidate
}
