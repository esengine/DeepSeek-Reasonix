package tui

import "strings"

// settledPrefix is how much of a streaming answer is made of finished
// Markdown blocks: everything up to the last blank line that is not inside a
// fenced code block. That part cannot change as more text arrives, so it can
// go to the terminal's scrollback while the rest is still being written.
func settledPrefix(text string) int {
	settled := 0
	fence := ""
	offset := 0
	for line := range strings.SplitAfterSeq(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case fence == "" && (strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")):
			fence = trimmed[:3]
		case fence != "" && strings.HasPrefix(trimmed, fence):
			fence = ""
		case fence == "" && trimmed == "" && strings.HasSuffix(line, "\n"):
			settled = offset + len(line)
		}
		offset += len(line)
	}
	return settled
}
