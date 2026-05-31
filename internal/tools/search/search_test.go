package search

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dydanz/akb48/internal/env"
)

func makeEnvReader(t *testing.T, keys ...string) *env.Reader {
	t.Helper()
	var lines string
	for _, k := range keys {
		lines += k + "=ignored\n"
	}
	f, err := os.CreateTemp(t.TempDir(), ".env")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(lines)
	f.Close()
	return env.New(f.Name())
}

func marshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// rewriteTransport redirects all requests to a test server.
type rewriteTransport struct {
	base  string
	inner http.RoundTripper
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = "http"
	clone.URL.Host = strings.TrimPrefix(rt.base, "http://")
	return rt.inner.RoundTrip(clone)
}

func braveBody(results []map[string]string) []byte {
	type result struct {
		Title       string `json:"title"`
		URL         string `json:"url"`
		Description string `json:"description"`
	}
	type webSection struct {
		Results []result `json:"results"`
	}
	type response struct {
		Web webSection `json:"web"`
	}
	resp := response{}
	for _, r := range results {
		resp.Web.Results = append(resp.Web.Results, result{
			Title:       r["title"],
			URL:         r["url"],
			Description: r["description"],
		})
	}
	b, _ := json.Marshal(resp)
	return b
}

func TestWebSearch_MissingKey(t *testing.T) {
	er := makeEnvReader(t) // BRAVE_SEARCH_API_KEY not declared
	_, handler := newHandlerWithClient(er, 500, http.DefaultClient)

	_, err := handler(t.Context(), marshal(t, map[string]string{"query": "golang"}))
	if err == nil || !strings.Contains(err.Error(), "BRAVE_SEARCH_API_KEY") {
		t.Errorf("expected BRAVE_SEARCH_API_KEY error, got %v", err)
	}
}

func TestWebSearch_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Subscription-Token") != "testkey" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(braveBody([]map[string]string{
			{"title": "Go 1.23 Release", "url": "https://go.dev/doc/go1.23", "description": "New iterators and more."},
			{"title": "Go Blog", "url": "https://go.dev/blog", "description": "Official Go blog."},
		}))
	}))
	defer srv.Close()

	er := makeEnvReader(t, "BRAVE_SEARCH_API_KEY")
	t.Setenv("BRAVE_SEARCH_API_KEY", "testkey")
	client := &http.Client{Transport: &rewriteTransport{base: srv.URL, inner: srv.Client().Transport}}
	_, handler := newHandlerWithClient(er, 500, client)

	out, err := handler(t.Context(), marshal(t, map[string]string{"query": "golang 1.23"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "[1]") || !strings.Contains(out, "Go 1.23 Release") {
		t.Errorf("unexpected output: %s", out)
	}
	if !strings.Contains(out, "URL:") || !strings.Contains(out, "Snippet:") {
		t.Errorf("expected formatted output with URL and Snippet, got: %s", out)
	}
}

func TestWebSearch_HTTP4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer srv.Close()

	er := makeEnvReader(t, "BRAVE_SEARCH_API_KEY")
	t.Setenv("BRAVE_SEARCH_API_KEY", "tok")
	client := &http.Client{Transport: &rewriteTransport{base: srv.URL, inner: srv.Client().Transport}}
	_, handler := newHandlerWithClient(er, 500, client)

	_, err := handler(t.Context(), marshal(t, map[string]string{"query": "test"}))
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Errorf("expected HTTP 403 error, got %v", err)
	}
}

func TestWebSearch_RateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
	}))
	defer srv.Close()

	er := makeEnvReader(t, "BRAVE_SEARCH_API_KEY")
	t.Setenv("BRAVE_SEARCH_API_KEY", "tok")
	client := &http.Client{Transport: &rewriteTransport{base: srv.URL, inner: srv.Client().Transport}}
	_, handler := newHandlerWithClient(er, 500, client)

	_, err := handler(t.Context(), marshal(t, map[string]string{"query": "test"}))
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("expected rate limited error, got %v", err)
	}
}

func TestWebSearch_NoResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(braveBody(nil)) // empty results
	}))
	defer srv.Close()

	er := makeEnvReader(t, "BRAVE_SEARCH_API_KEY")
	t.Setenv("BRAVE_SEARCH_API_KEY", "tok")
	client := &http.Client{Transport: &rewriteTransport{base: srv.URL, inner: srv.Client().Transport}}
	_, handler := newHandlerWithClient(er, 500, client)

	out, err := handler(t.Context(), marshal(t, map[string]string{"query": "xyzzy404notfound"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "no results found") {
		t.Errorf("expected no-results message, got: %s", out)
	}
}

func TestWebSearch_Truncation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a very long description
		w.Write(braveBody([]map[string]string{
			{"title": "Long Result", "url": "https://example.com", "description": strings.Repeat("x", 500)},
		}))
	}))
	defer srv.Close()

	er := makeEnvReader(t, "BRAVE_SEARCH_API_KEY")
	t.Setenv("BRAVE_SEARCH_API_KEY", "tok")
	client := &http.Client{Transport: &rewriteTransport{base: srv.URL, inner: srv.Client().Transport}}
	_, handler := newHandlerWithClient(er, 10, client) // maxResultTokens=10 → maxChars=40

	out, err := handler(t.Context(), marshal(t, map[string]string{"query": "test"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "[truncated]") {
		t.Errorf("expected truncation marker, got: %s", out)
	}
}

func TestWebSearch_DefaultCount(t *testing.T) {
	capturedCount := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCount = r.URL.Query().Get("count")
		w.Write(braveBody(nil))
	}))
	defer srv.Close()

	er := makeEnvReader(t, "BRAVE_SEARCH_API_KEY")
	t.Setenv("BRAVE_SEARCH_API_KEY", "tok")
	client := &http.Client{Transport: &rewriteTransport{base: srv.URL, inner: srv.Client().Transport}}
	_, handler := newHandlerWithClient(er, 500, client)

	// No count specified → default 5
	handler(t.Context(), marshal(t, map[string]string{"query": "test"})) //nolint:errcheck
	if capturedCount != "5" {
		t.Errorf("expected default count=5, got %q", capturedCount)
	}
}
