package transcript

import (
	"fmt"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// Terminal records belong to the authoritative snapshot, not only the live
// frontend. Advancing coverage without them would permanently hide recovery
// actions on reconnect, including after the WAL has been compacted.
func (p *Projection) applyTerminalNotices(e event.Event) {
	var rows []Message
	row := Message{Role: "notice", Level: "info"}
	switch {
	case e.Outcome == event.TurnOutcomeIncompleteRead:
		row.Code, row.ReadPause = e.Outcome, e.ReadPause
	case e.Outcome == event.TurnOutcomeFinalReadiness:
		row.Code, row.Pending, row.Readiness = e.Outcome, true, e.Readiness
		row.Content = "Task is not complete; continue the remaining work or checks."
	case e.Outcome == event.TurnOutcomeRecoveryPaused:
		row.Code, row.Content = e.Outcome, "Automatic recovery paused. You can continue the task."
	case e.Outcome == event.TurnOutcomeCompletionUncertain:
		row.Code, row.Content = e.Outcome, "The host could not confirm this turn is complete."
	case e.Status == event.TurnInterrupted || e.Status == event.TurnRecoveryRequired:
		row = interruptedNotice(nil)
	case e.Err != nil:
		row.Code, row.Level, row.Content, row.Detail = event.NoticeCodeProviderRequestFailed, "warn", e.Err.Error(), e.Detail
		row.Diagnostic = e.Diagnostic
	default:
		row = Message{}
	}
	if row.Role != "" {
		rows = append(rows, row)
	}
	if e.ReadCompletion != nil {
		rows = append(rows, readCompletionMessage(e.ReadCompletion))
	}
	if e.ProtocolRecovery != nil && e.Status != event.TurnInterrupted {
		rows = append(rows, Message{Role: "notice", Code: "protocol_recovery", Level: "info", Pending: true,
			Content: "The interrupted task can continue from valid context.", ProtocolRecovery: e.ProtocolRecovery})
	}
	for _, row := range rows {
		present := false
		for _, existing := range p.buffer.messages {
			if existing.message.TurnID == e.TurnID && existing.message.Code == row.Code && row.Code != "" {
				present = true
				break
			}
		}
		if present {
			continue
		}
		row.RecordID = fmt.Sprintf("terminal:%s:%s", e.TurnID, row.Code)
		row.TurnID, row.Source = e.TurnID, e.Source
		p.buffer.messages = append(p.buffer.messages, &bufferedMessage{message: row})
	}
}

func (p *Projection) retireRecoveryNotices() {
	for _, row := range p.buffer.messages {
		if row.message.Role == "notice" && (row.message.ProtocolRecovery != nil || row.message.Readiness != nil) {
			row.message.Pending = false
		}
	}
}

func readCompletionMessage(receipt *provider.ReadCompletion) Message {
	parts := make([]string, 0, len(receipt.Reads))
	for _, read := range receipt.Reads {
		parts = append(parts, fmt.Sprintf("%s · %s · covered=%v", read.Path, read.Verdict, read.Covered))
	}
	return Message{Role: "notice", Code: "read_completion", Level: "info", Content: "Partial read coverage was accepted for this turn.",
		Detail: strings.Join(parts, "\n"), ReadCompletion: receipt}
}
