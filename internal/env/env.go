package env

import (
	"bufio"
	"log/slog"
	"os"
	"strings"
)

// Reader gates os.Getenv to only keys declared in a .env file.
// Values are read from the OS environment at call time, not stored in memory.
type Reader struct {
	allowed map[string]struct{}
}

// New parses the .env file at path and returns a Reader.
// Missing file logs a warning and returns an empty allowlist — not fatal.
func New(path string) *Reader {
	r := &Reader{allowed: make(map[string]struct{})}
	f, err := os.Open(path)
	if err != nil {
		slog.Warn("env: .env not found — tool env reads disabled", "path", path)
		return r
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, _ := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if key != "" {
			r.allowed[key] = struct{}{}
		}
	}
	slog.Info("env: allowlist loaded", "declared_keys", len(r.allowed))
	return r
}

// Get returns os.Getenv(key) if key is declared in .env, else "".
// Undeclared reads are logged as warnings.
func (r *Reader) Get(key string) string {
	if _, ok := r.allowed[key]; !ok {
		slog.Warn("env: tool attempted to read undeclared env var", "key", key)
		return ""
	}
	return os.Getenv(key)
}

// Keys returns the declared key names (values are never exposed).
func (r *Reader) Keys() []string {
	keys := make([]string, 0, len(r.allowed))
	for k := range r.allowed {
		keys = append(keys, k)
	}
	return keys
}
