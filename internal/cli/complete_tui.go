package cli

import (
	tea "charm.land/bubbletea/v2"

	"reasonix/internal/i18n"
)

// handleCompletionKey reports whether the menu consumed the key. Unhandled
// keys fall through to the composer, including Enter on a completed command
// and Escape when a running turn must also be canceled.
func (m *chatTUI) handleCompletionKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !m.completion.active {
		return nil, false
	}
	switch msg.String() {
	case "up", "ctrl+p":
		m.moveCompletion(-1)
		return nil, true
	case "down", "ctrl+n":
		m.moveCompletion(1)
		return nil, true
	case "tab", "enter":
		if name := m.selectedTheme(); msg.String() == "enter" && name != "" {
			m.input.Reset()
			m.completion = completion{}
			cmd := m.runThemeSubcommand("/theme " + name)
			return finalize(*m, []tea.Cmd{cmd}), true
		}
		if msg.String() == "enter" && (m.completionExactLabel() || m.completionBareOverlayCommand()) {
			m.completion = completion{}
			return nil, false
		}
		// Submit an already completed token instead of accepting it again
		// (/resume 1 still has /resume 10 as a prefix match).
		if msg.String() == "enter" && m.completionSelectedInsertPresent() {
			m.completion = completion{}
			return nil, false
		}
		m.acceptCompletion()
		return nil, true
	case "esc":
		previewing := m.selectedTheme() != ""
		m.completion = completion{}
		return nil, m.state != tuiRunning || previewing
	}
	return nil, false
}

func (m chatTUI) completionHint() string {
	if m.completion.kind == compAt {
		return i18n.M.CompHintFile
	}
	if m.selectedTheme() != "" {
		return i18n.M.ThemeHint
	}
	return i18n.M.CompHintSlash
}
