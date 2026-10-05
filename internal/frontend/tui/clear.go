package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
	"reasonix/internal/frontend/termrender"
)

// clearConfirm is the /clear question: the transcript is deleted, not saved,
// so it is asked first. The cursor starts on Cancel.
type clearConfirm struct {
	clear bool
}

// askClear opens the question, or clears at once where approvals are skipped:
// YOLO is a standing answer to confirmations like this one.
func (m *model) askClear() tea.Cmd {
	if m.status.ToolApprovalMode == "yolo" {
		return m.clearContext()
	}
	m.clearing = &clearConfirm{}
	return nil
}

func (m *model) clearKey(k string) (tea.Cmd, bool) {
	c := m.clearing
	if c == nil {
		return nil, false
	}
	switch k {
	case "up", "down", "left", "right", "j", "k", "tab", "shift+tab":
		c.clear = !c.clear
	case "y", "Y":
		return m.clearContext(), true
	case "n", "N", "esc", "ctrl+c":
		m.clearing = nil
	case "enter":
		if c.clear {
			return m.clearContext(), true
		}
		m.clearing = nil
	}
	return nil, true
}

// clearContext hands /clear to the kernel and starts the screen over under the
// banner; the kernel's notice says how it went.
func (m *model) clearContext() tea.Cmd {
	m.clearing = nil
	m.resetScreen()
	send := m.call("clear", func(ctx context.Context) error { return m.client.Submit(ctx, "/clear") })
	return tea.Sequence(tea.ClearScreen, m.greet(), send)
}

func (m *model) clearPanel() []string {
	lines := []string{
		i18n.M.SlashClearPrompt,
		termrender.Dim("This deletes the current transcript from local history and keeps only the system prompt."),
		"",
		rowLine(m.clearing.clear, 1, "", "Clear", false),
		rowLine(!m.clearing.clear, 2, "", "Cancel", false),
	}
	out := panel(lines, m.width, accentEdge)
	return append(out, " "+termrender.Dim("Enter confirm · y clear · n/Esc cancel"), out[0])
}
