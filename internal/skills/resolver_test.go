package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, dir, name, content string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const noteCaptureSkill = `---
name: note-capture
description: Capture and store facts into the knowledge brain.
triggers:
  - remember
  - note that
  - store this
---
## Note Capture
Store structured entities in GBrain.`

const researchSkill = `---
name: research
description: Research a topic with structured output.
triggers:
  - research
  - compare
  - look into
---
## Research Process
Search brain first, then enumerate options.`

func TestResolve_NoteCapture(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "note-capture", noteCaptureSkill)

	r := NewResolver(dir)
	skill, err := r.Resolve("remember that staging is ap-southeast-1")
	if err != nil {
		t.Fatal(err)
	}
	if skill == nil {
		t.Fatal("expected note-capture skill, got nil")
	}
	if skill.Frontmatter.Name != "note-capture" {
		t.Errorf("expected note-capture, got %q", skill.Frontmatter.Name)
	}
}

func TestResolve_Research(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "research", researchSkill)

	r := NewResolver(dir)
	skill, err := r.Resolve("research options for message queuing")
	if err != nil {
		t.Fatal(err)
	}
	if skill == nil || skill.Frontmatter.Name != "research" {
		t.Errorf("expected research skill, got %v", skill)
	}
}

func TestResolve_NoMatch(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "note-capture", noteCaptureSkill)

	r := NewResolver(dir)
	skill, err := r.Resolve("hello world, how are you?")
	if err != nil {
		t.Fatal(err)
	}
	if skill != nil {
		t.Errorf("expected nil for no match, got %q", skill.Frontmatter.Name)
	}
}

func TestResolve_LongestTriggerWins(t *testing.T) {
	dir := t.TempDir()
	// Skill A: trigger "look"
	// Skill B: trigger "look into" (longer → should win)
	writeSkill(t, dir, "skill-a", `---
name: skill-a
description: short trigger
triggers:
  - look
---
Body A.`)
	writeSkill(t, dir, "skill-b", `---
name: skill-b
description: long trigger
triggers:
  - look into
---
Body B.`)

	r := NewResolver(dir)
	skill, err := r.Resolve("look into this problem")
	if err != nil {
		t.Fatal(err)
	}
	if skill == nil || skill.Frontmatter.Name != "skill-b" {
		t.Errorf("expected skill-b (longer trigger), got %v", skill)
	}
}

func TestResolve_CorruptYAML_OtherSkillsLoad(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "note-capture", noteCaptureSkill)
	writeSkill(t, dir, "corrupt", `---
name: [invalid yaml {{
---
body`)

	r := NewResolver(dir)
	// Valid skill still resolves despite corrupt one
	skill, err := r.Resolve("remember this")
	if err != nil {
		t.Fatal(err)
	}
	if skill == nil || skill.Frontmatter.Name != "note-capture" {
		t.Errorf("expected note-capture despite corrupt skill, got %v", skill)
	}
}

func TestResolve_HotReload(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "dynamic", `---
name: dynamic
description: test
triggers:
  - oldtrigger
---
body`)

	r := NewResolver(dir)
	// Initially matches oldtrigger
	skill, _ := r.Resolve("oldtrigger match")
	if skill == nil || skill.Frontmatter.Name != "dynamic" {
		t.Fatal("expected initial match")
	}

	// Rewrite skill file with new trigger
	writeSkill(t, dir, "dynamic", `---
name: dynamic
description: test
triggers:
  - newtrigger
---
body`)

	// Hot-reload: old trigger no longer matches
	skill, _ = r.Resolve("oldtrigger match")
	if skill != nil {
		t.Error("old trigger should not match after hot-reload")
	}

	// New trigger matches
	skill, _ = r.Resolve("newtrigger match")
	if skill == nil || skill.Frontmatter.Name != "dynamic" {
		t.Error("new trigger should match after hot-reload")
	}
}

func TestResolve_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	r := NewResolver(dir)
	skill, err := r.Resolve("anything")
	if err != nil {
		t.Fatal(err)
	}
	if skill != nil {
		t.Error("expected nil for empty skills dir")
	}
}

func TestResolve_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "note-capture", noteCaptureSkill)

	r := NewResolver(dir)
	for _, msg := range []string{"REMEMBER this", "Remember That", "rEmEmBeR"} {
		skill, _ := r.Resolve(msg)
		if skill == nil {
			t.Errorf("expected match for %q (case insensitive)", msg)
		}
	}
}

func TestParseSkill_MissingDelimiters(t *testing.T) {
	_, err := parseSkill([]byte("no frontmatter here"))
	if err == nil {
		t.Error("expected error for missing delimiters")
	}
}

func TestParseSkill_BodyExtracted(t *testing.T) {
	skill, err := parseSkill([]byte(noteCaptureSkill))
	if err != nil {
		t.Fatal(err)
	}
	if skill.Body == "" {
		t.Error("expected non-empty body")
	}
	if skill.EstTokens == 0 {
		t.Error("expected non-zero EstTokens")
	}
}
