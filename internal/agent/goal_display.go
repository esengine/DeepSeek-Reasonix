package agent

import (
	"fmt"
	"strings"
)

// StripGoalMarkers removes goal status markers like [goal:complete],
// [goal:continue], and [goal:blocked:...] from display text so users see
// natural language instead of protocol markers. Exported for use by frontends.
// The markers are still kept in the session history for controller parsing.
func StripGoalMarkers(text string) string {
	text = strings.TrimSpace(text)
	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "[goal:complete]" || trimmed == "[goal:continue]" {
			continue
		}
		if strings.HasPrefix(trimmed, "[goal:blocked:") && strings.HasSuffix(trimmed, "]") {
			reason := strings.TrimPrefix(trimmed, "[goal:blocked:")
			reason = strings.TrimSuffix(reason, "]")
			if reason != "" {
				cleaned = append(cleaned, fmt.Sprintf("\u26a0\ufe0f Blocked: %s", reason))
			}
			continue
		}
		cleaned = append(cleaned, line)
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}
