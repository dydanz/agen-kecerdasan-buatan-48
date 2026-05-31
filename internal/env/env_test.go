package env

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempEnv(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), ".env")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

func TestGet_AllowedKey(t *testing.T) {
	path := writeTempEnv(t, "MY_TOKEN=ignored\n")
	t.Setenv("MY_TOKEN", "secret123")

	r := New(path)
	if got := r.Get("MY_TOKEN"); got != "secret123" {
		t.Errorf("want secret123, got %q", got)
	}
}

func TestGet_BlockedKey(t *testing.T) {
	path := writeTempEnv(t, "DECLARED=ignored\n")
	t.Setenv("UNDECLARED", "should-not-leak")

	r := New(path)
	if got := r.Get("UNDECLARED"); got != "" {
		t.Errorf("want empty string for undeclared key, got %q", got)
	}
}

func TestGet_MissingEnvFile(t *testing.T) {
	r := New(filepath.Join(t.TempDir(), "nonexistent.env"))
	t.Setenv("ANY_VAR", "value")

	if got := r.Get("ANY_VAR"); got != "" {
		t.Errorf("want empty string when .env missing, got %q", got)
	}
}

func TestNew_SkipsCommentsAndBlanks(t *testing.T) {
	path := writeTempEnv(t, `
# this is a comment
REAL_KEY=value

# another comment
OTHER_KEY=val
`)
	r := New(path)
	keys := r.Keys()
	if len(keys) != 2 {
		t.Errorf("want 2 keys, got %d: %v", len(keys), keys)
	}
}

func TestKeys_ReturnsOnlyNames(t *testing.T) {
	path := writeTempEnv(t, "ALPHA=1\nBETA=2\n")
	r := New(path)
	keys := r.Keys()
	seen := make(map[string]bool)
	for _, k := range keys {
		seen[k] = true
	}
	if !seen["ALPHA"] || !seen["BETA"] {
		t.Errorf("expected ALPHA and BETA in keys, got %v", keys)
	}
}
