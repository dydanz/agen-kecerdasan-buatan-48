package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dydanz/akb48/internal/env"
	"github.com/dydanz/akb48/internal/tools"
)

const braveSearchURL = "https://api.search.brave.com/res/v1/web/search"

type input struct {
	Query string `json:"query"`
	Count int    `json:"count"`
}

var definition = tools.ToolDefinition{
	Name:        "web_search",
	Description: "Search the web for current information. Use when you need real-time data, recent events, docs, or anything that may have changed since training cutoff (Aug 2025).",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "Search query. Be specific. E.g. 'golang 1.23 release notes' not 'go new features'"
			},
			"count": {
				"type": "integer",
				"description": "Number of results to return (1-10, default 5)"
			}
		},
		"required": ["query"]
	}`),
}

// NewHandler returns the ToolDefinition and ToolHandler for web_search.
// er gates env reads to keys declared in .env; maxResultTokens caps response size.
func NewHandler(er *env.Reader, maxResultTokens int) (tools.ToolDefinition, tools.ToolHandler) {
	client := &http.Client{Timeout: 10 * time.Second}
	return definition, makeHandler(er, maxResultTokens, client)
}

// newHandlerWithClient injects a custom HTTP client for tests.
func newHandlerWithClient(er *env.Reader, maxResultTokens int, client *http.Client) (tools.ToolDefinition, tools.ToolHandler) {
	return definition, makeHandler(er, maxResultTokens, client)
}

func makeHandler(er *env.Reader, maxResultTokens int, client *http.Client) tools.ToolHandler {
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		var in input
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", fmt.Errorf("web_search: invalid input: %w", err)
		}
		if in.Query == "" {
			return "", fmt.Errorf("web_search: query is required")
		}
		if in.Count <= 0 || in.Count > 10 {
			in.Count = 5
		}

		apiKey := er.Get("BRAVE_SEARCH_API_KEY")
		if apiKey == "" {
			return "", fmt.Errorf("web_search: BRAVE_SEARCH_API_KEY not set or not declared in .env")
		}

		reqURL := fmt.Sprintf("%s?q=%s&count=%d", braveSearchURL, url.QueryEscape(in.Query), in.Count)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return "", fmt.Errorf("web_search: build request: %w", err)
		}
		req.Header.Set("X-Subscription-Token", apiKey)
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("web_search: request: %w", err)
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("web_search: read response: %w", err)
		}

		if resp.StatusCode == 429 {
			return "", fmt.Errorf("web_search: rate limited — try again in a moment")
		}
		if resp.StatusCode >= 400 {
			return "", fmt.Errorf("web_search: HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
		}

		result := formatResults(body)
		if result == "" {
			return fmt.Sprintf("web_search: no results found for query: %s", in.Query), nil
		}

		// Truncate: ~4 chars/token
		if maxChars := maxResultTokens * 4; len(result) > maxChars {
			result = result[:maxChars] + "\n[truncated]"
		}
		return result, nil
	}
}

type braveResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}

func formatResults(body []byte) string {
	var resp braveResponse
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Web.Results) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, r := range resp.Web.Results {
		fmt.Fprintf(&sb, "[%d] %s\nURL: %s\nSnippet: %s\n\n", i+1, r.Title, r.URL, r.Description)
	}
	return strings.TrimRight(sb.String(), "\n")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
