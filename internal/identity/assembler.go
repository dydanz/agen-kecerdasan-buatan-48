package identity

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"github.com/dydanz/akb48/internal/config"
	"github.com/dydanz/akb48/internal/session"
	"github.com/dydanz/akb48/internal/skills"
)

// ContextAssembler implements runtime.ContextAssembler with 4-tier caching.
//
// Tier 1 [CACHED]   — System prompt: AGENTS.md + SOUL.md + USER.md + Skill body
// Tier 2 [CACHED]   — Session history: all turns except last 2
// Tier 3 [NO CACHE] — Cold opener context (once per cold session)
// Tier 4 [NO CACHE] — Live turn: last 2 turns + current user message
type ContextAssembler struct {
	agentsContent string
	soulContent   string
	userContent   string
	resolver      *skills.Resolver
	manager       *session.SessionManager
	cfg           config.IdentityConfig
}

// New creates a ContextAssembler. Call Load() before first use.
func New(cfg config.IdentityConfig, resolver *skills.Resolver, manager *session.SessionManager) *ContextAssembler {
	return &ContextAssembler{
		cfg:      cfg,
		resolver: resolver,
		manager:  manager,
	}
}

// Load reads identity files from disk. AGENTS.md and SOUL.md are required.
// USER.md is optional — missing USER.md logs a warning only.
func (a *ContextAssembler) Load() error {
	agentsPath := filepath.Join(a.cfg.Dir, "AGENTS.md")
	agentsData, err := os.ReadFile(agentsPath)
	if err != nil {
		return fmt.Errorf("AGENTS.md missing or unreadable: %w", err)
	}
	a.agentsContent = string(agentsData)

	soulPath := filepath.Join(a.cfg.Dir, "SOUL.md")
	soulData, err := os.ReadFile(soulPath)
	if err != nil {
		return fmt.Errorf("SOUL.md missing or unreadable: %w", err)
	}
	a.soulContent = string(soulData)

	userPath := filepath.Join(a.cfg.Dir, "USER.md")
	userData, err := os.ReadFile(userPath)
	if err != nil {
		slog.Warn("USER.md not found — operator context not loaded", "path", userPath)
	} else {
		a.userContent = string(userData)
	}

	return nil
}

// Build assembles system prompt + messages for one LLM call.
// coldContext is injected as Tier 3 if non-empty.
func (a *ContextAssembler) Build(
	sess *session.Session,
	manager *session.SessionManager,
	userMessage string,
	coldContext string,
) (string, []anthropic.MessageParam, error) {
	// --- Tier 1: System prompt (marked cached at LLM call site) ---
	systemParts := []string{a.agentsContent, a.soulContent}
	if a.userContent != "" {
		systemParts = append(systemParts, a.userContent)
	}

	if a.resolver != nil {
		skill, err := a.resolver.Resolve(userMessage)
		if err != nil {
			slog.Warn("Skill resolution error", "error", err)
		} else if skill != nil {
			systemParts = append(systemParts, skill.Body)
		}
	}
	systemPrompt := strings.Join(systemParts, "\n\n---\n\n")

	// --- Tier 2: Session history (all turns except last 2) ---
	contextTurns := manager.GetContextTurns(sess)

	var tier2Turns, tier4Turns []session.SessionTurn
	if len(contextTurns) > 2 {
		tier2Turns = contextTurns[:len(contextTurns)-2]
		tier4Turns = contextTurns[len(contextTurns)-2:]
	} else {
		tier4Turns = contextTurns
	}

	tier2Msgs := turnsToMessages(tier2Turns)
	// Mark last Tier 2 message with cache_control to cache prefix up to this point.
	if len(tier2Msgs) > 0 {
		markCached(&tier2Msgs[len(tier2Msgs)-1])
	}

	var allMsgs []anthropic.MessageParam
	allMsgs = append(allMsgs, tier2Msgs...)

	// --- Tier 3: Cold opener context (no cache) ---
	if coldContext != "" {
		allMsgs = append(allMsgs,
			anthropic.NewUserMessage(
				anthropic.NewTextBlock("[What I recall that may be relevant]\n"+coldContext),
			),
			anthropic.NewAssistantMessage(
				anthropic.NewTextBlock("Understood. I have that context."),
			),
		)
	}

	// --- Tier 4: Recent 2 turns + current message (no cache) ---
	allMsgs = append(allMsgs, turnsToMessages(tier4Turns)...)

	return systemPrompt, allMsgs, nil
}

// turnsToMessages converts session turns to anthropic message pairs.
func turnsToMessages(turns []session.SessionTurn) []anthropic.MessageParam {
	msgs := make([]anthropic.MessageParam, 0, len(turns)*2)
	for _, t := range turns {
		msgs = append(msgs,
			anthropic.NewUserMessage(anthropic.NewTextBlock(t.UserMessage)),
			anthropic.NewAssistantMessage(anthropic.NewTextBlock(t.AssistantResponse)),
		)
	}
	return msgs
}

// markCached sets cache_control: ephemeral on the last content block of a message.
func markCached(msg *anthropic.MessageParam) {
	if len(msg.Content) == 0 {
		return
	}
	last := &msg.Content[len(msg.Content)-1]
	if last.OfText != nil {
		last.OfText.CacheControl = anthropic.NewCacheControlEphemeralParam()
	}
}
