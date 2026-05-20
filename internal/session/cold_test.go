package session

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type mockBrainSearcher struct {
	result string
	err    error
	calls  []string
}

func (m *mockBrainSearcher) SearchEntities(_ context.Context, query string, _ int) (string, error) {
	m.calls = append(m.calls, query)
	return m.result, m.err
}

func TestBuildColdContext_ColdSession(t *testing.T) {
	searcher := &mockBrainSearcher{result: "- staging is ap-southeast-1 (decision)"}
	opener := NewColdOpener(searcher, 30)
	sess := NewSession("test")

	result := opener.BuildColdContext(context.Background(), sess, "what do you know about staging?")

	if result != "- staging is ap-southeast-1 (decision)" {
		t.Errorf("unexpected result: %q", result)
	}
	if len(searcher.calls) != 1 {
		t.Errorf("expected 1 search call, got %d", len(searcher.calls))
	}
}

func TestBuildColdContext_WarmSession(t *testing.T) {
	searcher := &mockBrainSearcher{result: "some result"}
	opener := NewColdOpener(searcher, 30)
	sess := NewSession("test")

	// Add a recent turn — session becomes warm
	sess.AddTurn(SessionTurn{
		TurnID:    "t1",
		Timestamp: time.Now().UTC(),
	})

	result := opener.BuildColdContext(context.Background(), sess, "hello")

	if result != "" {
		t.Errorf("expected empty result for warm session, got %q", result)
	}
	if len(searcher.calls) != 0 {
		t.Error("SearchEntities should not be called for warm session")
	}
}

func TestBuildColdContext_BrainError(t *testing.T) {
	searcher := &mockBrainSearcher{err: errors.New("brain disconnected")}
	opener := NewColdOpener(searcher, 30)
	sess := NewSession("test")

	result := opener.BuildColdContext(context.Background(), sess, "hello")

	if result != "" {
		t.Errorf("expected empty result on brain error, got %q", result)
	}
}

func TestBuildColdContext_EmptyBrainResult(t *testing.T) {
	searcher := &mockBrainSearcher{result: ""}
	opener := NewColdOpener(searcher, 30)
	sess := NewSession("test")

	result := opener.BuildColdContext(context.Background(), sess, "anything")
	if result != "" {
		t.Errorf("expected empty result when brain returns nothing, got %q", result)
	}
}

func TestBuildColdContext_ResumedAfterThreshold(t *testing.T) {
	searcher := &mockBrainSearcher{result: "- some fact"}
	opener := NewColdOpener(searcher, 30)
	sess := NewSession("test")

	// Add a turn but backdate it by 31 minutes
	sess.AddTurn(SessionTurn{
		TurnID:    "old",
		Timestamp: time.Now().Add(-31 * time.Minute).UTC(),
	})
	// Manually set UpdatedAt to simulate old resume
	sess.mu.Lock()
	sess.UpdatedAt = time.Now().Add(-31 * time.Minute)
	sess.mu.Unlock()

	result := opener.BuildColdContext(context.Background(), sess, "what was I working on?")
	if result == "" {
		t.Error("expected cold context after 31-min gap")
	}
	if len(searcher.calls) != 1 {
		t.Error("expected search to be called after threshold exceeded")
	}
}

func TestExtractQuery_StopWordsRemoved(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"What do you know about our staging cluster?", "staging cluster"},
		{"remember that we chose postgres", "remember chose postgres"},
		{"How can I deploy this to production?", "deploy production"},
		{"tell me about the current deployment", "current deployment"},
	}
	for _, c := range cases {
		got := extractQuery(c.input)
		if got != c.want {
			t.Errorf("extractQuery(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestExtractQuery_MaxWords(t *testing.T) {
	// 20 non-stop words — should cap at 12
	msg := "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron pi rho sigma tau upsilon"
	got := extractQuery(msg)
	words := len(splitWords(got))
	if words > 12 {
		t.Errorf("extractQuery returned %d words, expected <= 12", words)
	}
}

func TestExtractQuery_EmptyMessage(t *testing.T) {
	got := extractQuery("")
	if got != "" {
		t.Errorf("expected empty result, got %q", got)
	}
}

func splitWords(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}
