package cli

import (
	"fmt"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"strings"
	"time"
)

func (m *chatTUI) ingestEvent(e event.Event) {
	if m.ingestPreflight(e) {
		return
	}
	switch e.Kind {
	case event.Reasoning:
		m.ingestReasoning(e)
	case event.Text:
		m.ingestText(e)
	case event.Message:
		m.ingestMessage(e)
	case event.ToolDispatch:
		m.ingestToolDispatch(e)
	case event.ToolProgress:
		m.ingestToolProgress(e)
	case event.ToolResult:
		m.ingestToolResult(e)
	case event.Usage:
		m.ingestUsage(e)
	case event.ReadStatus:
		m.ingestReadStatus(e)
	case event.TurnPhase:
		m.ingestTurnPhase(e)
	case event.CompletionSummary:
		m.ingestCompletionSummary(e)
	case event.Notice:
		m.ingestNotice(e)
	case event.GuardianAssessment:
		m.ingestGuardianAssessment(e)
	case event.ExtensionStatus:
		m.ingestExtensionStatus(e)
	case event.ExtensionSurface:
		m.ingestExtensionSurface(e)
	case event.CompactionStarted:
		m.ingestCompactionStarted(e)
	case event.CompactionDone:
		m.ingestCompactionDone(e)
	case event.Phase:
		m.ingestPhase(e)
	case event.ApprovalRequest:
		m.ingestApprovalRequest(e)
	case event.AskRequest:
		m.ingestAskRequest(e)
	case event.MCPInteractionRequest:
		m.ingestMCPInteractionRequest(e)
	case event.MCPSurfaceReady:
		m.ingestMCPSurfaceReady(e)
	case event.TurnDone:
		m.ingestTurnDone(e)
	}
}

func (m *chatTUI) ingestReasoning(e event.Event) {
	if m.nativeScrollback {
		if !m.reasoningNative {
			m.thinkStart = time.Now()
			m.reasoningNative = true
		}
		m.streamReasoning(e.Text)
		return
	}
	if m.reasoningLineIdx < 0 {
		// Show the marker plus a live text block the moment thinking starts; the
		// text streams in below it and the block collapses to "thought for Ns"
		// when it closes (kept expanded only in verbose mode).
		m.commitSpacer()
		m.thinkStart = time.Now()
		m.reasoningLineIdx = len(m.transcript)
		m.commitLine(dim("  ▎ " + i18n.M.ChatThinking))
		m.reasoningTextIdx = len(m.transcript)
		m.commitLine("")
		m.reasoningView = m.reasoningView[:0]
	}
	m.streamReasoning(e.Text)
}

func (m *chatTUI) ingestText(e event.Event) {
	m.commitReasoningBeforeAnswer()
	m.pending.WriteString(e.Text)
	m.streamAnswer()
}

func (m *chatTUI) ingestMessage(e event.Event) {
	// The answer stream is complete — freeze reasoning + the markdown answer.
	// Message.Text is the canonical display text (protocol markers already
	// stripped at emission), so it replaces the raw streamed accumulation.
	if e.Text != "" && m.pending.Len() > 0 {
		m.pending.Reset()
		m.pending.WriteString(e.Text)
	}
	m.writeSearchFootnotes()
	m.commitReasoning()
	m.commitPending()
}

func (m *chatTUI) ingestToolDispatch(e event.Event) {
	// The early (partial) dispatch only carries the name — the full dispatch
	// with args prints the line. Same-ID preview refreshes are ignored because
	// native scrollback cannot replace an already-printed diff card.
	if e.Tool.Partial || e.Tool.Refreshed {
		return
	}
	m.finalizeStreamed()
	switch e.Tool.Name {
	case "todo_write":
		// The result decides whether this list becomes canonical; dispatch only
		// means the model asked for an update.
	case planApprovalTool:
		// No longer a tool, but guard anyway: the plan is the assistant's reply.
	default:
		m.commitSpacer()
		if block := diffBlock(e.Tool.Name, e.Tool.Args, e.Tool.FileDiff, m.width, m.diffMaxLines); block != nil {
			for _, ln := range block {
				m.commitLine(ln)
			}
			return
		}
		m.commitTranscriptSource(transcriptSource{
			kind: transcriptSourceToolCard, raw: e.Tool.Name, aux: e.Tool.Args,
		})
		m.beginToolRunning(e.Tool.ID)
	}
}

func (m *chatTUI) ingestToolProgress(e event.Event) {
	if event.IsSubagentProgressName(e.Tool.Name) {
		m.streamSubagentProgress(e.Tool)
		return
	}
	// Unknown names in the reserved namespace may come from a newer agent.
	// Keep them out of ordinary tool output even though this CLI cannot render
	// their payload yet.
	if event.IsReservedSubagentProgressName(e.Tool.Name) {
		return
	}
	m.streamToolOutput(e.Tool.ID, e.Tool.Output)
}

