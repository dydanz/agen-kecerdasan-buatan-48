package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestOfficeWrite_Blocked(t *testing.T) {
	er := makeEnvReader(t, "OFFICE_GITHUB_TOKEN")
	t.Setenv("OFFICE_GITHUB_TOKEN", "tok")
	_, handler := newHandlerWithClient(er, 500, http.DefaultClient)

	_, err := handler(t.Context(), marshal(t, map[string]string{
		"account":  "office",
		"method":   "POST",
		"endpoint": "/repos/acme/backend/issues",
		"body":     `{"title":"x"}`,
	}))
	if err == nil || err.Error() != "office account is read-only" {
		t.Errorf("want 'office account is read-only', got %v", err)
	}
}

func TestMissingToken_ReturnsError(t *testing.T) {
	er := env.New(filepath.Join(t.TempDir(), "nonexistent.env"))
	_, handler := newHandlerWithClient(er, 500, http.DefaultClient)

	_, err := handler(t.Context(), marshal(t, map[string]string{
		"account":  "personal",
		"method":   "GET",
		"endpoint": "/repos/dydanz/akb48/issues",
	}))
	if err == nil || err.Error() == "" {
		t.Errorf("want token error, got %v", err)
	}
}

func TestPersonalGET_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mytoken" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`[{"id":1}]`))
	}))
	defer srv.Close()

	er := makeEnvReader(t, "GITHUB_TOKEN")
	t.Setenv("GITHUB_TOKEN", "mytoken")
	_, handler := newHandlerWithClient(er, 500, srv.Client())

	// Override baseURL for test server — patch via closure trick
	origBase := baseURL
	_ = origBase // baseURL is a const; test server URL used via custom transport below

	// Use a client that redirects to test server
	client := &http.Client{
		Transport: &rewriteTransport{base: srv.URL, inner: srv.Client().Transport},
	}
	_, handler2 := newHandlerWithClient(er, 500, client)

	out, err := handler2(t.Context(), marshal(t, map[string]string{
		"account":  "personal",
		"method":   "GET",
		"endpoint": "/repos/dydanz/akb48/issues",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != `[{"id":1}]` {
		t.Errorf("unexpected output: %s", out)
	}
	_ = handler
}

func TestOfficeGET_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name":"repo"}`))
	}))
	defer srv.Close()

	er := makeEnvReader(t, "OFFICE_GITHUB_TOKEN")
	t.Setenv("OFFICE_GITHUB_TOKEN", "officetoken")
	client := &http.Client{
		Transport: &rewriteTransport{base: srv.URL, inner: srv.Client().Transport},
	}
	_, handler := newHandlerWithClient(er, 500, client)

	out, err := handler(t.Context(), marshal(t, map[string]string{
		"account":  "office",
		"method":   "GET",
		"endpoint": "/repos/acme/backend",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != `{"name":"repo"}` {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestTruncation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":"` + string(make([]byte, 200)) + `"}`))
	}))
	defer srv.Close()

	er := makeEnvReader(t, "GITHUB_TOKEN")
	t.Setenv("GITHUB_TOKEN", "tok")
	client := &http.Client{
		Transport: &rewriteTransport{base: srv.URL, inner: srv.Client().Transport},
	}
	_, handler := newHandlerWithClient(er, 10, client) // maxResultTokens=10 → maxChars=40

	out, err := handler(t.Context(), marshal(t, map[string]string{
		"account":  "personal",
		"method":   "GET",
		"endpoint": "/test",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "[truncated]") {
		t.Errorf("expected truncation marker in output, got: %s", out)
	}
}

func TestHTTP4xx_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()

	er := makeEnvReader(t, "GITHUB_TOKEN")
	t.Setenv("GITHUB_TOKEN", "tok")
	client := &http.Client{
		Transport: &rewriteTransport{base: srv.URL, inner: srv.Client().Transport},
	}
	_, handler := newHandlerWithClient(er, 500, client)

	_, err := handler(t.Context(), marshal(t, map[string]string{
		"account":  "personal",
		"method":   "GET",
		"endpoint": "/repos/dydanz/missing",
	}))
	if err == nil {
		t.Error("expected error for 404 response")
	}
}

// rewriteTransport rewrites the host of every request to the test server base URL.
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
