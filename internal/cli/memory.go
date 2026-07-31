package cli

import (
	"fmt"
	"strings"

	"reasonix/internal/control"
	"reasonix/internal/i18n"
)

// showMemory reports what memory is loaded and where it lives — the TUI analog
// of Claude Code's /memory. It surfaces the doc files and the auto-memory store
// path so the user can open and edit them directly, since the in-terminal UI
// doesn't shell out to an editor.
func (m *chatTUI) showMemory(input string) {
	args := strings.TrimSpace(strings.TrimPrefix(input, "/memory"))
	if args == "" {
		m.commitLine(renderMemory(m.width, m.ctrl.Memory()))
		return
	}
	m.commitLine(viewProtectLines(control.MemoryCommandText(m.ctrl, args), m.width))
}

// forgetMemory deletes a saved auto-memory by name (the slug shown in /memory).
// It is the manual counterpart to the model's `forget` tool.
func (m *chatTUI) forgetMemory(name string) {
	if name == "" {
		m.notice(i18n.M.ForgetUsage)
		return
	}
	if err := m.ctrl.ForgetMemory(name); err != nil {
		m.notice(fmt.Sprintf("forget: %v", err))
		return
	}
	m.notice(fmt.Sprintf(i18n.M.ForgetDoneFmt, name))
}
