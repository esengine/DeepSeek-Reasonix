package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"

	"reasonix/internal/control"
	"reasonix/internal/i18n"
	"reasonix/internal/skill"
)

// compKind distinguishes the two completion menus.
type compKind int

const (
	compSlash    compKind = iota // slash command names, while the line is a bare "/word"
	compSlashArg                 // a structured argument of a slash command (e.g. "/mcp remove <name>")
	compAt                       // @-references (files / MCP resources)
)

// compItem is one menu row: label shown, insert applied on accept, hint dimmed.
// descend marks a directory entry — accepting it fills the input and re-opens
// the menu one level deeper instead of closing.
type compItem struct {
	label   string
	insert  string
	hint    string
	descend bool
}

// completion is the live autocomplete menu state. Empty value = inactive.
// replaceFrom is the byte offset in the input where the completed token starts
// (0 for a slash line, the '@' index for an @-reference).
type completion struct {
	active      bool
	kind        compKind
	items       []compItem
	sel         int
	replaceFrom int
}

const (
	// maxCompRows caps how many menu rows show at once; the list windows around
	// the selection when longer.
	maxCompRows = 8
	// maxCompItems caps how many entries a single directory contributes, so a
	// pathologically large directory can't blow up the menu — we read only one
	// level (os.ReadDir), never the whole tree.
	maxCompItems = 200
)

// slashItems is the full set of slash commands offered for completion: the
// built-in verbs, custom commands, skills (each as "/<name>"), and MCP prompts.
func (m *chatTUI) slashItems() []compItem {
	items := []compItem{
		{label: "/compact", insert: "/compact ", hint: i18n.M.CmdCompact},
		{label: "/new", insert: "/new ", hint: i18n.M.CmdNew},
		{label: "/resume", insert: "/resume ", hint: i18n.M.CmdResume},
		{label: "/rewind", insert: "/rewind", hint: i18n.M.CmdRewind},
		{label: "/tree", insert: "/tree", hint: i18n.M.CmdTree},
		{label: "/branch", insert: "/branch ", hint: i18n.M.CmdBranch},
		{label: "/switch", insert: "/switch ", hint: i18n.M.CmdSwitchBranch},
		{label: "/mcp", insert: "/mcp ", hint: i18n.M.CmdMcp, descend: true},
		{label: "/model", insert: "/model ", hint: i18n.M.CmdModel, descend: true},
		{label: "/skill", insert: "/skill ", hint: i18n.M.CmdSkill, descend: true},
		{label: "/hooks", insert: "/hooks ", hint: i18n.M.CmdHooks, descend: true},
		{label: "/paste-image", insert: "/paste-image", hint: i18n.M.CmdPasteImage},
		{label: "/output-style", insert: "/output-style", hint: i18n.M.CmdOutputStyle},
		{label: "/verbose", insert: "/verbose", hint: i18n.M.CmdVerbose},
		{label: "/effort", insert: "/effort ", hint: i18n.M.CmdEffort, descend: true},
		{label: "/help", insert: "/help ", hint: i18n.M.CmdHelp},
		{label: "/memory", insert: "/memory ", hint: i18n.M.CmdMemory},
		{label: "/forget", insert: "/forget ", hint: i18n.M.CmdForget},
		{label: "/quit", insert: "/quit", hint: i18n.M.CmdQuit},
	}
	for _, c := range m.commands {
		items = append(items, compItem{label: "/" + c.Name, insert: "/" + c.Name + " ", hint: c.Description})
	}
	for _, s := range m.skills {
		hint := s.Description
		if s.RunAs == skill.RunSubagent {
			hint = "🧬 " + hint
		}
		items = append(items, compItem{label: "/" + s.Name, insert: "/" + s.Name + " ", hint: hint})
	}
	for _, p := range m.prompts() {
		items = append(items, compItem{label: "/" + p.Name, insert: "/" + p.Name + " ", hint: p.Description})
	}
	return items
}

// updateCompletion recomputes the menu from the current input: a slash menu
// while the line is a single "/word" token, or an @-reference menu while the
// token under the cursor is "@…".
func (m *chatTUI) updateCompletion() {
	val := m.input.Value()

	// An @-reference token under the cursor wins — it can appear mid-line, even
	// inside a slash command's arguments (e.g. "/review @file").
	if at, token, ok := activeAtToken(val); ok {
		if items := m.atItems(token); len(items) > 0 {
			m.setCompletion(compAt, items, at)
			return
		}
	}

	if strings.HasPrefix(val, "/") {
		if !strings.ContainsAny(val, " \t\n") {
			// Still naming the command itself.
			if items := filterByPrefix(m.slashItems(), val); len(items) > 0 {
				m.setCompletion(compSlash, items, 0)
				return
			}
		} else if items, from, ok := m.slashArgItems(val); ok && len(items) > 0 {
			// Past the command word — complete its structured arguments.
			m.setCompletion(compSlashArg, items, from)
			return
		}
	}

	m.completion = completion{}
}

