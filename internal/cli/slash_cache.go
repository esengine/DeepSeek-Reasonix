package cli

import (
	"strings"

	"reasonix/internal/control"
)

// slashCompletionCache memoizes the two expensive completion snapshots: the
// slash catalog (commands, skills, prompts) and the arg-completion data
// (config models/providers, plugin state, MCP names, memory refs). Assembling
// the arg data runs several config loads plus a plugin-state read per keystroke
// otherwise, which reads as visible input lag on cold Linux caches (#9503).
type slashCompletionCache struct {
	items []compItem
	// argData is the memoized snapshot; argBuilt separates "not built" from a
	// snapshot taken while modelRef was still "".
	argData  control.ArgData
	argModel string
	// argContext identifies the structured command whose editing session owns
	// argData. It survives an empty filter result but is cleared by an explicit
	// dismissal, input reset, invalidation, or a different command.
	argContext string
	argBuilt   bool
}

func (m *chatTUI) ensureSlashCache() *slashCompletionCache {
	if m.slashCache == nil {
		m.slashCache = &slashCompletionCache{}
	}
	return m.slashCache
}

// slashItems returns the cached slash catalog. Rebuilds only after
// invalidateSlashCatalog — never on ordinary keystrokes.
func (m *chatTUI) slashItems() []compItem {
	if c := m.slashCache; c != nil && c.items != nil {
		return c.items
	}
	// Immutable snapshot so keystroke filtering never mutates shared state.
	items := m.buildSlashCatalog()
	out := make([]compItem, len(items))
	copy(out, items)
	m.ensureSlashCache().items = out
	return out
}

// slashArgDataSnapshot returns the memoized arg-completion data. It rebuilds
// when the active model changed or the cache was invalidated; filtering stays
// in-memory, so keystrokes inside an open arg popup cost no I/O.
func (m *chatTUI) slashArgDataSnapshot() control.ArgData {
	if c := m.slashCache; c != nil && c.argBuilt && c.argModel == m.modelRef {
		return c.argData
	}
	c := m.ensureSlashCache()
	c.argData = m.slashArgData()
	c.argModel = m.modelRef
	c.argBuilt = true
	return c.argData
}

func (m *chatTUI) cachedSlashArgItems(line string) ([]control.SlashItem, int, bool) {
	context := ""
	if end := strings.IndexAny(line, " \t"); end >= 0 {
		context = line[:end]
	}
	usedData := false
	items, from, applies := control.SlashArgItemsLazy(line, func() control.ArgData {
		usedData = true
		m.prepareSlashArgSnapshot(context)
		return m.slashArgDataSnapshot()
	})
	if !usedData {
		m.endSlashArgSnapshot()
	}
	return items, from, applies
}

// prepareSlashArgSnapshot keeps one stable snapshot for a structured argument
// editing session. Candidate filtering may temporarily hide the popup, so the
// command context—not menu visibility—owns the generation boundary.
func (m *chatTUI) prepareSlashArgSnapshot(context string) {
	c := m.ensureSlashCache()
	if context != "" && c.argContext == context {
		return
	}
	c.clearArgData()
	c.argContext = context
}

func (c *slashCompletionCache) clearArgData() {
	c.argData = control.ArgData{}
	c.argModel = ""
	c.argBuilt = false
}

func (m *chatTUI) endSlashArgSnapshot() {
	if c := m.slashCache; c != nil {
		c.clearArgData()
		c.argContext = ""
	}
}

func (m *chatTUI) endSlashArgSnapshotForKey(key string) string {
	switch key {
	case "esc", "ctrl+c", "super+c", "meta+c", "ctrl+enter", "enter":
		m.endSlashArgSnapshot()
	}
	return key
}

func (m *chatTUI) resetComposerInput() {
	m.input.Reset()
	m.endSlashArgSnapshot()
}

// invalidateSlashCatalog drops the cached catalog and arg data so the next
// slashItems/slashArgDataSnapshot call rebuilds them. Call from model switch,
// skill rescan, /reload-cmd, and any path that mutates
// commands/skills/host/extension actions.
func (m *chatTUI) invalidateSlashCatalog() {
	m.slashCache = nil
}
