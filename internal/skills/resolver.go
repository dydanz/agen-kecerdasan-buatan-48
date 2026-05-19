package skills

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type SkillFrontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Triggers    []string `yaml:"triggers"`
}

type Skill struct {
	Frontmatter SkillFrontmatter
	Body        string
	EstTokens   int
}

type Resolver struct {
	skillsDir string
}

func NewResolver(dir string) *Resolver {
	return &Resolver{skillsDir: dir}
}

// Resolve scans skillsDir on every call (hot-reload).
// Returns skill with longest matching trigger, or nil for general mode.
func (r *Resolver) Resolve(message string) (*Skill, error) {
	lower := strings.ToLower(message)

	entries, err := os.ReadDir(r.skillsDir)
	if err != nil {
		return nil, fmt.Errorf("read skills dir: %w", err)
	}

	var best *Skill
	bestLen := 0

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := filepath.Join(r.skillsDir, entry.Name(), "SKILL.md")
		data, err := os.ReadFile(skillPath)
		if err != nil {
			continue // no SKILL.md in this dir — skip silently
		}

		skill, err := parseSkill(data)
		if err != nil {
			slog.Warn("Invalid SKILL.md", "path", skillPath, "error", err)
			continue
		}

		if skill.EstTokens > 2000 {
			slog.Warn("Skill body exceeds 2000 token estimate",
				"name", skill.Frontmatter.Name, "est_tokens", skill.EstTokens)
		}

		for _, trigger := range skill.Frontmatter.Triggers {
			if strings.Contains(lower, strings.ToLower(trigger)) && len(trigger) > bestLen {
				best = skill
				bestLen = len(trigger)
			}
		}
	}

	return best, nil
}

// parseSkill splits YAML frontmatter from body and returns a Skill.
func parseSkill(data []byte) (*Skill, error) {
	// Format: "---\n<yaml>\n---\n<body>"
	// SplitN with 3 gives: ["", yaml_block, body]
	parts := strings.SplitN(string(data), "---", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("missing frontmatter delimiters")
	}

	var fm SkillFrontmatter
	if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}
	if fm.Name == "" {
		return nil, fmt.Errorf("skill missing name field")
	}

	body := strings.TrimSpace(parts[2])
	return &Skill{
		Frontmatter: fm,
		Body:        body,
		EstTokens:   len(body) / 4,
	}, nil
}