func (m *chatTUI) ingestToolResult(e event.Event) {
	// A successful result is silent (it only feeds the model); a blocked/failed
	// call surfaces a red card. Pass the final output so collapseToolOutput has
	// a last-resort line count when live state was already reset.
	m.collapseFinalToolOutput(e.Tool)
	if e.Tool.Name == "todo_write" && e.Tool.Err == "" {
		m.todoArgs = e.Tool.Args
	}
	m.rememberSearchResult(e.Tool)
	if e.Tool.Err != "" {
		m.finalizeStreamed()
		label := shellToolDisplayName(e.Tool.Name, e.Tool.Execution)
		detail := shellFailureDetail(e.Tool.Execution)
		errText := e.Tool.Err
		if detail != "" {
			errText = detail + " · " + errText
		}
		m.commitLine("  " + red("●") + " " + bold(label) + " " + red("⊘ "+errText))
	}
}

func (m *chatTUI) ingestUsage(e event.Event) {
	if e.Usage != nil {
		m.turnTokens += e.Usage.CompletionTokens
	}
	m.addSessionCostQuote(e.CostQuote)
	if m.showTurnUsage {
		if line := renderQuotedTurnReceipt(e.Usage, e.CostQuote, e.CacheDiagnostics); line != "" {
			m.finalizeStreamed()
			m.commitSpacer()
			m.commitTranscriptSource(transcriptSource{kind: transcriptSourceTurnReceipt, raw: line})
		}
	}
}

func (m *chatTUI) ingestReadStatus(e event.Event) {
	m.ingest(e.ReadStatus)
}

func (m *chatTUI) ingestTurnPhase(e event.Event) {
	// Content-free host phase for the live status line only.
	if phase := strings.TrimSpace(string(e.PhaseName)); phase != "" {
		m.turnPhase = phase
	} else if phase := strings.TrimSpace(e.Text); phase != "" {
		m.turnPhase = phase
	}
}

func (m *chatTUI) ingestCompletionSummary(e event.Event) {
	if e.Completion != nil {
		if completionSummaryNeedsAttention(e.Completion, m.ctrlQualityFloor()) {
			m.finalizeStreamed()
			m.commitLine(fmt.Sprintf("  ! %s", completionSummaryWarning(e.Completion)))
		}
		if m.showReasoning {
			m.finalizeStreamed()
			m.commitLine(dim("  · " + formatCompletionSummaryLine(e.Completion)))
		}
	}
}

func (m *chatTUI) ingestNotice(e event.Event) {
	glyph := "·"
	if e.Level == event.LevelWarn {
		glyph = "!"
	}
	m.finalizeStreamed()
	m.commitLine(fmt.Sprintf("  %s %s", glyph, e.Text))
}

func (m *chatTUI) ingestGuardianAssessment(e event.Event) {
	m.finalizeStreamed()
	g := e.Guardian
	line := fmt.Sprintf("Guardian %s · %s", g.Outcome, g.Tool)
	if g.Subject != "" {
		line += " · " + truncateSubject(g.Subject, m.width)
	}
	if g.RiskLevel != "" {
		line += " · risk=" + g.RiskLevel
	}
	if g.UserAuthorization != "" {
		line += " · authorization=" + g.UserAuthorization
	}
	if g.Rationale != "" {
		line += " · " + g.Rationale
	}
	if g.Outcome == "deny" {
		m.commitLine("  ! " + line)
	} else {
		m.commitLine("  · " + line)
	}
}

func (m *chatTUI) ingestExtensionStatus(e event.Event) {
	// One-line status contribution from an extension sidecar — a
	// severity-aware notice line, like event.Notice.
	if line := extensionStatusLine(e.Extension); line != "" {
		m.finalizeStreamed()
		m.commitLine(line)
	}
}

func (m *chatTUI) ingestExtensionSurface(e event.Event) {
	// A published card/form renders as a transcript card; a notification
	// renders as a notice line. Form fields themselves arrive through the
	// Ask machinery (the hub translates them), so no dialog work here.
	m.finalizeStreamed()
	if e.Extension != nil && e.Extension.Notification != nil {
		if line := extensionNotificationLine(e.Extension); line != "" {
			m.commitLine(line)
		}
		return
	}
	for _, ln := range extensionSurfaceLines(e.Extension, m.width) {
		m.commitLine(ln)
	}
}

