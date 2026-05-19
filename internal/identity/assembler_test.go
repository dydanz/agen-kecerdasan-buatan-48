package identity

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dydanz/akb48/internal/config"
	"github.com/dydanz/akb48/internal/session"
	"github.com/dydanz/akb48/internal/skills"
	"github.com/dydanz/akb48/internal/types"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testManager(t *testing.T) (*session.SessionManager, string) {
	t.Helper()
	dir := t.TempDir()
	mgr := session.NewSessionManager(config.SessionConfig{
		StorageDir:                 dir,
		MaxTurnsInContext:          50,
		MaxContextTokens:           32000,
		ColdResumeThresholdMinutes: 30,
	})
	return mgr, dir
}

func addTurns(sess *session.Session, n int) {
	for i := range n {
		sess.AddTurn(session.SessionTurn{
			TurnID:            string(rune('a' + i)),
			Timestamp:         time.Now().UTC(),
			UserMessage:       "msg",
			AssistantResponse: "resp",
			TokenUsage:        types.TokenUsage{},
		})
	}
}

func TestLoad_MissingAgentsMD(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "SOUL.md", "soul content")

	a := New(config.IdentityConfig{Dir: dir}, nil, nil)
	if err := a.Load(); err == nil {
		t.Error("expected error when AGENTS.md missing")
	}
}

func TestLoad_MissingSoulMD(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "AGENTS.md", "agents content")

	a := New(config.IdentityConfig{Dir: dir}, nil, nil)
	if err := a.Load(); err == nil {
		t.Error("expected error when SOUL.md missing")
	}
}

func TestLoad_MissingUserMD_WarningOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "AGENTS.md", "agents")
	writeFile(t, dir, "SOUL.md", "soul")
	// No USER.md

	a := New(config.IdentityConfig{Dir: dir}, nil, nil)
	if err := a.Load(); err != nil {
		t.Errorf("expected nil error when USER.md missing, got: %v", err)
	}
}

func TestLoad_AllFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "AGENTS.md", "agents content")
	writeFile(t, dir, "SOUL.md", "soul content")
	writeFile(t, dir, "USER.md", "user content")

	a := New(config.IdentityConfig{Dir: dir}, nil, nil)
	if err := a.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if a.agentsContent != "agents content" {
		t.Error("AGENTS.md content not loaded")
	}
	if a.soulContent != "soul content" {
		t.Error("SOUL.md content not loaded")
	}
	if a.userContent != "user content" {
		t.Error("USER.md content not loaded")
	}
}

func TestBuild_SystemPromptContainsIdentity(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "AGENTS.md", "AGENTS CONTENT")
	writeFile(t, dir, "SOUL.md", "SOUL CONTENT")
	writeFile(t, dir, "USER.md", "USER CONTENT")

	mgr, _ := testManager(t)
	a := New(config.IdentityConfig{Dir: dir}, nil, mgr)
	if err := a.Load(); err != nil {
		t.Fatal(err)
	}

	sess, _ := mgr.ResolveOrCreate(context.Background(), "test")
	systemPrompt, _, err := a.Build(sess, mgr, "hello", "")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"AGENTS CONTENT", "SOUL CONTENT", "USER CONTENT"} {
		if !strings.Contains(systemPrompt, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
}

func TestBuild_TierStructure_10Turns(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "AGENTS.md", "agents")
	writeFile(t, dir, "SOUL.md", "soul")

	mgr, _ := testManager(t)
	a := New(config.IdentityConfig{Dir: dir}, nil, mgr)
	a.Load()

	sess, _ := mgr.ResolveOrCreate(context.Background(), "test")
	addTurns(sess, 10)

	_, msgs, err := a.Build(sess, mgr, "new question", "")
	if err != nil {
		t.Fatal(err)
	}

	// 10 turns × 2 msgs = 20 total
	// Tier 2: 8 turns × 2 = 16 msgs (turns 0-7)
	// Tier 4: 2 turns × 2 = 4 msgs (turns 8-9)
	// Total: 20 (current user msg added in runtime, not here)
	if len(msgs) != 20 {
		t.Errorf("expected 20 messages for 10-turn session, got %d", len(msgs))
	}

	// Last Tier 2 message (index 15, assistant from turn 7) should be cached
	tier2LastIdx := 15
	if tier2LastIdx < len(msgs) {
		last := msgs[tier2LastIdx]
		if len(last.Content) > 0 && last.Content[0].OfText != nil {
			cc := last.Content[0].OfText.CacheControl
			if cc.Type != "ephemeral" {
				t.Errorf("expected cache_control ephemeral on last Tier 2 message, got %q", cc.Type)
			}
		}
	}
}

