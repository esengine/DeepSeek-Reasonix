package provider

import (
	"slices"
	"strings"

	"reasonix/internal/nilutil"
)

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
// wire protocol accepts an assistant tool turn without provider-issued
// reasoning, either as an explicit empty field or by omitting an optional
// reasoning item. Anthropic thinking blocks do not have that fallback.
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

// ProjectReasoningStrippedMessages is the catch-and-repair projection for a
// provider that rejected replayed thinking/reasoning history with
// ReasoningReplayError. It follows the vendor-documented self-heal recipe:
// strip every assistant message's reasoning/thinking metadata from the
// provider-visible projection, then drop the tool activity that can no longer
// be paired with its thinking block. Canonical session messages are never
// modified; a history with nothing to strip keeps its backing slice.
func ProjectReasoningStrippedMessages(p Provider, msgs []Message) ([]Message, bool) {
	return projectReasoningStrippedMessages(p, msgs, len(msgs))
}

// ProjectReasoningStrippedMessagesPrefix applies the strong projection only
// to the provider-visible prefix that was present when a replay 400 was fixed.
// Messages appended after that prefix keep their normal reasoning/tool replay.
func ProjectReasoningStrippedMessagesPrefix(p Provider, msgs []Message, prefix int) ([]Message, bool) {
	if prefix < 0 || prefix > len(msgs) {
		return msgs, false
	}
	return projectReasoningStrippedMessages(p, msgs, prefix)
}

func projectReasoningStrippedMessages(p Provider, msgs []Message, prefix int) ([]Message, bool) {
	work := msgs
	stripped := false
	for i, m := range msgs[:prefix] {
		if m.Role != RoleAssistant {
			continue
		}
		if m.ReasoningContent == "" && m.ReasoningSignature == "" && m.ReasoningID == "" && m.ReasoningStatus == "" && len(m.ThinkingBlocks) == 0 && len(m.ResponsesItems) == 0 && m.ReasoningState == "" {
			continue
		}
		if !stripped {
			work = append([]Message(nil), msgs...)
			stripped = true
		}
		work[i].ReasoningContent = ""
		work[i].ReasoningSignature = ""
		work[i].ReasoningID = ""
		work[i].ReasoningStatus = ""
		work[i].ReasoningState = ReasoningIncomplete
		work[i].ThinkingBlocks = nil
		work[i].ResponsesItems = nil
	}
	projected, projectedChanged := projectReplaySafeMessages(p, work, prefix, false)
	return projected, stripped || projectedChanged
}

// ProjectReplaySafeMessages returns the provider-visible projection for
// histories that contain assistant activity without the reasoning required to
// replay it. Canonical session messages are never modified. Healthy histories
// retain their backing slice so their wire bytes and prompt-cache prefix stay
// unchanged.
//
// For an unreplayable turn, visible assistant text is preserved as a plain
// message while provider-bound activity metadata and its contiguous client-tool
// results are omitted. Providers with an explicit empty-reasoning fallback do
// not need projection.
func ProjectReplaySafeMessages(p Provider, msgs []Message) ([]Message, bool) {
	return projectReplaySafeMessages(p, msgs, len(msgs), true)
}

func projectReplaySafeMessages(p Provider, msgs []Message, prefix int, honorEmptyFallback bool) ([]Message, bool) {
	msgs, converted := projectCompatibleReplay(p, msgs, prefix)

	isUnreplayable := func(m Message) bool {
		return m.Role == RoleAssistant &&
			RequiresAssistantReasoningReplay(p, m) &&
			!HasReplayableReasoning(p, m) && (!honorEmptyFallback || !CanReplayAssistantMessage(p, m))
	}

	if !slices.ContainsFunc(msgs[:prefix], isUnreplayable) {
		return msgs, converted
	}

	out := make([]Message, 0, len(msgs))
	for i := 0; i < len(msgs); {
		m := msgs[i]
		if i >= prefix || !isUnreplayable(m) {
			out = append(out, m)
			i++
			continue
		}

		if strings.TrimSpace(m.Content) != "" {
			plain := m
			plain.ReasoningContent = ""
			plain.ReasoningSignature = ""
			plain.ReasoningID = ""
			plain.ReasoningStatus = ""
			plain.ReasoningState = ""
			plain.ThinkingBlocks = nil
			plain.ToolCalls = nil
			plain.ResponsesItems = nil
			plain.ServerSearch = nil
			out = append(out, plain)
		}
		i++
		if len(m.ToolCalls) > 0 {
			for i < len(msgs) && msgs[i].Role == RoleTool && !msgs[i].LocalOnly {
				i++
			}
		}
	}
	return out, true
}
