package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dydanz/akb48/internal/env"
	"github.com/dydanz/akb48/internal/tools"
)

const baseURL = "https://api.github.com"

type input struct {
	Account  string `json:"account"`
	Method   string `json:"method"`
	Endpoint string `json:"endpoint"`
	Body     string `json:"body"`
}

var definition = tools.ToolDefinition{
	Name:        "github_api",
	Description: "Make GitHub API requests. Use account=personal for dydanz repos (read+write), account=office for office org (read-only).",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"account":  {"type": "string", "enum": ["personal", "office"], "description": "personal = dydanz (read+write), office = office org (read-only)"},
			"method":   {"type": "string", "enum": ["GET", "POST", "PATCH", "DELETE"]},
			"endpoint": {"type": "string", "description": "GitHub API path, e.g. /repos/dydanz/akb48/issues"},
			"body":     {"type": "string", "description": "JSON body for POST/PATCH; omit for GET/DELETE"}
		},
		"required": ["account", "method", "endpoint"]
	}`),
}

// NewHandler returns the ToolDefinition and ToolHandler for github_api.
// er gates env reads to keys declared in .env; maxResultTokens caps response size.
func NewHandler(er *env.Reader, maxResultTokens int) (tools.ToolDefinition, tools.ToolHandler) {
	client := &http.Client{Timeout: 30 * time.Second}
	return definition, makeHandler(er, maxResultTokens, client)
}

// newHandlerWithClient is used by tests to inject a custom HTTP client.
func newHandlerWithClient(er *env.Reader, maxResultTokens int, client *http.Client) (tools.ToolDefinition, tools.ToolHandler) {
	return definition, makeHandler(er, maxResultTokens, client)
}

func makeHandler(er *env.Reader, maxResultTokens int, client *http.Client) tools.ToolHandler {
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		var in input
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", fmt.Errorf("github_api: invalid input: %w", err)
		}

		if in.Account == "office" && in.Method != "GET" {
			return "", fmt.Errorf("office account is read-only")
		}

		tokenKey := "GITHUB_TOKEN"
		if in.Account == "office" {
			tokenKey = "OFFICE_GITHUB_TOKEN"
		}
		token := er.Get(tokenKey)
		if token == "" {
			return "", fmt.Errorf("github_api: %s not set or not declared in .env", tokenKey)
		}

		var bodyReader io.Reader
		if in.Body != "" {
			bodyReader = strings.NewReader(in.Body)
		}
		req, err := http.NewRequestWithContext(ctx, in.Method, baseURL+in.Endpoint, bodyReader)
		if err != nil {
			return "", fmt.Errorf("github_api: build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if in.Body != "" {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("github_api: request: %w", err)
		}
		defer resp.Body.Close()

		respBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("github_api: read response: %w", err)
		}
		result := string(respBytes)

		// Truncate: rough 4 chars/token
		if maxChars := maxResultTokens * 4; len(result) > maxChars {
			result = result[:maxChars] + "\n[truncated]"
		}

		if resp.StatusCode >= 400 {
			return "", fmt.Errorf("github_api: HTTP %d: %s", resp.StatusCode, result)
		}
		return result, nil
	}
}
