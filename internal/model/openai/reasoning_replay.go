package openai

import "reasonix/internal/contract/provider"

// toolCallReasoning returns the reasoning_content to serialize on m, and whether
// this turn's reasoning was left behind instead. One function, so the wire
// decision and the diagnosis of a refused body cannot disagree.
func (c *client) toolCallReasoning(m provider.Message) (*string, bool) {
	if m.Role != provider.RoleAssistant {
		return nil, false
	}
	switch {
	case c.kimiK3 && (m.ReasoningContent != "" || len(m.ToolCalls) > 0):
		// Kimi K3 requires the complete assistant message on multi-turn and
		// tool-call requests, including provider-issued reasoning.
		return &m.ReasoningContent, false
	case c.deepseek && len(m.ToolCalls) > 0:
		// DeepSeek 400s a tool_calls turn whose reasoning_content key is absent;
		// an empty value passes. Thinking off tolerates any shape, so the key
		// stays absent there and mixed sessions keep their cache prefix.
		if c.RequiresToolCallReasoning() || m.ReasoningContent != "" {
			return &m.ReasoningContent, false
		}
	case c.zhipu && m.ReasoningContent != "":
		// GLM interleaved and preserved thinking require provider-issued reasoning
		// returned unchanged in later history, including after thinking is turned
		// off, so an enabled→disabled session keeps valid history bytes.
		return &m.ReasoningContent, false
	}
	// Nothing went out. A tool_calls turn that had reasoning is the one shape a
	// thinking endpoint refuses, and the only one worth reporting.
	return nil, len(m.ToolCalls) > 0 && m.ReasoningContent != ""
}
