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
	case c.RequiresToolCallReasoning():
		// DeepSeek thinking mode 400s an assistant history turn whose
		// reasoning_content key is absent, a plain turn included; an empty
		// value passes.
		return c.replayedReasoning(m), false
	case c.deepseek && m.ReasoningContent != "":
		// Thinking off tolerates any shape, so a keyless plain turn keeps its
		// cache prefix; reasoning the endpoint already issued still replays.
		return c.replayedReasoning(m), false
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

// replayedReasoning is the reasoning_content to send back on a DeepSeek
// assistant turn. The key must be present, but strip_chain_of_thought sends it
// empty so the endpoint does not bill the replayed reasoning as prompt input.
func (c *client) replayedReasoning(m provider.Message) *string {
	if c.stripChainOfThought {
		empty := ""
		return &empty
	}
	return &m.ReasoningContent
}
