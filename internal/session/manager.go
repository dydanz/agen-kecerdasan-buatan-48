package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dydanz/akb48/internal/config"
)

type SessionManager struct {
	storageDir  string
	sessions    sync.Map // map[string]*Session
	fileHandles sync.Map // map[string]*os.File
	cfg         config.SessionConfig
}

func NewSessionManager(cfg config.SessionConfig) *SessionManager {
	return &SessionManager{
		storageDir: cfg.StorageDir,
		cfg:        cfg,
	}
}

// ResolveOrCreate returns an existing session or creates a new one.
// Race-safe via sync.Map.LoadOrStore.
func (m *SessionManager) ResolveOrCreate(ctx context.Context, sessionID string) (*Session, error) {
	if sess, ok := m.sessions.Load(sessionID); ok {
		return sess.(*Session), nil
	}

	// Try load from disk first
	path := m.filePath(sessionID)
	var newSess *Session
	if _, err := os.Stat(path); err == nil {
		loaded, err := LoadFromDisk(path)
		if err != nil {
			slog.Warn("Failed to load session from disk; starting fresh",
				"session_id", sessionID, "error", err)
			loaded = NewSession(sessionID)
		}
		loaded.SessionID = sessionID
		newSess = loaded
	} else {
		newSess = NewSession(sessionID)
		if err := os.MkdirAll(m.storageDir, 0o700); err != nil {
			return nil, fmt.Errorf("create storage dir: %w", err)
		}
		if err := PersistCreated(path, newSess); err != nil {
			return nil, fmt.Errorf("persist session created: %w", err)
		}
	}

	actual, loaded := m.sessions.LoadOrStore(sessionID, newSess)
	if loaded {
		// Another goroutine beat us; use theirs
		return actual.(*Session), nil
	}
	return newSess, nil
}

// GetContextTurns returns turns for the LLM context window.
// Respects MaxTurnsInContext and MaxContextTokens (oldest dropped first).
func (m *SessionManager) GetContextTurns(sess *Session) []SessionTurn {
	sess.mu.RLock()
	defer sess.mu.RUnlock()

	turns := make([]SessionTurn, len(sess.Turns))
	copy(turns, sess.Turns)

	if m.cfg.MaxTurnsInContext > 0 && len(turns) > m.cfg.MaxTurnsInContext {
		turns = turns[len(turns)-m.cfg.MaxTurnsInContext:]
	}

	if m.cfg.MaxContextTokens > 0 {
		tokenCount := 0
		cutoff := 0
		for i := len(turns) - 1; i >= 0; i-- {
			raw, _ := json.Marshal(turns[i])
			tokenCount += len(raw) / 4
			if tokenCount > m.cfg.MaxContextTokens {
				cutoff = i + 1
				break
			}
		}
		turns = turns[cutoff:]
	}

	return turns
}

// appendCompactionRecord appends a compaction record to the session JSONL file.
func (m *SessionManager) appendCompactionRecord(sessionID string, rec compactionRecord) error {
	path := m.filePath(sessionID)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	_, err = f.Write(line)
	return err
}

// Persist appends one turn to the session JSONL file.
func (m *SessionManager) Persist(sess *Session, turn SessionTurn) error {
	path := m.filePath(sess.SessionID)

	fRaw, _ := m.fileHandles.LoadOrStore(sess.SessionID, (*os.File)(nil))
	f, _ := fRaw.(*os.File)

	if f == nil {
		var err error
		f, err = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("open session file: %w", err)
		}
		m.fileHandles.Store(sess.SessionID, f)
	}

	line, err := json.Marshal(turn)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	_, err = f.Write(line)

	if err == nil {
		m.warnIfLarge(sess.SessionID, path)
	}

	return err
}

// LoadAll scans storageDir for .jsonl files and loads each session at startup.
func (m *SessionManager) LoadAll(ctx context.Context) error {
	if err := os.MkdirAll(m.storageDir, 0o700); err != nil {
		return fmt.Errorf("create storage dir: %w", err)
	}

	entries, err := os.ReadDir(m.storageDir)
	if err != nil {
		return fmt.Errorf("read storage dir: %w", err)
	}

	sessionCount := 0
	totalTurns := 0

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		path := filepath.Join(m.storageDir, entry.Name())
		sess, err := LoadFromDisk(path)
		if err != nil {
			slog.Warn("Failed to load session file", "path", path, "error", err)
			continue
		}

		// Reconstruct session ID from filename (reverse sanitize)
		sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")
		if sess.SessionID == "" {
			sess.SessionID = sessionID
		}
		m.sessions.Store(sess.SessionID, sess)
		sessionCount++
		totalTurns += len(sess.Turns)
	}

	slog.Info("Sessions loaded", "count", sessionCount, "total_turns", totalTurns)
	return nil
}

// IsCold returns true if the given session is cold (new or resumed after threshold).
func (m *SessionManager) IsCold(sess *Session) bool {
	threshold := time.Duration(m.cfg.ColdResumeThresholdMinutes) * time.Minute
	return sess.IsCold(threshold)
}

// Shutdown closes all open file handles.
func (m *SessionManager) Shutdown() error {
	var firstErr error
	m.fileHandles.Range(func(k, v any) bool {
		f, ok := v.(*os.File)
		if !ok || f == nil {
			return true
		}
		if err := f.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		return true
	})
	return firstErr
}

func (m *SessionManager) filePath(sessionID string) string {
	return filepath.Join(m.storageDir, SanitizeID(sessionID)+".jsonl")
}

func (m *SessionManager) warnIfLarge(sessionID, path string) {
	maxBytes := int64(m.cfg.MaxFileSizeMB) * 1024 * 1024
	if maxBytes <= 0 {
		return
	}
	fi, err := os.Stat(path)
	if err == nil && fi.Size() > maxBytes {
		slog.Warn("Session file exceeds size limit",
			"session_id", sessionID,
			"size_mb", fi.Size()/1024/1024,
			"limit_mb", m.cfg.MaxFileSizeMB)
	}
}
