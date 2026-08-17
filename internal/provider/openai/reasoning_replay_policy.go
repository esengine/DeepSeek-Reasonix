package openai

import (
	"strings"

	"reasonix/internal/provider"
)

// RequiresAssistantReasoningReplay narrows GLM's broad preservation policy to
// reasoning the endpoint actually emitted. GLM may omit reasoning on valid
// thinking-enabled tool turns; those turns remain replayable without inventing
// an empty chain. Kimi K3 still requires every complete assistant message.
func (c *client) RequiresAssistantReasoningReplay(m provider.Message) bool {
	if c == nil {
		return false
	}
	if c.kimiK3 {
		return true
	}
	if c.zhipu {
		return strings.TrimSpace(m.ReasoningContent) != ""
	}
	return len(m.ToolCalls) > 0 && c.RequiresToolCallReasoning()
}