func TestBuild_WithColdContext(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "AGENTS.md", "agents")
	writeFile(t, dir, "SOUL.md", "soul")

	mgr, _ := testManager(t)
	a := New(config.IdentityConfig{Dir: dir}, nil, mgr)
	a.Load()

	sess, _ := mgr.ResolveOrCreate(context.Background(), "test")
	// Empty session (cold), cold context provided
	coldCtx := "- staging cluster: ap-southeast-1 (decision)"
	_, msgs, err := a.Build(sess, mgr, "what is staging?", coldCtx)
	if err != nil {
		t.Fatal(err)
	}

	// Should have 2 cold context messages (user + assistant pair)
	if len(msgs) < 2 {
		t.Fatalf("expected cold context messages, got %d", len(msgs))
	}
	firstContent := ""
	if msgs[0].Content[0].OfText != nil {
		firstContent = msgs[0].Content[0].OfText.Text
	}
	if !strings.Contains(firstContent, "What I recall") {
		t.Errorf("expected cold context marker in first message, got %q", firstContent)
	}
	if !strings.Contains(firstContent, "ap-southeast-1") {
		t.Errorf("expected cold context content in first message, got %q", firstContent)
	}
}

func TestBuild_SkillInjected(t *testing.T) {
	// Identity dir
	identDir := t.TempDir()
	writeFile(t, identDir, "AGENTS.md", "agents")
	writeFile(t, identDir, "SOUL.md", "soul")

	// Skills dir with note-capture
	skillsDir := t.TempDir()
	ncDir := filepath.Join(skillsDir, "note-capture")
	os.MkdirAll(ncDir, 0o755)
	os.WriteFile(filepath.Join(ncDir, "SKILL.md"), []byte(`---
name: note-capture
description: capture facts
triggers:
  - remember
---
NOTE CAPTURE BODY`), 0o644)

	resolver := skills.NewResolver(skillsDir)
	mgr, _ := testManager(t)
	a := New(config.IdentityConfig{Dir: identDir}, resolver, mgr)
	a.Load()

	sess, _ := mgr.ResolveOrCreate(context.Background(), "test")
	systemPrompt, _, err := a.Build(sess, mgr, "remember that X", "")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(systemPrompt, "NOTE CAPTURE BODY") {
		t.Errorf("expected skill body in system prompt, got:\n%s", systemPrompt)
	}
}

func TestBuild_NoColdContext_WarmSession(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "AGENTS.md", "agents")
	writeFile(t, dir, "SOUL.md", "soul")

	mgr, _ := testManager(t)
	a := New(config.IdentityConfig{Dir: dir}, nil, mgr)
	a.Load()

	sess, _ := mgr.ResolveOrCreate(context.Background(), "test")
	addTurns(sess, 3)

	// Empty cold context → no Tier 3 injected
	_, msgs, err := a.Build(sess, mgr, "hello", "")
	if err != nil {
		t.Fatal(err)
	}

	// 3 turns × 2 msgs = 6 (Tier 2: 1 turn = 2 msgs, Tier 4: 2 turns = 4 msgs)
	for _, msg := range msgs {
		if len(msg.Content) > 0 && msg.Content[0].OfText != nil {
			if strings.Contains(msg.Content[0].OfText.Text, "What I recall") {
				t.Error("cold context should not appear for empty coldContext string")
			}
		}
	}
}