// slashArgItems completes the arguments of a slash command (everything after the
// command word). It returns the menu items, the byte offset where the current
// token begins (replaceFrom, so accept replaces just that token), and whether
// anything applied. Only commands with structured arguments participate —
// currently /mcp; custom commands and MCP prompts take free-form template args,
// so they yield nothing.
func (m *chatTUI) slashArgItems(val string) ([]compItem, int, bool) {
	if items, from, ok := m.branchArgItems(val); ok {
		return items, from, len(items) > 0
	}
	if items, from, ok := m.resumeArgItems(val); ok {
		return items, from, len(items) > 0
	}
	// Delegate to the shared completion logic so the chat TUI and the desktop
	// offer identical sub-command hints. We supply the data from the TUI's own
	// cached lists (no live controller needed), build the items, and adapt them
	// to compItem.
	data := control.ArgData{
		Skills:       m.skills,
		ModelRefs:    modelRefs(),
		CurrentModel: m.modelRef,
	}
	if m.ctrl != nil {
		data.ConfiguredMCP = m.ctrl.ConfiguredMCPNames()
		data.DisconnectedMCP = m.ctrl.DisconnectedMCPNames()
	}
	if m.host != nil {
		data.ServerNames = m.host.ServerNames()
	}
	items, from := control.SlashArgItems(val, data)
	if len(items) == 0 {
		return nil, 0, false
	}
	out := make([]compItem, len(items))
	for i, it := range items {
		out[i] = compItem{label: it.Label, insert: it.Insert, hint: it.Hint, descend: it.Descend}
	}
	return out, from, true
}

func (m *chatTUI) branchArgItems(val string) ([]compItem, int, bool) {
	cmdEnd := strings.IndexAny(val, " \t")
	if cmdEnd < 0 || val[:cmdEnd] != "/switch" {
		return nil, 0, false
	}
	from := strings.LastIndexAny(val, " \t") + 1
	prior := strings.Fields(val[:from])
	if len(prior) != 1 || m.ctrl == nil {
		return nil, from, true
	}
	branches, err := m.ctrl.Branches()
	if err != nil {
		return nil, from, true
	}
	cur := strings.ToLower(val[from:])
	var out []compItem
	for _, b := range branches {
		label := b.ID
		if cur != "" && !strings.HasPrefix(strings.ToLower(label), cur) &&
			!strings.HasPrefix(strings.ToLower(b.Name), cur) {
			continue
		}
		hint := b.Name
		if hint == "" {
			hint = b.Preview
		}
		if hint != "" {
			hint = fmt.Sprintf("%d turns · %s", b.Turns, hint)
		}
		out = append(out, compItem{label: label, insert: label, hint: hint})
	}
	return out, from, true
}

// setCompletion installs items, preserving the selection index only while the
// same menu kind stays open.
func (m *chatTUI) setCompletion(kind compKind, items []compItem, replaceFrom int) {
	sel := 0
	if m.completion.active && m.completion.kind == kind && m.completion.sel < len(items) {
		sel = m.completion.sel
	}
	m.completion = completion{active: true, kind: kind, items: items, sel: sel, replaceFrom: replaceFrom}
}

// filterByPrefix keeps items whose label starts with prefix (case-insensitive).
func filterByPrefix(items []compItem, prefix string) []compItem {
	lp := strings.ToLower(prefix)
	var out []compItem
	for _, it := range items {
		if strings.HasPrefix(strings.ToLower(it.label), lp) {
			out = append(out, it)
		}
	}
	return out
}

// activeAtToken finds the @-reference token ending at the cursor (assumed at the
// input's end). The '@' must start the line or follow whitespace, so emails
// like "a@b" don't trigger it. Returns the '@' offset and the text after it.
func activeAtToken(val string) (int, string, bool) {
	for i := len(val) - 1; i >= 0; i-- {
		switch val[i] {
		case ' ', '\t', '\n':
			return 0, "", false // hit whitespace before an '@' → no active token
		case '@':
			if i == 0 || val[i-1] == ' ' || val[i-1] == '\t' || val[i-1] == '\n' {
				return i, val[i+1:], true
			}
			return 0, "", false
		}
	}
	return 0, "", false
}

// atItems builds the @-reference menu for a token. A "server:uri" token whose
// server is connected lists that server's MCP resources; otherwise the token is
// a path and we list one directory level (never a recursive walk), plus — at the
// top level — any matching MCP resources.
func (m *chatTUI) atItems(token string) []compItem {
	if i := strings.Index(token, ":"); i > 0 && m.isMCPServer(token[:i]) {
		return m.resourceItems(token[:i], token[i+1:])
	}
	return m.fileItems(token)
}

