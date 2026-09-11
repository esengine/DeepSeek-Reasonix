package agent

import (
	"slices"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func (a *Agent) preserveRawReasoning(reasoning, signature, reasoningID, reasoningStatus string, calls []provider.ToolCall, searches []provider.ServerSearchCall) bool {
	if signature != "" || reasoningID != "" || reasoningStatus != "" {
		return true
	}
	return provider.RequiresAssistantReasoningReplay(a.svc.prov, provider.Message{
		Role: provider.RoleAssistant, ReasoningContent: reasoning, ToolCalls: calls, ServerSearch: searches,
	})
}

// reasoningReplayMessageFingerprint identifies the last provider-visible
// message at the original repair boundary. It deliberately ignores durable UI fields,
// matching the same wire-visible fields used by the context projection hash.
func reasoningReplayMessageFingerprint(message provider.Message) string {
	return providerVisibleFingerprint(provider.ModelMessages([]provider.Message{message}))
}

// resolveReasoningReplayPrefix maps the repaired provider prefix back onto a
// later canonical snapshot. Strong repair can remove old assistant/tool
// messages, so a raw message count alone can point into a different old turn.
func resolveReasoningReplayPrefix(msgs []provider.Message, hint int, anchor string) int {
	if hint <= 0 || hint > len(msgs) {
		return 0
	}
	if anchor == "" {
		return hint
	}
	// Removed messages only make the canonical location move forward. Prefer
	// the first matching anchor at/after the old provider-visible count; this
	// also avoids selecting an earlier duplicate user message.
	for i, message := range msgs {
		if i+1 >= hint && reasoningReplayMessageFingerprint(message) == anchor {
			return i + 1
		}
	}
	return 0
}

func (a *Agent) emitReasoningReplayAttemptOutcome(id string, attempt int, err error) {
	if err != nil {
		a.emitStreamAttempt(id, event.StreamAttemptDiscard, attempt, "reasoning_replay", err)
		return
	}
	a.emitStreamAttempt(id, event.StreamAttemptCommit, attempt, "", nil)
}

func (a *Agent) reasoningReplayIssue(result streamedTurn) ReasoningReplayFailure {
	decision := provider.DecideReasoningReplay(a.svc.prov, result.assistantMessage(), result.reasoningComplete)
	if decision == provider.ReplayDirect || decision == provider.ReplayCompatible {
		return ""
	}
	if result.reasoningState == provider.ReasoningIncomplete || result.reasoningStatus == "in_progress" || result.reasoningStatus == "incomplete" {
		return ReasoningReplayIncomplete
	}
	if !result.reasoningComplete || result.reasoningState == provider.ReasoningTruncated {
		return ReasoningReplayOverflow
	}
	if decision == provider.ReplayReject {
		return ReasoningReplayIncomplete
	}
	if !provider.HasReplayableReasoning(a.svc.prov, result.assistantMessage()) {
		return ReasoningReplayMissing
	}
	return ""
}

// finishReasoningReplayOverflow terminates an attempt whose required reasoning
// was truncated by the client limit: audit, finalize usage, and hand the turn
// to the unreplayable-reasoning policy.
func (a *Agent) finishReasoningReplayOverflow(result streamedTurn, sink *deferredStreamSink, issue ReasoningReplayFailure, billable *provider.Usage, attemptID string, attempt int) streamedTurn {
	if issue == ReasoningReplayOverflow {
		event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryReasoningOverflowDetected})
	}
	result.usage = finalizeSamplingUsage(billable, result.usage)
	terminal := a.finishUnreplayableReasoning(result, sink, issue)
	a.emitReasoningReplayAttemptOutcome(attemptID, attempt, terminal.err)
	return terminal
}

