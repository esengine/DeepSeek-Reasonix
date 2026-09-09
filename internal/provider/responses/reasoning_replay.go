package responses

import (
	"reasonix/internal/provider"
	"slices"
	"strings"
)

// RequiresToolCallReasoning tells the agent to preserve stateless vendors'
// reasoning on assistant tool-call turns so the follow-up can replay it.
// DeepSeek and MiMo document this requirement for multi-turn tool calls.
func (c *client) RequiresToolCallReasoning() bool {
	return c != nil && c.caps.toolCallReasoning && !responsesReasoningDisabled(c.effort)
}

// AllowsEmptyReasoningFallback reports that a Responses tool turn remains
// replayable when the provider emitted no reasoning item. DeepSeek and MiMo
// both accept the function_call/function_call_output pair without fabricating
// a reasoning item; any reasoning that was emitted is still preserved above.
func (c *client) AllowsEmptyReasoningFallback() bool {
	return c.RequiresToolCallReasoning()
}

func (c *client) MissingToolCallReasoningWarningIdentity() string {
	if c == nil {
		return ""
	}
	return strings.Join([]string{
		"responses", strings.TrimSpace(c.name), strings.TrimSpace(c.requestURL),
		strings.TrimSpace(c.model), strings.TrimSpace(c.vendor), strings.TrimSpace(c.mode), strings.TrimSpace(c.effort),
	}, "\x00")
}

// WarnOnMissingToolCallReasoning reports a tool_calls turn that arrived
// without reasoning only for vendors whose endpoint reliably emits it.
// DeepSeek's official API emits tool-call reasoning for its pro-tier models,
// so a missing chain-of-thought there is a real degradation worth one warning.
// MiMo documents reasoning alongside tool calls but does not guarantee it on
// every round (observed: mimo-v2.5-pro tool-call turn with empty reasoning),
// so a missing chain-of-thought is endpoint-conditional, not a degradation
// signal — silence the warning. Capability-driven (review #7234):
// toolCallReasoning=false vendors (DashScope) never warn — no round-trip
// contract; singleSegmentReasoning=true vendors (MiMo) never warn — their
// tool-call thinking is a single optional segment. Only multi-segment
// thinking vendors that require replay (DeepSeek) warn, scoped to non-flash.
func (c *client) WarnOnMissingToolCallReasoning() bool {
	if !c.RequiresToolCallReasoning() || c.caps.singleSegmentReasoning {
		return false
	}
	model := strings.ToLower(strings.TrimSpace(c.model))
	// Flash-tier DeepSeek models do not emit tool-call reasoning (same carve
	// as openai.go expectsDeepSeekToolCallReasoning).
	return !strings.Contains(model, "flash")
}

func (c *client) RequiresAssistantReasoningReplay(m provider.Message) bool {
	if slices.ContainsFunc(m.ResponsesItems, provider.IsReplayableResponsesReasoning) {
		return true
	}

	return c.RequiresToolCallReasoning() && (len(m.ToolCalls) > 0 || m.ReasoningContent != "")
}
