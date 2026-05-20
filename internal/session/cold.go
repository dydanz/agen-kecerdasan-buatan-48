package session

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// BrainSearcher is the interface cold opener uses to query the knowledge brain.
// Satisfied by GBrainBridge in Phase 3; use a stub in Phase 2 tests.
type BrainSearcher interface {
	SearchEntities(ctx context.Context, query string, limit int) (string, error)
}

// ColdOpener injects relevant brain context at the start of a cold session.
type ColdOpener struct {
	searcher  BrainSearcher
	threshold time.Duration
}

// NewColdOpener creates a ColdOpener with the given threshold in minutes.
func NewColdOpener(searcher BrainSearcher, thresholdMinutes int) *ColdOpener {
	return &ColdOpener{
		searcher:  searcher,
		threshold: time.Duration(thresholdMinutes) * time.Minute,
	}
}

// BuildColdContext returns formatted recall context for a cold session.
// Returns "" if the session is warm, brain is unavailable, or search returns nothing.
func (c *ColdOpener) BuildColdContext(ctx context.Context, sess *Session, message string) string {
	if !sess.IsCold(c.threshold) {
		return ""
	}

	query := extractQuery(message)
	if query == "" {
		return ""
	}

	results, err := c.searcher.SearchEntities(ctx, query, 3)
	if err != nil {
		slog.Debug("Cold opener: brain search failed", "query", query, "error", err)
		return ""
	}
	if results == "" {
		slog.Debug("Cold opener: brain search returned nothing", "query", query)
		return ""
	}

	return results
}

// stopWords are filtered from the search query.
var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "are": true,
	"was": true, "were": true, "what": true, "how": true, "can": true,
	"i": true, "do": true, "my": true, "me": true, "we": true,
	"our": true, "it": true, "in": true, "on": true, "at": true,
	"for": true, "to": true, "of": true, "and": true, "or": true,
	"you": true, "this": true, "that": true, "with": true, "about": true,
	"tell": true, "know": true, "any": true, "has": true, "have": true,
}

// extractQuery strips stop words and returns the first 12 meaningful words.
func extractQuery(message string) string {
	words := strings.Fields(strings.ToLower(message))
	filtered := make([]string, 0, 12)
	for _, w := range words {
		// Strip trailing punctuation
		w = strings.TrimRight(w, "?.,!;:")
		if w == "" || stopWords[w] {
			continue
		}
		filtered = append(filtered, w)
		if len(filtered) == 12 {
			break
		}
	}
	return strings.Join(filtered, " ")
}
