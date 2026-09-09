package openai

import (
	"strings"

	"reasonix/internal/provider"
)

func hasReasoningOrToolCall(m provider.Message) bool {
	return len(m.ToolCalls) > 0 || m.ReasoningContent != ""
}

// RequiresAssistantReasoningReplay keeps provider-issued reasoning replayable
// on every DeepSeek-style assistant turn that carries it. Tool turns remain
// required even when reasoning was lost because the wire accepts an empty key.
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
	if c.deepseek {
		return strings.TrimSpace(m.ReasoningContent) != "" ||
			(len(m.ToolCalls) > 0 && c.RequiresToolCallReasoning())
	}
	if c.RequiresToolCallReasoning() {
		return len(m.ToolCalls) > 0 || strings.TrimSpace(m.ReasoningContent) != ""
	}
	return false
}
