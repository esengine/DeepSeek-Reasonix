package history

import (
	"regexp"
	"strings"
)

var reComposeBlock = regexp.MustCompile(`(?s)^\s*<(?:memory-update|background-jobs|active-goal)>.*?</(?:memory-update|background-jobs|active-goal)>\s*\n`)

const planModeMarkerPrefix = "[Plan mode"

func stripComposePrefixes(content string) string {
	s := content
	for {
		next := reComposeBlock.ReplaceAllString(s, "")
		if next == s {
			break
		}
		s = next
	}
	trimmed := strings.TrimSpace(s)
	if strings.HasPrefix(trimmed, planModeMarkerPrefix) {
		if _, rest, ok := strings.Cut(trimmed, "]"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return trimmed
}
