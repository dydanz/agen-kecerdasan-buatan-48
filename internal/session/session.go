package session

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dydanz/akb48/internal/types"
)

type ToolCallRecord struct {
	Name       string          `json:"name"`
	Input      json.RawMessage `json:"input"`
	Output     string          `json:"output"`
	DurationMs int64           `json:"duration_ms"`
}

type SessionTurn struct {
	TurnID            string           `json:"turn_id"`
	Timestamp         time.Time        `json:"timestamp"`
	UserMessage       string           `json:"user_message"`
	AssistantResponse string           `json:"assistant_response"`
	ToolCalls         []ToolCallRecord `json:"tool_calls,omitempty"`
	TokenUsage        types.TokenUsage `json:"token_usage"`
	SkillUsed         string           `json:"skill_used,omitempty"`
	LatencyMs         int64            `json:"latency_ms"`
	ToolEventCount    int              `json:"tool_event_count,omitempty"`
}

type Session struct {
	SessionID string        `json:"session_id"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	Turns     []SessionTurn `json:"turns"`
	mu        sync.RWMutex
}

type sessionCreatedEvent struct {
	Event     string    `json:"event"`
	SessionID string    `json:"session_id"`
	CreatedAt time.Time `json:"created_at"`
}

func NewSession(sessionID string) *Session {
	now := time.Now().UTC()
	return &Session{
		SessionID: sessionID,
		CreatedAt: now,
		UpdatedAt: now,
		Turns:     []SessionTurn{},
	}
}

func (s *Session) AddTurn(turn SessionTurn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Turns = append(s.Turns, turn)
	s.UpdatedAt = time.Now().UTC()
}

func (s *Session) TurnCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Turns)
}

func (s *Session) IsCold(threshold time.Duration) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Turns) == 0 || time.Since(s.UpdatedAt) > threshold
}

// SanitizeID replaces ":" with "_" for filesystem safety.
func SanitizeID(sessionID string) string {
	return strings.ReplaceAll(sessionID, ":", "_")
}

// Persist appends one turn to the session's JSONL file.
// File opened with O_APPEND|O_CREATE|O_WRONLY — never rewrites.
func Persist(filePath string, turn SessionTurn) error {
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	line, err := json.Marshal(turn)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	_, err = f.Write(line)
	return err
}

// PersistCreated writes the session_created header line.
func PersistCreated(filePath string, s *Session) error {
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	evt := sessionCreatedEvent{
		Event:     "session_created",
		SessionID: s.SessionID,
		CreatedAt: s.CreatedAt,
	}
	line, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	_, err = f.Write(line)
	return err
}

// LoadFromDisk reads a JSONL file, skips corrupt lines with slog.Warn.
// Returns a Session with all valid turns loaded.
func LoadFromDisk(filePath string) (*Session, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sess := &Session{Turns: []SessionTurn{}}
	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}

		// Try header event first
		var evt sessionCreatedEvent
		if json.Unmarshal(raw, &evt) == nil && evt.Event == "session_created" {
			sess.SessionID = evt.SessionID
			sess.CreatedAt = evt.CreatedAt
			continue
		}

		// Try compaction record — replaces turns_replaced oldest turns with a summary turn
		var compRec compactionRecord
		if json.Unmarshal(raw, &compRec) == nil && compRec.Type == "compaction" {
			summaryTurn := SessionTurn{
				TurnID:            "compaction-summary",
				Timestamp:         compRec.Timestamp,
				UserMessage:       "[compacted]",
				AssistantResponse: compRec.Summary,
			}
			replace := compRec.TurnsReplaced
			if replace > len(sess.Turns) {
				replace = len(sess.Turns)
			}
			sess.Turns = append([]SessionTurn{summaryTurn}, sess.Turns[replace:]...)
			continue
		}

		// Try turn
		var turn SessionTurn
		if err := json.Unmarshal(raw, &turn); err != nil {
			slog.Warn("Session load: skipping corrupt line",
				"file", filePath, "line", lineNum, "error", err)
			continue
		}
		sess.Turns = append(sess.Turns, turn)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if len(sess.Turns) > 0 {
		sess.UpdatedAt = sess.Turns[len(sess.Turns)-1].Timestamp
	} else if !sess.CreatedAt.IsZero() {
		sess.UpdatedAt = sess.CreatedAt
	}

	return sess, nil
}