// fileItems lists one directory level for a path token. dir is the part up to
// the last '/', frag the part after; entries of dir starting with frag are
// offered (directories descend, files complete). Hidden entries are skipped
// unless frag starts with '.'. Top-level tokens also surface MCP resources.
func (m *chatTUI) fileItems(token string) []compItem {
	dir, frag := splitPathToken(token)
	readDir := dir
	if readDir == "" {
		readDir = "."
	}
	entries, err := os.ReadDir(readDir)
	if err != nil {
		entries = nil
	}
	// Directories first, then files; ReadDir is already name-sorted.
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].IsDir() && !entries[j].IsDir()
	})

	showHidden := strings.HasPrefix(frag, ".")
	var items []compItem
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, frag) {
			continue
		}
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir() {
			items = append(items, compItem{label: name + "/", insert: "@" + dir + name + "/", hint: "dir", descend: true})
		} else {
			items = append(items, compItem{label: name, insert: "@" + dir + name})
		}
		if len(items) >= maxCompItems {
			break
		}
	}

	// At the top level (still naming the first segment) MCP resources share the
	// '@' namespace, so offer the matching ones too.
	if !strings.Contains(token, "/") {
		items = append(items, m.resourceItems("", token)...)
	}
	return items
}

// splitPathToken splits a path token into (dir, frag): dir keeps its trailing
// slash ("internal/" ), frag is the segment being typed.
func splitPathToken(token string) (dir, frag string) {
	if i := strings.LastIndex(token, "/"); i >= 0 {
		return token[:i+1], token[i+1:]
	}
	return "", token
}

// isMCPServer reports whether name is a connected MCP server.
func (m *chatTUI) isMCPServer(name string) bool {
	if m.host == nil {
		return false
	}
	for _, s := range m.host.ServerNames() {
		if s == name {
			return true
		}
	}
	return false
}

// resourceItems lists MCP resources as @server:uri completions. When server is
// "" (top level) it matches by the whole "server:uri" prefix; otherwise it lists
// the named server's resources filtered by the uri prefix.
func (m *chatTUI) resourceItems(server, frag string) []compItem {
	if m.host == nil {
		return nil
	}
	var items []compItem
	for _, r := range m.host.Resources() {
		ref := r.Server + ":" + r.URI
		switch {
		case server == "":
			if !strings.HasPrefix(ref, frag) {
				continue
			}
		case r.Server == server:
			if !strings.HasPrefix(r.URI, frag) {
				continue
			}
		default:
			continue
		}
		label := r.Name
		if label == "" {
			label = "resource"
		}
		items = append(items, compItem{label: "@" + ref, insert: "@" + ref, hint: label})
	}
	return items
}

// moveCompletion advances the selection by delta, wrapping around.
func (m *chatTUI) moveCompletion(delta int) {
	n := len(m.completion.items)
	if n == 0 {
		return
	}
	m.completion.sel = ((m.completion.sel+delta)%n + n) % n
}

// acceptCompletion applies the selected item to the input. A directory descends
// (the input is filled and the menu re-opens one level deeper); anything else
// completes and closes the menu.
func (m *chatTUI) acceptCompletion() {
	if m.completion.sel >= len(m.completion.items) {
		m.completion = completion{}
		return
	}
	it := m.completion.items[m.completion.sel]
	val := m.input.Value()
	rf := m.completion.replaceFrom
	if rf > len(val) {
		rf = len(val)
	}
	m.input.SetValue(val[:rf] + it.insert)
	m.input.CursorEnd()
	if it.descend {
		m.updateCompletion() // re-list the directory we just descended into
		return
	}
	m.completion = completion{}
}

var compSelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("173")).Bold(true)

// renderCompletion draws the menu above the input box: matching items, windowed
// around the selection, the current row highlighted, hints dimmed.
func (m chatTUI) renderCompletion() string {
	if !m.completion.active || len(m.completion.items) == 0 {
		return ""
	}
	items := m.completion.items
	start := 0
	if len(items) > maxCompRows {
		start = m.completion.sel - maxCompRows/2
		if start < 0 {
			start = 0
		}
		if start > len(items)-maxCompRows {
			start = len(items) - maxCompRows
		}
	}
	end := start + maxCompRows
	if end > len(items) {
		end = len(items)
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		it := items[i]
		if i == m.completion.sel {
			b.WriteString(accent("› ") + compSelStyle.Render(it.label))
		} else {
			b.WriteString("  " + it.label)
		}
		if it.hint != "" {
			b.WriteString("  " + dim(it.hint))
		}
		b.WriteByte('\n')
	}
	// A key-hint footer so users discover Tab — many won't know it accepts a
	// completion, let alone descends into a folder.
	hint := i18n.M.CompHintSlash
	if m.completion.kind == compAt {
		hint = i18n.M.CompHintFile
	}
	b.WriteString(dim(hint))
	return b.String()
}
