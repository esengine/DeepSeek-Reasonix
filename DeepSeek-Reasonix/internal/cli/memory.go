package cli

import (
	"fmt"

	"reasonix/internal/i18n"
)

// showMemory reports what memory is loaded and where it lives — the TUI analog
// of Claude Code's /memory. It surfaces the doc files and the auto-memory store
// path so the user can open and edit them directly, since the in-terminal UI
// doesn't shell out to an editor.
func (m *chatTUI) showMemory() {
	set := m.ctrl.Memory()
	if set == nil || set.Empty() {
		m.notice(i18n.M.MemoryNone)
		return
	}
	m.commitLine(renderMemory(m.width, set))
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
