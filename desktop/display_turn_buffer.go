package main

import (
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/provider"
	"reasonix/internal/turnevent"
	"strings"
)

// displayTextAccumulator retains provider chunks without repeatedly copying
// the complete prefix. A turn only materializes the final string when its
// display-only history is persisted; successful executor turns are discarded
// without ever joining their chunks.
type displayTextAccumulator struct {
	parts []string
	size  int
}

func (a *displayTextAccumulator) append(text string) {
	if text == "" {
		return
	}
	a.parts = append(a.parts, text)
	a.size += len(text)
}

func (a *displayTextAccumulator) replace(text string) {
	a.parts = nil
	a.size = 0
	a.append(text)
}

func (a *displayTextAccumulator) hasNonWhitespace() bool {
	for _, part := range a.parts {
		if strings.TrimSpace(part) != "" {
			return true
		}
	}
	return false
}

func (a *displayTextAccumulator) string() string {
	switch len(a.parts) {
	case 0:
		return ""
	case 1:
		return a.parts[0]
	}
	var out strings.Builder
	out.Grow(a.size)
	for _, part := range a.parts {
		out.WriteString(part)
	}
	return out.String()
}

type bufferedHistoryMessage struct {
	message   HistoryMessage
	content   displayTextAccumulator
	reasoning displayTextAccumulator
}

func (m *bufferedHistoryMessage) materialize() HistoryMessage {
	out := m.message
	if out.Role == "assistant" {
		out.Content = m.content.string()
		out.Reasoning = m.reasoning.string()
	}
	if len(out.MemoryCitations) > 0 {
		out.MemoryCitations = append([]provider.MemoryCitation(nil), out.MemoryCitations...)
	}
	if len(out.ToolCalls) > 0 {
		out.ToolCalls = append([]HistoryToolCall(nil), out.ToolCalls...)
	}
	return out
}

type displayTurnBuffer struct {
	messages   []*bufferedHistoryMessage
	tools      map[string]string
	completion *eventwire.CompletionSummary
}

func (b *displayTurnBuffer) reset() {
	b.messages = nil
	b.tools = nil
	b.completion = nil
}

func (b *displayTurnBuffer) resultMessages() []HistoryMessage {
	var out []HistoryMessage
	for _, m := range b.messages {
		if m.message.Code == "turn_result" {
			out = append(out, m.materialize())
		}
	}
	return out
}

func (b *displayTurnBuffer) materialize() []HistoryMessage {
	if len(b.messages) == 0 {
		return nil
	}
	out := make([]HistoryMessage, 0, len(b.messages))
	for _, message := range b.messages {
		out = append(out, message.materialize())
	}
	return out
}

