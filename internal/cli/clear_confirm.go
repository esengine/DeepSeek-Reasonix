package cli

import (
	"strings"
)

func (m *chatTUI) resetFreshContextView(clearTranscript bool) {
	m.finalizeStreamed()
	m.pending.Reset()
	m.reasoning.Reset()
	m.todoArgs = ""
	m.chooser = nil
	m.pendingApproval = nil
	m.bubblePending = false
	m.turnDiscarded = false
	if clearTranscript {
		m.transcript = nil
		m.wrappedLines = nil
		m.viewport.SetContent("")
	} else {
		m.commitLine("")
	}
	m.commitLine(strings.TrimRight(renderTUIBanner(m.label, "", m.width), "\n"))
	m.transcriptDirty = true
}
