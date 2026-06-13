package agent

import (
	"regexp"
	"strings"
)

var reTransientUserBlock = regexp.MustCompile(`(?s)^\s*<(?:reasoning-language|memory-update|background-jobs)>.*?</(?:reasoning-language|memory-update|background-jobs)>\s*\n?`)

// StripTransientUserBlocks removes controller-injected transient XML blocks
// from persisted user messages before deriving display text, previews, or
// titles. The blocks are sent in user turns so they never affect the stable
// prompt prefix, but they should not become user-facing text later.
func StripTransientUserBlocks(content string) string {
	s := content
	for {
		next := reTransientUserBlock.ReplaceAllStringFunc(s, func(string) string {
			return ""
		})
		if next == s {
			break
		}
		s = next
	}
	return strings.TrimLeft(s, " \t\r\n")
}

// UserPreviewText returns the user-authored part of a persisted user message.
func UserPreviewText(content string) string {
	s := StripTransientUserBlocks(content)
	s = HandoffTask(s)
	s = StripTransientUserBlocks(s)
	return strings.TrimSpace(s)
}
