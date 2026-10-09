package openai

import "reasonix/internal/contract/provider"

// toolCallReasoning returns the reasoning_content to serialize on m, or nil when
// the endpoint's declared protocol has no place for it.
func (c *client) toolCallReasoning(m provider.Message) *string {
	if m.Role != provider.RoleAssistant {
		return nil
	}
	switch {
	case c.kimiK3 && (m.ReasoningContent != "" || len(m.ToolCalls) > 0):
		// Kimi K3 requires the complete assistant message on multi-turn and
		// tool-call requests, including provider-issued reasoning.
		return &m.ReasoningContent
	case c.deepseek && len(m.ToolCalls) > 0:
		// DeepSeek 400s a tool_calls turn whose reasoning_content key is absent;
		// an empty value passes. Thinking off tolerates any shape, so the key
		// stays absent there and mixed sessions keep their cache prefix.
		if c.RequiresToolCallReasoning() || m.ReasoningContent != "" {
			return &m.ReasoningContent
		}
	case c.zhipu && m.ReasoningContent != "":
		// GLM interleaved and preserved thinking require provider-issued reasoning
		// returned unchanged in later history, including after thinking is turned
		// off, so an enabled→disabled session keeps valid history bytes.
		return &m.ReasoningContent
	}
	return nil
}