func recordHistoryDisplayEvent(buffer *displayTurnBuffer, e event.Event) {
	switch e.Kind {
	case event.CompletionSummary:
		wire := eventwire.ToWire(e)
		buffer.completion = wire.Completion
	case event.TurnDone:
		wire := eventwire.ToWire(e)
		if wire.Receipt != nil || buffer.completion != nil {
			buffer.messages = append(buffer.messages, &bufferedHistoryMessage{message: HistoryMessage{
				Role: "notice", Code: "turn_result", Level: "info", TurnID: e.TurnID,
				CompletionReceipt: wire.Receipt, CompletionSummary: buffer.completion, CheckpointTurn: e.CheckpointTurn,
			}})
		}
	case event.Phase:
		if strings.TrimSpace(e.Text) != "" {
			buffer.messages = append(buffer.messages, &bufferedHistoryMessage{message: HistoryMessage{Role: "phase", Content: e.Text}})
		}
	case event.Reasoning:
		if e.Text != "" {
			hm := ensureDisplayAssistant(buffer)
			hm.reasoning.append(e.Text)
		}
	case event.Text:
		if e.Text != "" {
			hm := ensureDisplayAssistant(buffer)
			hm.content.append(e.Text)
		}
	case event.Message:
		if e.Text != "" || e.Reasoning != "" || len(e.MemoryCitations) > 0 {
			hm := ensureDisplayAssistant(buffer)
			if e.Text != "" {
				hm.content.replace(e.Text)
			}
			if e.Reasoning != "" {
				hm.reasoning.replace(e.Reasoning)
			}
			if len(e.MemoryCitations) > 0 {
				hm.message.MemoryCitations = append([]provider.MemoryCitation(nil), e.MemoryCitations...)
			}
		}
	case event.ToolDispatch:
		recordHistoryToolDispatch(buffer, e)
	case event.ToolResult:
		callID := strings.TrimSpace(e.Tool.ID)
		content := firstNonEmpty(e.Tool.Output, e.Tool.Err)
		display, errPreview := plannerToolResultDisplay(content, e.Tool.Err != "")
		if callID != "" {
			updateBufferedHistoryToolCallSummary(buffer.messages, callID, content)
		}
		toolName := e.Tool.Name
		if toolName == "" && buffer.tools != nil {
			toolName = buffer.tools[callID]
		}
		buffer.messages = append(buffer.messages, &bufferedHistoryMessage{message: HistoryMessage{
			Role:            "tool",
			ToolCallID:      callID,
			ToolName:        toolName,
			Content:         display,
			ToolResultError: errPreview,
		}})
	case event.Notice:
		if strings.TrimSpace(e.Text) != "" {
			level := "info"
			if e.Level == event.LevelWarn {
				level = "warn"
			}
			buffer.messages = append(buffer.messages, &bufferedHistoryMessage{message: HistoryMessage{
				Role:            "notice",
				Level:           level,
				Content:         e.Text,
				Detail:          e.Detail,
				Code:            e.Code,
				DecisionReceipt: cloneDecisionReceipt(e.DecisionReceipt),
			}})
		}
	}
}

func displayEventFromEnvelope(envelope turnevent.Envelope) (event.Event, bool) {
	w := envelope.Event
	e := event.Event{
		TurnID: envelope.TurnID, Sequence: envelope.Sequence, Status: envelope.Status,
		Text: w.Text, Detail: w.Detail, Reasoning: w.Reasoning, ItemID: envelope.ItemID, Source: envelope.Source,
	}
	switch envelope.Kind {
	case "phase":
		e.Kind = event.Phase
	case "reasoning":
		e.Kind = event.Reasoning
	case "text":
		e.Kind = event.Text
	case "message":
		e.Kind = event.Message
	case "tool_dispatch":
		e.Kind = event.ToolDispatch
	case "tool_result":
		e.Kind = event.ToolResult
	case "notice":
		e.Kind = event.Notice
	case "completion_summary":
		e.Kind = event.CompletionSummary
		if w.Completion != nil {
			c := w.Completion
			e.Completion = &event.CompletionSummaryInfo{Preset: c.Preset, Verdict: c.Verdict, Mutations: c.Mutations, ChecksPassed: c.ChecksPassed, ChecksFailed: c.ChecksFailed, ChecksSuppressed: c.ChecksSuppressed, Review: c.Review, GapKinds: c.GapKinds, ConstraintDegraded: c.ConstraintDegraded, Floor: c.Floor, Attention: c.Attention}
		}
	case "turn_done":
		e.Kind = event.TurnDone
		e.Receipt = eventwire.CompletionReceiptEvent(w.Receipt)
		e.CheckpointTurn = w.CheckpointTurn
	default:
		return event.Event{}, false
	}
	if w.Level == "warn" {
		e.Level = event.LevelWarn
	}
	e.Code = w.Code
	if w.Tool != nil {
		e.Tool = event.Tool{
			ID: w.Tool.ID, Name: w.Tool.Name, Args: w.Tool.Args, ResolvedName: w.Tool.ResolvedName,
			CapabilityID: w.Tool.CapabilityID, Output: w.Tool.Output, Err: w.Tool.Err,
			ReadOnly: w.Tool.ReadOnly, Truncated: w.Tool.Truncated, DurationMs: w.Tool.DurationMs,
			StartedAt: w.Tool.StartedAt, EndedAt: w.Tool.EndedAt, Partial: w.Tool.Partial,
			ArgChars: w.Tool.ArgChars, Refreshed: w.Tool.Refreshed, ParentID: w.Tool.ParentID,
			AttemptID: w.Tool.AttemptID, FileDiff: event.FileDiff{Diff: w.Tool.Diff, Added: w.Tool.Added, Removed: w.Tool.Removed},
			SubagentRef: w.Tool.SubagentRef, SubagentStatus: w.Tool.SubagentStatus,
			SubagentErrorCode: w.Tool.SubagentErrorCode, SubagentRetryable: w.Tool.SubagentRetryable,
		}
	}
	if len(w.MemoryCitations) > 0 {
		e.MemoryCitations = make([]provider.MemoryCitation, 0, len(w.MemoryCitations))
		for _, citation := range w.MemoryCitations {
			e.MemoryCitations = append(e.MemoryCitations, provider.MemoryCitation{
				ID: citation.ID, Source: citation.Source, LineStart: citation.LineStart,
				LineEnd: citation.LineEnd, Note: citation.Note, Kind: citation.Kind,
			})
		}
	}
	if w.DecisionReceipt != nil {
		e.DecisionReceipt = &provider.DecisionReceipt{
			ID: w.DecisionReceipt.ID, Kind: w.DecisionReceipt.Kind, Tool: w.DecisionReceipt.Tool,
			Subject: w.DecisionReceipt.Subject, Outcome: w.DecisionReceipt.Outcome,
		}
	}
	return e, true
}