func (m *chatTUI) ingestCompactionStarted(e event.Event) {
	m.finalizeStreamed()
	m.commitLine(dim("  ⋯ " + i18n.M.CompactionWorking))
}

func (m *chatTUI) ingestCompactionDone(e event.Event) {
	// An aborted pass carries no summary; the accompanying Notice (auto) or
	// compactDoneMsg error (manual) explains why, so don't draw an empty card.
	if e.Compaction.Summary == "" {
		return
	}
	m.finalizeStreamed()
	for _, ln := range compactionCardLines(e.Compaction) {
		m.commitLine(ln)
	}
}

func (m *chatTUI) ingestPhase(e event.Event) {
	m.finalizeStreamed()
	m.commitLine(fmt.Sprintf("[%s]", e.Text))
}

func (m *chatTUI) ingestApprovalRequest(e event.Event) {
	// The controller's run goroutine is blocked in the gate awaiting this
	// decision; the banner shows it in View and key input answers it via
	// ctrl.Approve. At most one prompt is outstanding, so a field holds it.
	a := e.Approval
	m.pendingApproval = &a
	m.approvalSelection = 0
	if isRecoveryPlanChangeApproval(&a) {
		// A plan decision must start neutral: Enter alone cannot make Auto's
		// strategy/scope choice for the user.
		m.approvalSelection = -1
	}
}

func (m *chatTUI) ingestAskRequest(e event.Event) {
	// The `ask` tool raised a question card; the run goroutine blocks until
	// ctrl.AnswerQuestion resolves it. Keys drive the card while it's set.
	m.finalizeStreamed()
	m.chooser = newChooser(e.Ask)
}

func (m *chatTUI) ingestMCPInteractionRequest(e event.Event) {
	m.startElicit(e.MCPInteraction)
}

func (m *chatTUI) ingestMCPSurfaceReady(e event.Event) {
	// Prompts/resources may have arrived after connect; refresh host and
	// drop the slash catalog so /prompt names reappear without a restart.
	m.refreshHostAndInvalidateSlashCatalog()
	m.refreshMCPManager()
}

func (m *chatTUI) ingestTurnDone(e event.Event) {
	m.readStatusState = readStatusState{}
	m.clearElicitCard()
	// The turn settled — freeze anything still streaming, surface a real error,
	// and gate a plan-mode proposal on the user's approval. Autosave already
	// happened in Controller, so frontends share the activity-time semantics.
	m.writeSearchFootnotes()
	m.commitReasoning()
	m.commitPending()
	// The bubble was echoed on Enter and an un-sent turn is swallowed above
	// (turnDiscarded), so any turn reaching here keeps its bubble in scrollback;
	// just clear the un-sendable flag.
	m.confirmBubbleSent()
	m.state = tuiIdle
	m.turnPhase = ""
	m.noteWatchdogIdle()
	m.queueEditCursor, m.queueEditDraft = -1, ""
	m.clearSubmittedPastes()
	m.commitTurnPauseNotice(e)
	m.commitReceipt(e.Receipt)
	// Long turns on Windows ConPTY often drop mouse tracking; re-arm on
	// the next frame so wheel keeps scrolling the transcript (#7583).
	m.wantMouseReenable = true
	// Plan-mode approval is now driven by the controller (it emits an
	// ApprovalRequest when a plan-mode turn produces a proposal), so there's
	// nothing to detect here.
}

func (m *chatTUI) ingestPreflight(e event.Event) bool {
	if e.Kind == event.Retrying {
		m.setRecoveryStatus(e)
		return true
	}
	if e.Kind == event.StreamAttempt {
		// Clear speculative presentation when an attempt is discarded.
		if e.StreamAttempt.Action == event.StreamAttemptDiscard {
			m.toolPartial = ""
			m.toolTail = nil
			m.toolStreamIdx = -1
			m.toolLineCount = 0
			m.recordRecoveryDiscard(e.StreamAttempt.Reason)
		}
		return true
	}
	// Any other event means the connection got past the retry window (or the turn
	// ended), so the transient "retrying" indicator clears.
	m.clearRecoveryStatus()
	if m.turnDiscarded {
		// The turn was un-sent (Esc before any packet); swallow whatever was already
		// buffered for it until it settles, so nothing lands in scrollback.
		if e.Kind == event.TurnDone {
			m.turnDiscarded = false
			m.state = tuiIdle
			m.noteWatchdogIdle()
		}
		return true
	}
	// The first packet of any kind means the server replied — confirm the send so
	// Esc cancels the stream instead of un-sending. TurnStarted is local (emitted
	// before the request) and TurnDone is handled in its own case.
	if e.Kind != event.TurnStarted && e.Kind != event.TurnDone {
		m.confirmBubbleSent()
	}
	return false
}
