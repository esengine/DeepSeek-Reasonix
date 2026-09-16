package agent

import (
	"context"
	"errors"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"strings"
)

func (a *Agent) recordInterruptedDisplay(text, reasoning string, calls []provider.ToolCall, pending bool, terminalErr error, workDurationMs int64, messageIDs ...string) {
	displayCalls := make([]provider.ToolCall, 0, len(calls))
	interrupted := make([]string, 0, len(calls))
	notStarted := make([]provider.InterruptedToolSummary, 0, len(calls))
	seen := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(call.Name)
		key := call.ID + "\x00" + name
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		displayCalls = append(displayCalls, provider.ToolCall{ID: call.ID, Name: name})
		if name != "" {
			interrupted = append(interrupted, name)
			notStarted = append(notStarted, provider.InterruptedToolSummary{ID: call.ID, Name: name})
		}
	}
	terminalStatus := "interrupted"
	var failureDiagnostic *provider.FailureDiagnostic
	if terminalErr != nil && !errors.Is(terminalErr, context.Canceled) {
		terminalStatus = "failed"
		failureDiagnostic = provider.DiagnoseFailure(terminalErr)
	}
	var messageID string
	if len(messageIDs) > 0 {
		messageID = messageIDs[0]
	}
	err := a.appendCommittedMessages(context.Background(), "interrupted-attempt", provider.Message{
		ID:               messageID,
		Role:             provider.RoleTool,
		Content:          text,
		ReasoningContent: reasoning,
		ToolCalls:        displayCalls,
		ToolCallID:       provider.LocalOnlyToolID,
		Name:             provider.LocalOnlyToolName,
		WorkDurationMs:   workDurationMs,
		LocalOnly:        true,
		InterruptedTurn: &provider.InterruptedTurnRecovery{
			TerminalStatus:          terminalStatus,
			FailureDiagnostic:       failureDiagnostic,
			Pending:                 pending,
			InterruptedTools:        interrupted,
			NotStartedTools:         notStarted,
			DroppedPartialText:      strings.TrimSpace(text) != "",
			DroppedPartialReasoning: strings.TrimSpace(reasoning) != "",
		},
	})
	if err != nil {
		a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Code: "transcript_save_failed", Text: "Interrupted output could not be saved; displayed output is retained."})
	} else if messageID != "" {
		a.emitStreamAttempt(messageID, event.StreamAttemptCommit, 0, "interrupted", nil)
	}
}
