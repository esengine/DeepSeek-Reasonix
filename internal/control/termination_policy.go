package control

import (
	"reasonix/internal/agent"
	"reasonix/internal/provider"
	"strings"
	"time"
)

// planCancelledMessages preserves the existing provider replay policy without
// mutating the executor, storage, or UI. Its result belongs to the caller.
func planCancelledMessages(msgs []provider.Message, idx int, fallback provider.Message, startedAt time.Time, canReplay func(provider.Message) bool, evidence *interruptedTailEvidence) []provider.Message {
	if start, ok := resolveInterruptedTurnStart(msgs, idx, true, startedAt, fallback); ok {
		idx = start
	}
	if idx < 0 {
		idx = 0
	}
	if idx > len(msgs) {
		idx = len(msgs)
	}
	next := append([]provider.Message{}, msgs[:idx]...)
	keptUser := false
	userEnd := idx
	for i, m := range msgs[idx:] {
		if !agent.IsUserAuthoredTurnMessage(m) {
			continue
		}
		m.Content = StripComposePrefixes(m.Content)
		next = append(next, m)
		keptUser = true
		userEnd = idx + i + 1
		break
	}
	if !keptUser && agent.IsUserAuthoredTurnMessage(fallback) {
		fallback.Content = StripComposePrefixes(fallback.Content)
		if strings.TrimSpace(fallback.Content) != "" {
			fallback.Images = append([]string(nil), fallback.Images...)
			next = append(next, fallback)
			keptUser = true
			userEnd = idx
		}
	}
	if !keptUser && len(msgs) <= idx {
		return nil
	}
	recovery := &provider.InterruptedTurnRecovery{Pending: true}
	localIndexes := make([]int, 0, 1)
	for i := userEnd; i < len(msgs); {
		m := msgs[i]
		if m.LocalOnly {
			m.Role = provider.RoleTool
			m.ToolCallID = provider.LocalOnlyToolID
			m.Name = provider.LocalOnlyToolName
			previousRecovery := m.InterruptedTurn
			m.InterruptedTurn = nil
			next = append(next, m)
			localIndexes = append(localIndexes, len(next)-1)
			recovery.DroppedPartialText = recovery.DroppedPartialText || strings.TrimSpace(m.Content) != ""
			recovery.DroppedPartialReasoning = recovery.DroppedPartialReasoning || strings.TrimSpace(m.ReasoningContent) != ""
			if previousRecovery != nil {
				mergeInterruptedRecovery(recovery, previousRecovery)
			} else {
				for _, call := range m.ToolCalls {
					provider.RecordToolRecovery(recovery, interruptedToolSummary(call), provider.ToolRunUnknown)
				}
			}
			i++
			continue
		}
		// Keep compaction digests between the pinned input and recent tool tail;
		// their summarized work is no longer available verbatim.
		if agent.IsCompactionSummary(m) {
			next = append(next, m)
			i++
			continue
		}
		if m.Role == provider.RoleAssistant {
			recordInterruptedAssistantRecovery(recovery, msgs, i, evidence)
		}
		if end, ok := completeToolTurnEnd(msgs, i); ok && canReplay(m) {
			next = append(next, msgs[i:end]...)
			i = end
			continue
		}
		switch m.Role {
		case provider.RoleAssistant:
			local := m
			local.Role = provider.RoleTool
			local.LocalOnly = true
			local.ToolCallID = provider.LocalOnlyToolID
			local.Name = provider.LocalOnlyToolName
			local.InterruptedTurn = nil
			next = append(next, local)
			localIndexes = append(localIndexes, len(next)-1)
			recovery.DroppedPartialText = recovery.DroppedPartialText || strings.TrimSpace(local.Content) != ""
			recovery.DroppedPartialReasoning = recovery.DroppedPartialReasoning || strings.TrimSpace(local.ReasoningContent) != ""
		case provider.RoleTool:
			local := m
			local.LocalOnly = true
			local.ToolCalls = []provider.ToolCall{{ID: m.ToolCallID, Name: m.Name}}
			local.ToolCallID = provider.LocalOnlyToolID
			local.Name = provider.LocalOnlyToolName
			next = append(next, local)
			localIndexes = append(localIndexes, len(next)-1)
		}
		i++
	}
	if len(localIndexes) == 0 {
		next = append(next, provider.Message{
			Role: provider.RoleTool, ToolCallID: provider.LocalOnlyToolID,
			Name: provider.LocalOnlyToolName, LocalOnly: true,
		})
		localIndexes = append(localIndexes, len(next)-1)
	}
	if evidence != nil {
		applyInterruptedTailEvidence(recovery, evidence)
	}
	next[localIndexes[len(localIndexes)-1]].InterruptedTurn = recovery
	return next
}

func applyInterruptedTailEvidence(recovery *provider.InterruptedTurnRecovery, evidence *interruptedTailEvidence) {
	if recovery.TerminalStatus != "failed" {
		recovery.Cause = "runtime_restart"
	}
	recovery.TurnID = evidence.turnID
	recovery.SilentInterruption = len(recovery.ToolCalls) == 0 && len(recovery.CompletedTools) == 0 && !recovery.DroppedPartialText && !recovery.DroppedPartialReasoning
}

func mergeInterruptedRecovery(dst, src *provider.InterruptedTurnRecovery) {
	if src.TerminalStatus != "" {
		dst.TerminalStatus = src.TerminalStatus
		dst.Cause = src.Cause
		dst.FailureDiagnostic = nil
	}
	if src.FailureDiagnostic != nil {
		diagnostic := *src.FailureDiagnostic
		dst.FailureDiagnostic = &diagnostic
	}
	dst.CompletedTools = append(dst.CompletedTools, src.CompletedTools...)
	dst.InterruptedTools = append(dst.InterruptedTools, src.InterruptedTools...)
	dst.NotStartedTools = append(dst.NotStartedTools, src.NotStartedTools...)
	dst.UnknownTools = append(dst.UnknownTools, src.UnknownTools...)
}
