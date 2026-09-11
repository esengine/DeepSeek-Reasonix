package anthropic

import (
	"strings"

	"reasonix/internal/provider"
)

func (c *client) requiresReceivedReasoning(m provider.Message) bool {
	if !c.replaysReceivedThinking() {
		return false
	}
	observed := m.ReasoningContent != "" || m.ReasoningSignature != ""
	for _, b := range m.ThinkingBlocks {
		observed = observed || b.Thinking != "" || b.Signature != "" || b.Data != ""
	}
	unsafe := m.ReasoningStatus == "in_progress" || m.ReasoningStatus == "incomplete"
	switch m.ReasoningState {
	case "", provider.ReasoningEmpty, provider.ReasoningComplete:
	default:
		unsafe = true
	}
	if !c.nativeAnthropic {
		// Receiving the Anthropic envelope is not evidence that a gateway
		// requires Claude signatures. Preserve its actual blocks instead.
		return observed || unsafe
	}
	activity := len(m.ToolCalls) > 0 || len(m.ServerSearch) > 0
	return c.replaysSignedThinking() && (observed || unsafe || activity)
}

// ConvertReasoningReplay uses Anthropic's ordinary assistant-text representation
// only for complete, unsigned, non-tool history. Tool continuations still need
// their original proof; DeepSeek and unknown gateways keep their own blocks.
func (c *client) ConvertReasoningReplay(m provider.Message) (provider.Message, bool) {
	if !c.nativeAnthropic || c.deepseek || !c.replaysSignedThinking() ||
		m.Role != provider.RoleAssistant || len(m.ToolCalls) > 0 || len(m.ServerSearch) > 0 ||
		m.ReasoningSignature != "" || len(m.ResponsesItems) > 0 {
		return m, false
	}
	var text []string
	if len(m.ThinkingBlocks) > 0 {
		for _, b := range m.ThinkingBlocks {
			if b.Type != "thinking" || b.Signature != "" || b.Data != "" {
				return m, false
			}
			if b.Thinking != "" {
				text = append(text, b.Thinking)
			}
		}
	} else if m.ReasoningContent != "" {
		text = append(text, m.ReasoningContent)
	}
	if len(text) == 0 || strings.TrimSpace(strings.Join(text, "")) == "" {
		return m, false
	}
	if m.Content != "" {
		text = append(text, m.Content)
	}
	m.Content = strings.Join(text, "\n\n")
	m.ReasoningContent, m.ReasoningSignature, m.ReasoningID, m.ReasoningStatus = "", "", "", ""
	m.ReasoningState, m.ThinkingBlocks = "", nil
	return m, true
}
