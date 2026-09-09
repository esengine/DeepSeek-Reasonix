package cli

import (
	"slices"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

func cliHistoryWithoutPinnedContextRevisions(messages []provider.Message) []provider.Message {
	for i, message := range messages {
		if !agent.IsPinnedContextRevision(message) {
			continue
		}
		visible := make([]provider.Message, 0, len(messages)-1)
		visible = append(visible, messages[:i]...)
		for _, candidate := range messages[i+1:] {
			if !agent.IsPinnedContextRevision(candidate) {
				visible = append(visible, candidate)
			}
		}
		return visible
	}
	return messages
}

// copyAssistantParts returns the Content of assistant messages after the last
// user message in msgs, skipping empty strings and model placeholders ("…", "...").
// The result is chronological (oldest first).
func copyAssistantParts(msgs []provider.Message) []string {
	lastUserIdx := -1
	for i, v := range slices.Backward(msgs) {
		if v.Role == provider.RoleUser && !agent.IsPinnedContextRevision(v) {
			lastUserIdx = i
			break
		}
	}
	start := lastUserIdx + 1
	if lastUserIdx < 0 {
		start = 0
	}
	var parts []string
	for i := start; i < len(msgs); i++ {
		if msgs[i].Role != provider.RoleAssistant {
			continue
		}
		c := strings.TrimSpace(msgs[i].Content)
		if c == "" || c == "..." || c == "…" {
			continue
		}
		parts = append(parts, c)
	}
	return parts
}