func displayMessagesFromProjection(projection turnevent.PendingProjection) []HistoryMessage {
	var planner displayTurnBuffer
	var executor displayTurnBuffer
	for _, envelope := range projection.Events {
		e, ok := displayEventFromEnvelope(envelope)
		if !ok {
			continue
		}
		buffer := &executor
		if strings.TrimSpace(e.Source) == event.UsageSourcePlanner {
			buffer = &planner
		}
		recordHistoryDisplayEvent(buffer, e)
	}
	out := planner.materialize()
	interrupted := projection.Status == event.TurnInterrupted || projection.Status == event.TurnRecoveryRequired
	if !interrupted {
		out = append(out, executor.resultMessages()...)
	}
	if interrupted {
		out = append(out, executor.materialize()...)
		if len(out) > 0 {
			out = append(out, HistoryMessage{
				Role: "notice", Level: "info", Code: event.NoticeCodeCancelledTurn,
				Content: "This turn was interrupted. Partial output is kept for reference; only completed tool pairs and a bounded recovery summary enter the next model turn. Inspect the workspace before continuing or reverting changes.",
			})
		}
	}
	return out
}

func recordHistoryToolDispatch(buffer *displayTurnBuffer, e event.Event) {
	if e.Tool.Partial || strings.TrimSpace(e.Tool.Name) == "" {
		return
	}
	hm := ensureDisplayAssistantForTool(buffer)
	resolvedReadOnly := e.Tool.ReadOnly
	call := HistoryToolCall{
		ID:               e.Tool.ID,
		Name:             e.Tool.Name,
		Arguments:        e.Tool.Args,
		ResolvedName:     e.Tool.ResolvedName,
		CapabilityID:     e.Tool.CapabilityID,
		ResolvedReadOnly: &resolvedReadOnly,
		Subject:          historyToolSubject(e.Tool.Name, e.Tool.Args),
		Summary:          historyToolSummary(e.Tool.Name, e.Tool.Args, ""),
		Diff:             e.Tool.Diff,
		Added:            e.Tool.Added,
		Removed:          e.Tool.Removed,
	}
	replaced := false
	if call.ID != "" {
		for i := range hm.message.ToolCalls {
			if hm.message.ToolCalls[i].ID == call.ID {
				hm.message.ToolCalls[i] = call
				replaced = true
				break
			}
		}
		if buffer.tools == nil {
			buffer.tools = map[string]string{}
		}
		buffer.tools[call.ID] = call.Name
	}
	if !replaced {
		hm.message.ToolCalls = append(hm.message.ToolCalls, call)
	}
}
