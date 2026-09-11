package cli

import (
	"time"

	"reasonix/internal/event"

	tea "charm.land/bubbletea/v2"
)

// startTurn commits the user bubble to scrollback, resets the turn accumulator,
// and kicks off the controller turn. `sent` goes to the model uncomposed (the
// controller frames it with any plan marker); `displayed` is what the transcript
// shows, and `restore` is what Esc puts back while the bubble is still deferred.
func (m *chatTUI) startTurn(sent, displayed, restore string) tea.Cmd {
	return m.startTurnWithRaw(sent, displayed, restore, sent)
}

// startTurnWithRaw is startTurn plus an explicit unresolved user prompt. This
// keeps reference-expanded model input separate from the text shown/restored by
// the frontend.
func (m *chatTUI) startTurnWithRaw(sent, displayed, restore, raw string) tea.Cmd {
	return m.startControllerTurnWithQueue(displayed, restore, raw, func() { m.ctrl.SendWithRaw(sent, raw) })
}

// startControllerTurn owns the TUI-side turn setup for controller entry points.
// Most prompts use SendWithRaw; slash-invoked skills use SubmitDisplay so the
// controller can choose inline vs isolated subagent execution from the live
// skill's RunAs metadata without the TUI reimplementing that policy.
func (m *chatTUI) startControllerTurn(displayed, restore string, start func()) tea.Cmd {
	return m.startControllerTurnWithQueue(displayed, restore, displayed, start)
}

func (m *chatTUI) startControllerTurnWithQueue(displayed, restore, queued string, start func()) tea.Cmd {
	if m.takeover != nil && m.takeover.Reclaiming() {
		m.notice("the remote side is taking this session back; new input is disabled")
		return nil
	}
	// The composer can read idle while the controller already runs a
	// dispatched queued follow-up (TurnStarted not yet ingested): queue rather
	// than race the admission guard's silent drop (#9575).
	if m.ctrl != nil && m.ctrl.Running() {
		receipt, err := m.enqueueFollowup(displayed, queued)
		if err != nil {
			m.notice("queue: " + err.Error())
			if m.input.Value() == "" {
				m.input.SetValue(restore)
				m.growInputToFit()
			}
			return nil
		}
		m.notice("durable follow-up queued #" + shortID(receipt.ItemID) + " — will run when idle")
		m.clearQueuedPastes(restore)
		return nil
	}
	// Flush any half-streamed leftover before the new turn (defensive).
	m.commitReasoning()
	m.commitPending()

	// Echo the user bubble to scrollback now so it appears the instant Enter is
	// pressed, not when the first packet lands: Esc before the reply pops it
	// back off and restores the text, leaving nothing stranded.
	m.pendingRestore = restore
	m.pendingPastes = m.pasteLabelsIn(restore)
	m.bubbleStartIdx = len(m.transcript)
	m.commitLine("") // blank line separating turns
	m.commitTranscriptSource(transcriptSource{
		kind: transcriptSourceUser, raw: displayed, planMode: m.planMode,
	})
	m.bubblePending = true
	m.turnDiscarded = false

	m.state = tuiRunning
	m.runStart = time.Now()
	m.elapsed = 0
	m.turnTokens = 0
	// The controller owns the run goroutine, its context, and cancellation; it
	// streams events to eventCh and emits TurnDone when the turn settles.
	m.noteWatchdogRunning()
	start()
	return m.startRunningTicks()
}

// confirmBubbleSent marks the already-echoed user bubble as really sent once a
// turn's first response packet arrives, so Esc no longer un-sends it (it cancels
// the stream instead). Also called defensively at turn end. A no-op once confirmed.
func (m *chatTUI) confirmBubbleSent() {
	if !m.bubblePending {
		return
	}
	m.bubblePending = false
	m.pendingRestore = ""
}

// drainAgentEvents ingests the events already buffered behind the first one:
// the producing goroutine has exited (a Cmd reads the channel once), so one
// re-wrap covers the whole batch instead of one per event.
type agentEventDrain struct {
	turnDone, gitMaybeChanged bool
	cmds                      []tea.Cmd
}

func (m *chatTUI) consumeAgentEvent(e event.Event, drained *agentEventDrain) {
	// Record before ingest so TurnDone still counts as an active heartbeat.
	m.noteWatchdogHeartbeat(watchdogAgentSource(e.Kind))
	if e.Kind == event.TurnStarted {
		if cmd := m.noteControllerTurnStarted(); cmd != nil {
			drained.cmds = append(drained.cmds, cmd)
		}
	}
	m.ingestEvent(e)
	drained.turnDone = drained.turnDone || e.Kind == event.TurnDone
	drained.gitMaybeChanged = drained.gitMaybeChanged || e.Kind == event.ToolResult && !e.Tool.ReadOnly
}

func (m *chatTUI) drainAgentEvents(first event.Event) agentEventDrain {
	var drained agentEventDrain
	m.consumeAgentEvent(first, &drained)
	for range maxEventDrain {
		select {
		case e2 := <-m.eventCh:
			m.consumeAgentEvent(e2, &drained)
		default:
			return drained
		}
	}
	return drained
}

// noteControllerTurnStarted enters running state for a turn the TUI did not
// submit itself — the controller auto-dispatching a queued follow-up. Without
// it the composer reads as ready while the dispatched turn streams, so an
// Enter races the dispatch (silently dropped, or preempting the queue) and the
// elapsed-tick heartbeat chain stays dead (#9575).
func (m *chatTUI) noteControllerTurnStarted() tea.Cmd {
	if m.state == tuiRunning {
		return nil
	}
	m.state = tuiRunning
	m.runStart = time.Now()
	m.elapsed = 0
	m.turnTokens = 0
	m.noteWatchdogRunning()
	return m.startRunningTicks()
}

func (m *chatTUI) startRunningTicks() tea.Cmd {
	m.elapsedTickGeneration++
	return tea.Batch(m.spinner.Tick, elapsedTick(m.elapsedTickGeneration))
}

func (m *chatTUI) clearQueuedPastes(restore string) {
	labels := m.pasteLabelsIn(restore)
	if len(labels) == 0 {
		return
	}
	queued := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		queued[label] = struct{}{}
	}
	kept := m.pastedBlocks[:0]
	for _, block := range m.pastedBlocks {
		if _, ok := queued[block.label]; !ok {
			kept = append(kept, block)
		}
	}
	m.pastedBlocks = kept
}
