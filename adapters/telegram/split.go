package telegram

import "strings"

// splitMessage splits text into chunks of at most maxLen bytes.
// Prefers splitting at double-newline (paragraph) boundaries,
// then single newline, then hard cut.
func splitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var parts []string
	for len(text) > maxLen {
		cut := maxLen

		if idx := strings.LastIndex(text[:maxLen], "\n\n"); idx > 0 {
			cut = idx + 2
		} else if idx := strings.LastIndex(text[:maxLen], "\n"); idx > 0 {
			cut = idx + 1
		}

		parts = append(parts, strings.TrimSpace(text[:cut]))
		text = strings.TrimSpace(text[cut:])
	}
	if text != "" {
		parts = append(parts, text)
	}
	return parts
}
