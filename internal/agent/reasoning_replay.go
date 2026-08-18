package agent

import (
	"context"
	"slices"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func (a *Agent) runMissingReasoningFallback(
	ctx context.Context,
	turn int,
	frozen *samplingRequest,
	attemptID string,
	attempt int,
	billable *provider.Usage,
	discarded ...*deferredStreamSink,
) (streamedTurn, bool) {
	if !a.activateMissingReasoningFallback() {
		return streamedTurn{}, false
	}
	for _, sink := range discarded {
		if sink != nil {
			sink.Discard()
		}
	}
	event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningFallback})
	a.emitProtocolRetry(2, true)
	fallbackSink := newDeferredStreamSink(a.svc.sink)
	fallback := a.runSamplingAttempt(ctx, turn, fallbackSink, frozen, attemptID)
	billable = mergeSamplingUsage(billable, fallback.usage)
	a.storeLatestRequestUsage(fallback.usage)
	fallback.usage = finalizeSamplingUsage(billable, fallback.usage)
	if fallback.err != nil {
		fallbackSink.Discard()
		a.emitReasoningReplayAttemptOutcome(attemptID, attempt, fallback.err)
		return fallback, true
	}
	fallbackSink.Flush()
	event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningRetryRecovered})
	a.emitReasoningReplayAttemptOutcome(attemptID, attempt, nil)
	return fallback, true
}

func (a *Agent) preserveRawReasoning(signature, reasoningID, reasoningStatus string, calls []provider.ToolCall, searches []provider.ServerSearchCall) bool {
	if signature != "" || reasoningID != "" || reasoningStatus != "" {
		return true
	}
	return provider.RequiresAssistantReasoningReplay(a.svc.prov, provider.Message{
		Role: provider.RoleAssistant, ToolCalls: calls, ServerSearch: searches,
	})
}

func (a *Agent) emitReasoningReplayAttemptOutcome(id string, attempt int, err error) {
	if err != nil {
		a.emitStreamAttempt(id, event.StreamAttemptDiscard, attempt, "reasoning_replay", err)
		return
	}
	a.emitStreamAttempt(id, event.StreamAttemptCommit, attempt, "", nil)
}

func (a *Agent) reasoningReplayIssue(result streamedTurn) ReasoningReplayFailure {
	if a.sess.missingReasoning.fallbackActive && provider.SupportsMissingReasoningFallback(a.svc.prov) {
		return ""
	}
	if !provider.RequiresAssistantReasoningReplay(a.svc.prov, result.assistantMessage()) {
		return ""
	}
	if !result.reasoningComplete {
		return ReasoningReplayOverflow
	}
	if strings.TrimSpace(result.reasoning) == "" {
		return ReasoningReplayMissing
	}
	return ""
}

func (a *Agent) finishReasoningReplayRetry(retry streamedTurn, sink *deferredStreamSink, billable *provider.Usage) streamedTurn {
	a.storeLatestRequestUsage(retry.usage)
	retry.usage = finalizeSamplingUsage(billable, retry.usage)
	issue := a.reasoningReplayIssue(retry)
	if issue != "" {
		if issue == ReasoningReplayMissing {
			a.observeMissingAssistantReasoning(retry.assistantMessage(), retry.reasoningComplete)
		} else {
			event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryReasoningOverflowDetected})
		}
		event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningDetected})
		event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningFallback})
		return a.finishUnreplayableReasoning(retry, sink, issue)
	}
	if len(retry.calls) == 0 && len(retry.serverSearch) == 0 {
		sink.Flush()
		event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningRetryReplaced})
		return retry
	}
	a.observeMissingAssistantReasoning(retry.assistantMessage(), retry.reasoningComplete)
	sink.Flush()
	event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningRetryRecovered})
	return retry
}

func (a *Agent) finishUnreplayableReasoning(result streamedTurn, sink *deferredStreamSink, issue ReasoningReplayFailure) streamedTurn {
	if issue == "" {
		sink.Flush()
		return result
	}
	// Empty can replace reasoning the provider never emitted, never reasoning
	// truncated by the client limit: preserved-thinking protocols require the
	// returned content to remain complete and unchanged.
	allowsFallback := issue == ReasoningReplayMissing && provider.AllowsEmptyReasoningFallback(a.svc.prov)
	if len(result.calls) > 0 && !allowsFallback {
		sink.Discard()
		event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryClientToolRejected})
		result.err = &ReasoningReplayError{Kind: issue}
		return result
	}
	if len(result.serverSearch) > 0 && !allowsFallback {
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
	if provider.RequiresReasoningRoundTrip(a.svc.prov) && !allowsFallback {
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
	if a == nil || provider.AllowsEmptyReasoningFallback(a.svc.prov) ||
		(a.sess.missingReasoning.fallbackActive && provider.SupportsMissingReasoningFallback(a.svc.prov)) ||
		!provider.RequiresAssistantReasoningReplay(a.svc.prov, m) {
		return true
	}
	return strings.TrimSpace(m.ReasoningContent) != ""
}

// ensureUnreplayableHistoryRecovery installs one existing-format LocalOnly
// handoff before the new user turn is persisted. The malformed canonical turn
// stays available to the UI while the current provider receives only bounded
// structural recovery facts.
func (a *Agent) ensureUnreplayableHistoryRecovery() {
	if a == nil || a.sess.conversation == nil {
		return
	}
	if provider.AllowsEmptyReasoningFallback(a.svc.prov) ||
		(a.sess.missingReasoning.fallbackActive && provider.SupportsMissingReasoningFallback(a.svc.prov)) {
		return
	}
	msgs := a.sess.conversation.Snapshot()
	latestBad := -1
	recovery := &provider.InterruptedTurnRecovery{Pending: true}
	for i, m := range msgs {
		if m.Role != provider.RoleAssistant || !provider.RequiresAssistantReasoningReplay(a.svc.prov, m) || strings.TrimSpace(m.ReasoningContent) != "" {
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
			if _, ok := results[call.ID+"\x00"+name]; ok {
				recovery.CompletedTools = append(recovery.CompletedTools, provider.InterruptedToolSummary{ID: call.ID, Name: name})
			} else {
				recovery.InterruptedTools = appendUniqueRecoveryName(recovery.InterruptedTools, name)
			}
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
