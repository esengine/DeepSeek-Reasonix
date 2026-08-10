package cli

import (
	tea "charm.land/bubbletea/v2"

	"reasonix/internal/memory"
)

// showContextReport prints the window, the thresholds derived from it, and how
// the last maintenance pass ended. It commits a block rather than a notice
// because the numbers are meant to be compared with each other.
func (m *chatTUI) showContextReport(input string) tea.Cmd {
	m.echoLocalCommand(input)
	summary, detail := m.ctrl.ContextReport()
	if detail != "" {
		summary += "\n" + detail
	}
	m.commitLine(summary)
	return nil
}

func (m *chatTUI) rememberNote(note string) {
	if note == "" {
		m.notice("nothing to remember")
		return
	}
	path, err := m.ctrl.QuickAdd(memory.ScopeProject, note)
	if err != nil {
		m.notice("memory: " + err.Error())
		return
	}
	m.notice("remembered → " + path)
}