func (a *Agent) finishUnreplayableReasoning(result streamedTurn, sink *deferredStreamSink, issue ReasoningReplayFailure) streamedTurn {
	if issue == "" {
		sink.Flush()
		return result
	}
	// Empty can replace reasoning the provider never emitted, never reasoning
	// truncated by the client limit: preserved-thinking protocols require the
	// returned content to remain complete and unchanged.
	allowsEmptyReasoning := issue == ReasoningReplayMissing && provider.AllowsEmptyReasoningFallback(a.svc.prov)
	if len(result.calls) > 0 && !allowsEmptyReasoning {
		sink.Discard()
		event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryClientToolRejected})
		result.err = &ReasoningReplayError{Kind: issue}
		return result
	}
	if len(result.serverSearch) > 0 && !allowsEmptyReasoning {
		if strings.TrimSpace(result.text) == "" {
			sink.Discard()
			result.err = &ReasoningReplayError{Kind: issue}
			return result
		}
		// Preserve the answer and search cards locally. The provider projection
		// removes these unreplayable search blocks on later requests.
		result.reasoning = ""
		result.signature = ""
		result.reasoningID = ""
		result.reasoningStatus = ""
		result.reasoningComplete = true
		sink.Flush()
		event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryServerSearchSalvaged})
		return result
	}
	if provider.RequiresReasoningRoundTrip(a.svc.prov) && !allowsEmptyReasoning {
		sink.Discard()
		result.err = &ReasoningReplayError{Kind: issue}
		return result
	}
	// OpenAI-style DeepSeek protocols deliberately serialize an empty
	// reasoning_content field as their final compatibility fallback.
	sink.Flush()
	return result
}

// CanReplayAssistantMessage lets the controller apply the provider-specific
// half of interrupted-turn validation without exposing the provider itself.
func (a *Agent) CanReplayAssistantMessage(m provider.Message) bool {
	return a == nil || provider.CanReplayAssistantMessage(a.svc.prov, m)
}

// ensureUnreplayableHistoryRecovery installs one existing-format LocalOnly
// handoff before the new user turn is persisted. The malformed canonical turn
// stays available to the UI while the current provider receives only bounded
// structural recovery facts.
func (a *Agent) ensureUnreplayableHistoryRecovery() {
	if a == nil || a.sess.conversation == nil {
		return
	}

	msgs := a.sess.conversation.Snapshot()
	latestBad := -1
	recovery := &provider.InterruptedTurnRecovery{Pending: true}
	for i, m := range msgs {
		if m.Role != provider.RoleAssistant || provider.CanReplayAssistantMessage(a.svc.prov, m) {
			continue
		}
		latestBad = i
		results := map[string]provider.Message{}
		for j := i + 1; j < len(msgs) && msgs[j].Role == provider.RoleTool && !msgs[j].LocalOnly; j++ {
			results[msgs[j].ToolCallID+"\x00"+msgs[j].Name] = msgs[j]
		}
		for _, call := range m.ToolCalls {
			name := strings.TrimSpace(call.Name)
			if name == "" {
				continue
			}
			state := provider.ToolRunUnknown
			if result, ok := results[call.ID+"\x00"+name]; ok {
				state = provider.ToolResultRunState(result)
			}
			provider.RecordToolRecovery(recovery, provider.InterruptedToolSummary{ID: call.ID, Name: name}, state)
		}
		for _, search := range m.ServerSearch {
			if len(search.Results) > 0 || len(search.Raw) > 0 {
				recovery.CompletedTools = append(recovery.CompletedTools, provider.InterruptedToolSummary{ID: search.ID, Name: "web_search"})
			} else {
				recovery.InterruptedTools = appendUniqueRecoveryName(recovery.InterruptedTools, "web_search")
			}
		}
	}
	if latestBad < 0 {
		return
	}
	for _, m := range msgs[latestBad+1:] {
		if m.LocalOnly && m.InterruptedTurn != nil {
			return
		}
	}
	a.sess.conversation.Add(provider.Message{
		Role: provider.RoleTool, ToolCallID: provider.LocalOnlyToolID,
		Name: provider.LocalOnlyToolName, LocalOnly: true, InterruptedTurn: recovery,
	})
	event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryHistoryRepaired})
}

func appendUniqueRecoveryName(dst []string, name string) []string {
	if slices.Contains(dst, name) {
		return dst
	}
	return append(dst, name)
}
