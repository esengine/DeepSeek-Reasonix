package provider

import "reasonix/internal/nilutil"

// AssistantReasoningReplayPolicy is optionally implemented by providers whose
// replay contract depends on the concrete assistant message. It extends the
// legacy tool-calls-only policy to provider-executed activity such as Anthropic
// server_tool_use without changing existing provider implementations.
type AssistantReasoningReplayPolicy interface {
	RequiresAssistantReasoningReplay(Message) bool
}

// RequiresAssistantReasoningReplay reports whether the exact provider-issued
// reasoning for m must survive storage and be replayed in later requests.
func RequiresAssistantReasoningReplay(p Provider, m Message) bool {
	if nilutil.IsNil(p) {
		return false
	}
	if policy, ok := p.(AssistantReasoningReplayPolicy); ok {
		return policy.RequiresAssistantReasoningReplay(m)
	}
	if RequiresReasoningRoundTrip(p) {
		return true
	}
	return len(m.ToolCalls) > 0 && RequiresToolCallReasoning(p)
}

// EmptyReasoningFallbackPolicy is optionally implemented by providers whose
// wire protocol accepts an explicit empty reasoning field for assistant tool
// turns. Anthropic thinking blocks do not have that fallback.
type EmptyReasoningFallbackPolicy interface {
	AllowsEmptyReasoningFallback() bool
}

// AllowsEmptyReasoningFallback defaults to false so unknown protocols never
// fabricate a replayable reasoning block.
func AllowsEmptyReasoningFallback(p Provider) bool {
	if nilutil.IsNil(p) {
		return false
	}
	policy, ok := p.(EmptyReasoningFallbackPolicy)
	return ok && policy.AllowsEmptyReasoningFallback()
}
