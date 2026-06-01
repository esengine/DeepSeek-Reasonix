package cli

import "fmt"

// showMemory reports what memory is loaded and where it lives — the TUI analog
// of Claude Code's /memory. It surfaces the doc files and the auto-memory store
// path so the user can open and edit them directly, since the in-terminal UI
// doesn't shell out to an editor.
func (m *chatTUI) showMemory() {
	set := m.ctrl.Memory()
	if set == nil || set.Empty() {
		m.notice("memory: none — add with “#<note>” or create REASONIX.md in the project root")
		return
	}
	m.notice("memory loaded:")
	for _, d := range set.Docs {
		m.notice(fmt.Sprintf("  • %s (%s)", d.Path, d.Scope))
	}
	if facts := set.Store.List(); len(facts) > 0 {
		m.notice("  saved memories (delete with “/forget <name>”):")
		for _, f := range facts {
			label := f.Title
			if label == "" {
				label = f.Description
			}
			m.notice(fmt.Sprintf("    • %s — %s", f.Name, label))
		}
		m.notice("  stored under " + set.Store.Dir)
	}
	m.notice("edit doc files or use “#<note>”; doc edits apply next session")
}

// forgetMemory deletes a saved auto-memory by name (the slug shown in /memory).
// It is the manual counterpart to the model's `forget` tool.
func (m *chatTUI) forgetMemory(name string) {
	if name == "" {
		m.notice("usage: /forget <name> — the slug shown under “saved memories” in /memory")
		return
	}
	if err := m.ctrl.ForgetMemory(name); err != nil {
		m.notice(fmt.Sprintf("forget: %v", err))
		return
	}
	m.notice("forgot memory: " + name)
}
