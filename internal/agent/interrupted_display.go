package agent

import (
	"context"
	"errors"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/secrets"
	"strings"
)

func (a *Agent) recordInterruptedDisplay(text, reasoning string, calls []provider.ToolCall, pending bool, terminalErr error, workDurationMs int64, messageIDs ...string) {
	var messageID string
	if len(messageIDs) > 0 {
		messageID = messageIDs[0]
	}
	record := interruptedDisplayRecord(messageID, text, reasoning, calls, pending, terminalErr)
	record.WorkDurationMs = workDurationMs
	err := a.appendCommittedMessages(context.Background(), "interrupted-attempt", record)
	if err != nil {
		a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Code: "transcript_save_failed", Text: "Interrupted output could not be saved; displayed output is retained."})
	} else if messageID != "" {
		a.emitStreamAttempt(messageID, event.StreamAttemptCommit, 0, "interrupted", nil)
	}
}

// InterruptedStreamRecord is the record a cancelled stream leaves, built for a
// stream that is still open so a host can hold it durably until the stream
// either commits its message or never returns.
func InterruptedStreamRecord(messageID, text, reasoning string) provider.Message {
	return interruptedDisplayRecord(messageID, text, reasoning, nil, true, nil)
}

func interruptedDisplayRecord(messageID, text, reasoning string, calls []provider.ToolCall, pending bool, terminalErr error) provider.Message {
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
	var cause string
	if terminalErr != nil && !errors.Is(terminalErr, context.Canceled) {
		terminalStatus = "failed"
		failureDiagnostic = provider.DiagnoseFailure(terminalErr)
		cause = secrets.FailureDetail(terminalErr)
	}
	return provider.Message{
		ID:               messageID,
		Role:             provider.RoleTool,
		Content:          text,
		ReasoningContent: reasoning,
		ToolCalls:        displayCalls,
		ToolCallID:       provider.LocalOnlyToolID,
		Name:             provider.LocalOnlyToolName,
		LocalOnly:        true,
		InterruptedTurn: &provider.InterruptedTurnRecovery{
			Cause:                   cause,
			TerminalStatus:          terminalStatus,
			FailureDiagnostic:       failureDiagnostic,
			Pending:                 pending,
			InterruptedTools:        interrupted,
			NotStartedTools:         notStarted,
			DroppedPartialText:      strings.TrimSpace(text) != "",
			DroppedPartialReasoning: strings.TrimSpace(reasoning) != "",
		},
	}
}
