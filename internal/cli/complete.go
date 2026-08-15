package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	rw "github.com/mattn/go-runewidth"

	"reasonix/internal/control"
	"reasonix/internal/fileref"
	"reasonix/internal/i18n"
	"reasonix/internal/plugin"
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
// replaceFrom/replaceTo are byte offsets of the token span that accept replaces
// (half-open [replaceFrom, replaceTo)). For a bare slash name, replaceFrom is 0
// and replaceTo is len(value). For @-refs, replaceFrom is the '@' and replaceTo
// is the first unescaped whitespace after the token (or end of input).
type completion struct {
	active      bool
	kind        compKind
	items       []compItem
	sel         int
	replaceFrom int
	replaceTo   int
}

const (
	// maxCompRows caps how many menu rows show at once; the list windows around
	// the selection when longer.
	maxCompRows = 8
	// maxCompItems caps how many entries a single directory contributes, so a
	// pathologically large directory can't blow up the menu — we read only one
	// level (os.ReadDir), never the whole tree.
	maxCompItems = 200
	// maxFileSearchItems caps basename search results for bare @tokens.
	maxFileSearchItems = 20
)

// slashItems returns the cached slash catalog. Rebuilds only after
// invalidateSlashCatalog — never on ordinary keystrokes.
func (m *chatTUI) slashItems() []compItem {
	if m.slashCatalogOnce && m.slashCatalog != nil {
		return m.slashCatalog
	}
	items := m.buildSlashCatalog()
	// Immutable snapshot so keystroke filtering never mutates shared state.
	out := make([]compItem, len(items))
	copy(out, items)
	m.slashCatalog = out
	m.slashCatalogOnce = true
	return m.slashCatalog
}

// invalidateSlashCatalog drops the cached catalog so the next slashItems call
// rebuilds it. Call from model switch, skill rescan, /reload-cmd, and any path
// that mutates commands/skills/host/extension actions.
func (m *chatTUI) invalidateSlashCatalog() {
	m.slashCatalogOnce = false
	m.slashCatalog = nil
}

// refreshHostAndInvalidateSlashCatalog reloads m.host from the controller and
// drops the slash catalog so MCP prompts (and any host-backed menu entries)
// rebuild on the next slashItems call. Use after connect/disconnect/remove/
// import, MCPSurfaceReady, auth clear, and every other host mutation path.
func (m *chatTUI) refreshHostAndInvalidateSlashCatalog() {
	if m.ctrl != nil {
		m.host = m.ctrl.Host()
	}
	m.invalidateSlashCatalog()
}

// setHostAndInvalidateSlashCatalog assigns a host pointer (e.g. from a model-
// switch message) and invalidates the slash catalog.
func (m *chatTUI) setHostAndInvalidateSlashCatalog(host *plugin.Host) {
	m.host = host
	m.invalidateSlashCatalog()
}

// buildSlashCatalog constructs the full slash menu from current sources.
func (m *chatTUI) buildSlashCatalog() []compItem {
	docsOwner := control.ResolveSlashCommandOwner(control.DocsSlashName, m.commands, m.skills)
	docsBuiltin := "/" + control.ResolvedBuiltinSlashName(control.DocsSlashName, m.commands, m.skills)
	items := renameSlashItem(builtinSlashItems(), "/docs", docsBuiltin)
	for _, c := range m.commands {
		if c.Hidden {
			continue
		}
		items = append(items, compItem{label: "/" + c.Name, insert: "/" + c.Name + " ", hint: customCommandHint(c)})
	}
	for _, s := range m.skills {
		if docsOwner == control.SlashOwnerCustom && s.SlashName() == control.DocsSlashName {
			continue
		}
		hint := s.Description
		if s.RunAs == skill.RunSubagent {
			hint = "🧬 " + hint
		}
		items = append(items, compItem{label: "/" + s.SlashName(), insert: "/" + s.SlashName() + " ", hint: skillCommandHint(s, hint)})
	}
	for _, p := range m.prompts() {
		items = append(items, compItem{label: "/" + p.Name, insert: "/" + p.Name + " ", hint: p.Description})
	}
	if m.ctrl != nil {
		for _, a := range m.ctrl.ExtensionActions() {
			items = append(items, compItem{label: a.Slash, insert: a.Slash + " ", hint: extensionActionHint(a)})
		}
	}
	return items
}

func renameSlashItem(items []compItem, oldLabel, newLabel string) []compItem {
	if oldLabel == newLabel {
		return items
	}
	for i := range items {
		if items[i].label != oldLabel {
			continue
		}
		items[i].label = newLabel
		if after, ok := strings.CutPrefix(items[i].insert, oldLabel); ok {
			items[i].insert = newLabel + after
		}
		break
	}
	return items
}

func removeSlashItems(items []compItem, label string) []compItem {
	out := make([]compItem, 0, len(items))
	for _, item := range items {
		if item.label != label {
			out = append(out, item)
		}
	}
	return out
}

// updateCompletion recomputes the menu from the current input: a slash menu
// while the line is a single "/word" token, or an @-reference menu while the
// token under the cursor is "@…".
func (m *chatTUI) updateCompletion() {
	val := m.input.Value()
	cursor := m.inputCursorByteOffset()

	// An @-reference token under the cursor wins — it can appear mid-line, even
	// inside a slash command's arguments (e.g. "/review @file").
	if at, end, token, ok := activeAtToken(val, cursor); ok {
		if items := m.atItems(token); len(items) > 0 {
			m.setCompletion(compAt, items, at, end)
			return
		}
	}

	// Slash completion only when the line is a pure slash command being typed
	// from the start (not mid-line after free text). Use the full value so a
	// mid-token cursor still filters the catalog without rewriting the line.
	if strings.HasPrefix(val, "/") {
		if items, from, ok := m.explicitSubcommandItems(val); ok && len(items) > 0 {
			m.setCompletion(compSlashArg, items, from, tokenEnd(val, from))
			return
		}
		if !strings.ContainsAny(val, " \t\n") {
			// Still naming the command itself. Catalog is cached; filter is cheap.
			if items := fuzzyFilterSlash(m.slashItems(), val); len(items) > 0 {
				m.setCompletion(compSlash, items, 0, len(val))
				return
			}
		} else if m.bareSubcommandSpace(val) {
			m.completion = completion{}
			return
		} else if items, from, ok := m.slashArgItems(val); ok && len(items) > 0 {
			// Past the command word — complete its structured arguments.
			m.setCompletion(compSlashArg, items, from, tokenEnd(val, from))
			return
		}
	}

	m.completion = completion{}
}

// inputCursorByteOffset returns the byte offset of the insertion caret in
// input.Value(). Falls back to len(Value) when layout is unavailable so
// completion still works in unit tests that never size the window.
func (m *chatTUI) inputCursorByteOffset() int {
	val := m.input.Value()
	if val == "" {
		return 0
	}
	// Prefer the visual-row model used by mouse selection: it maps the caret
	// to a stable rune offset into Value().
	if m.width > 0 {
		rows := m.composerRows()
		if len(rows) > 0 {
			if cur := m.input.Cursor(); cur != nil {
				absRow := m.input.ScrollYOffset() + cur.Y
				if absRow >= 0 && absRow < len(rows) {
					row := rows[absRow]
					// cur.X is screen-relative and includes the "❯ " prompt
					// gutter (composerPromptWidth columns). Subtract it so
					// we measure content columns only.
					col := max(cur.X-composerPromptWidth, 0)
					visual := 0
					for _, cell := range row.cells {
						w := rw.RuneWidth(cell.r)
						if visual+w > col {
							if cell.offset >= 0 {
								// cell.offset is a rune index into Value.
								return runeOffsetToByte(val, cell.offset)
							}
							break
						}
						visual += w
					}
					if row.endOffset >= 0 {
						return runeOffsetToByte(val, row.endOffset)
					}
				}
			}
		}
	}
	return len(val)
}

// runeOffsetToByte converts a rune index into Value() into a byte index.
func runeOffsetToByte(val string, runeOff int) int {
	if runeOff <= 0 {
		return 0
	}
	i := 0
	for ri := range val {
		if i == runeOff {
			return ri
		}
		i++
	}
	return len(val)
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
	if items, from, ok := m.themeArgItems(val); ok {
		return items, from, len(items) > 0
	}
	// Delegate to the shared completion logic so the chat TUI and the desktop
	// offer identical sub-command hints. We supply the data from the TUI's own
	// cached lists (no live controller needed), build the items, and adapt them
	// to compItem.
	items, from := control.SlashArgItems(val, m.slashArgData())
	if len(items) == 0 {
		return nil, 0, false
	}
	return slashItemsToComps(items), from, true
}

func (m *chatTUI) slashArgData() control.ArgData {
	curProvider := ""
	if parts := strings.SplitN(m.modelRef, "/", 2); len(parts) == 2 {
		curProvider = parts[0]
	}
	data := control.ArgData{
		Skills:          m.skills,
		ModelRefs:       modelRefs(),
		CurrentModel:    m.modelRef,
		ProviderNames:   providerNames(),
		CurrentProvider: curProvider,
		PluginNames:     pluginArgNames(),
	}
	if m.ctrl != nil {
		data.DisabledSkills = m.ctrl.DisabledSkills()
		data.ConfiguredMCP = m.ctrl.ConfiguredMCPNames()
		data.DisconnectedMCP = m.ctrl.DisconnectedMCPNames()
		data.MemoryRefs, data.MemoryArchives = control.MemoryCompletionData(m.ctrl.Memory())
	}
	if m.host != nil {
		data.ServerNames = m.host.ServerNames()
	}
	return data
}

func (m *chatTUI) explicitSubcommandItems(val string) ([]compItem, int, bool) {
	cmd, ok := strings.CutSuffix(val, "?")
	if !ok {
		return nil, 0, false
	}
	switch cmd {
	case "/mcp", "/skill", "/skills", "/plugin", "/plugins", "/memory":
	default:
		return nil, 0, false
	}
	items, _ := control.SlashArgItems(cmd+" ", m.slashArgData())
	if len(items) == 0 {
		return nil, 0, false
	}
	out := slashItemsToComps(items)
	for i := range out {
		out[i].insert = " " + out[i].insert
	}
	return out, len(cmd), true
}

func (m *chatTUI) bareSubcommandSpace(val string) bool {
	if !strings.ContainsAny(val, " \t") || strings.TrimRight(val, " \t") == val {
		return false
	}
	fields := strings.Fields(val)
	if len(fields) != 1 {
		return false
	}
	switch fields[0] {
	case "/mcp", "/skill", "/skills", "/plugin", "/plugins", "/memory":
		return true
	default:
		return false
	}
}

func slashItemsToComps(items []control.SlashItem) []compItem {
	out := make([]compItem, len(items))
	for i, it := range items {
		out[i] = compItem{label: it.Label, insert: it.Insert, hint: it.Hint, descend: it.Descend}
	}
	return out
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
	// Branches snapshots first, which can retarget the controller to a
	// recovery branch; keep the lease on whatever the controller now owns.
	m.followSessionLease()
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
// same menu kind stays open. replaceFrom/replaceTo form a half-open byte span
// of the token that acceptCompletion will replace.
func (m *chatTUI) setCompletion(kind compKind, items []compItem, replaceFrom, replaceTo int) {
	sel := 0
	if m.completion.active && m.completion.kind == kind && m.completion.sel < len(items) {
		sel = m.completion.sel
	}
	if replaceTo < replaceFrom {
		replaceTo = replaceFrom
	}
	m.completion = completion{
		active: true, kind: kind, items: items, sel: sel,
		replaceFrom: replaceFrom, replaceTo: replaceTo,
	}
}

// fuzzyFilterSlash returns the slash-menu items that match query as a
// case-insensitive subsequence of their label, with prefix hits ranked first
// (each group preserved in the input order from slashItems). An empty query
// matches everything — the same behavior the old prefix filter had, since
// every label trivially starts with "". A query that matches nothing returns
// nil so the caller can fall through and close the menu.
func fuzzyFilterSlash(items []compItem, query string) []compItem {
	if query == "" {
		out := make([]compItem, len(items))
		copy(out, items)
		return out
	}
	lq := strings.ToLower(query)
	var prefix, rest []compItem
	for _, it := range items {
		l := strings.ToLower(it.label)
		switch {
		case strings.HasPrefix(l, lq):
			prefix = append(prefix, it)
		case subsequenceMatch(l, lq):
			rest = append(rest, it)
		}
	}
	if len(prefix) == 0 && len(rest) == 0 {
		return nil
	}
	out := make([]compItem, 0, len(prefix)+len(rest))
	out = append(out, prefix...)
	out = append(out, rest...)
	return out
}

// subsequenceMatch reports whether query appears in target as a case-folded
// subsequence (each rune of query in order, not necessarily contiguous). It is
// the matcher behind the slash-menu fuzzy filter: typing "/modl" matches
// "/model", "/memory", or any other label where m-o-d-l appear in that order.
// Callers must pass already case-folded strings; an empty query matches
// every target, so callers that want a "no match" signal on the empty input
// should check that first.
func subsequenceMatch(target, query string) bool {
	if query == "" {
		return true
	}
	qr := []rune(query)
	ti := 0
	for _, r := range target {
		if r == qr[ti] {
			ti++
			if ti == len(qr) {
				return true
			}
		}
	}
	return false
}

// activeAtToken finds the @-reference token under the cursor. cursor is a byte
// offset into val; when out of range the scan uses the end of the string.
// The '@' must start the line or follow whitespace, so emails like "a@b" don't
// trigger it. A backslash-escaped space or tab is part of the token.
//
// Returns (at, end, query, ok):
//   - [at, end) is the full token span to replace on accept (including '@'),
//     extending past the caret to the next unescaped whitespace so mid-token
//     accept never leaves a dangling suffix ("@foo|bar" → "@file.md ", not
//     "@file.mdbar").
//   - query is only the text after '@' up to the caret, used for menu filtering
//     ("@fo|o" filters as "fo", not "foo").
func activeAtToken(val string, cursor int) (at, end int, query string, ok bool) {
	if cursor < 0 || cursor > len(val) {
		cursor = len(val)
	}
	for i := cursor - 1; i >= 0; i-- {
		switch val[i] {
		case ' ', '\t':
			if i > 0 && val[i-1] == '\\' {
				i-- // escaped whitespace stays inside the token
				continue
			}
			return 0, 0, "", false
		case '\n':
			return 0, 0, "", false
		case '@':
			if i == 0 || val[i-1] == ' ' || val[i-1] == '\t' || val[i-1] == '\n' {
				end = tokenEnd(val, i+1)
				queryEnd := min(max(cursor, i+1), end)
				return i, end, val[i+1 : queryEnd], true
			}
			return 0, 0, "", false
		}
	}
	return 0, 0, "", false
}

// tokenEnd returns the exclusive byte end of a path/ref token starting at from
// (just after '@'). Stops at unescaped whitespace or newline.
func tokenEnd(val string, from int) int {
	for i := from; i < len(val); i++ {
		switch val[i] {
		case ' ', '\t':
			if i > 0 && val[i-1] == '\\' {
				continue
			}
			return i
		case '\n':
			return i
		}
	}
	return len(val)
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
	// The typed token may carry backslash-escaped spaces (the form completion
	// itself inserts); filesystem lookups need the real path while inserts keep
	// the escaped grammar.
	fsFrag := control.UnescapeRefPath(frag)
	workspaceRoot := ""
	if m.ctrl != nil {
		workspaceRoot = m.ctrl.WorkspaceRoot()
	}
	readDir := control.UnescapeRefPath(dir)
	if workspaceRoot != "" {
		if readDir == "" {
			readDir = workspaceRoot
		} else if !filepath.IsAbs(readDir) {
			readDir = filepath.Join(workspaceRoot, filepath.FromSlash(readDir))
		}
	} else if readDir == "" {
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

	showHidden := strings.HasPrefix(fsFrag, ".")
	var items []compItem
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, fsFrag) {
			continue
		}
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir() {
			items = append(items, compItem{label: name + "/", insert: "@" + dir + control.EscapeRefPath(name) + "/", hint: "dir", descend: true})
		} else {
			items = append(items, compItem{label: name, insert: "@" + dir + control.EscapeRefPath(name)})
		}
		if len(items) >= maxCompItems {
			break
		}
	}

	// At the top level (still naming the first segment) MCP resources share the
	// '@' namespace, so offer the matching ones too.
	if !strings.Contains(token, "/") {
		seen := map[string]bool{}
		for _, it := range items {
			seen[strings.TrimPrefix(it.insert, "@")] = true
		}
		remaining := min(maxCompItems-len(items), maxFileSearchItems)
		results := m.searchFileRefs(fsFrag)
		if len(results) > remaining {
			results = results[:remaining]
		}
		for _, path := range results {
			escaped := control.EscapeRefPath(path)
			if seen[escaped] {
				continue
			}
			items = append(items, compItem{label: path, insert: "@" + escaped, hint: "file"})
			if len(items) >= maxCompItems {
				break
			}
		}
		items = append(items, m.resourceItems("", token)...)
	}
	return items
}

// searchFileRefs memoizes the bounded basename walk so re-rendering the menu
// for an unchanged @token fragment doesn't re-walk the workspace each keystroke.
func (m *chatTUI) searchFileRefs(frag string) []string {
	if m.fileSearchCache == nil {
		m.fileSearchCache = map[string][]string{}
	}
	if r, ok := m.fileSearchCache[frag]; ok {
		return r
	}
	searchRoot := "."
	if m.ctrl != nil {
		if wr := m.ctrl.WorkspaceRoot(); wr != "" {
			searchRoot = wr
		}
	}
	results := fileref.Search(searchRoot, frag, maxFileSearchItems)
	paths := make([]string, 0, len(results))
	for _, r := range results {
		paths = append(paths, r.Path)
	}
	m.fileSearchCache[frag] = paths
	return paths
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
	return slices.Contains(m.host.ServerNames(), name)
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

func (m *chatTUI) completionExactLabel() bool {
	if !m.completion.active || m.completion.sel >= len(m.completion.items) {
		return false
	}
	val := strings.TrimSpace(m.input.Value())
	return val == m.completion.items[m.completion.sel].label
}

func (m *chatTUI) completionBareOverlayCommand() bool {
	switch strings.TrimSpace(m.input.Value()) {
	case "/mcp", "/skills":
		return true
	default:
		return false
	}
}

func (m *chatTUI) completionSelectedInsertPresent() bool {
	if !m.completion.active || m.completion.sel >= len(m.completion.items) {
		return false
	}
	val := m.input.Value()
	rf, rt := m.completion.replaceFrom, m.completion.replaceTo
	if rf < 0 || rf > len(val) {
		return false
	}
	if rt < rf || rt > len(val) {
		rt = len(val)
	}
	return val[rf:rt] == m.completion.items[m.completion.sel].insert
}

// acceptCompletion applies the selected item to the input, then recomputes the
// menu from the new value: it re-opens one level deeper (a descended directory
// or a freshly completed command's arguments) or closes when nothing applies.
// Cursor moves to the end of the inserted token only on accept — ordinary
// keystrokes never call this path, so mid-line typing keeps its caret.
func (m *chatTUI) acceptCompletion() {
	if m.completion.sel >= len(m.completion.items) {
		m.completion = completion{}
		return
	}
	it := m.completion.items[m.completion.sel]
	val := m.input.Value()
	rf := m.completion.replaceFrom
	rt := m.completion.replaceTo
	if rf < 0 || rf > len(val) {
		rf = 0
	}
	// replaceTo must be set by setCompletion. Hand-built test completions may
	// leave it at 0; treat inverted/empty whole-line spans as "to end".
	if rt < rf || rt > len(val) {
		rt = len(val)
	} else if rt == rf && rf == 0 && len(val) > 0 {
		// Bare slash replace with unset replaceTo: replace the whole line.
		rt = len(val)
	}
	// Replace the full token span [rf, rt); keep any suffix after the token
	// so "see @foo and more" + accept @foobar.md becomes
	// "see @foobar.md and more" (not "@foobar.mdand" or "@foobar.mdfoo").
	newVal := val[:rf] + it.insert + val[rt:]
	insertEnd := rf + len(it.insert)
	m.input.SetValue(newVal)
	// Place caret at the end of the inserted completion only. Fall back to
	// CursorEnd when the layout has no width yet (unit tests).
	if m.width > 0 {
		m.setComposerCursor(len([]rune(newVal[:min(insertEnd, len(newVal))])))
	} else {
		m.input.CursorEnd()
	}
	if it.descend || strings.HasSuffix(it.insert, " ") {
		m.updateCompletion()
		return
	}
	m.updateCompletion() // re-filter for arg completion (e.g. /resume → numbered sessions)
	// If the completion re-opened with the same single item the user just
	// selected (i.e. the token was already typed), close it so the next Enter
	// submits the command rather than being captured again by acceptCompletion.
	if m.completion.active && len(m.completion.items) == 1 {
		rf, rt := m.completion.replaceFrom, m.completion.replaceTo
		val := m.input.Value()
		if rf >= 0 && rf <= len(val) && rt >= rf && rt <= len(val) {
			if val[rf:rt] == m.completion.items[0].insert {
				m.completion = completion{}
			}
		}
	}
}

var compSelStyle lipgloss.Style

const completionPadCell = "\u00a0"

// padCompletionLine pads completion rows with NBSPs instead of ASCII spaces.
// Ultraviolet treats trailing ASCII spaces as clearable cells and may emit EL
// or ECH erase sequences; mintty can leave stale CJK glyph cells after those
// erases. NBSP is visually blank but forces the renderer to overwrite cells.
func padCompletionLine(s string, w int) string {
	pad := w - visibleWidth(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(completionPadCell, pad)
}

// renderCompletion draws the menu above the input box: matching items, windowed
// around the selection, the current row highlighted, hints dimmed. Every line is
// padded to m.width with non-clearable blank cells so bubbletea's delta renderer
// has no ordinary trailing-space run to collapse into EL/ECH erase sequences.
// That avoids ghost cells on terminals (mintty) with unreliable erases after
// wide CJK glyphs.
func (m chatTUI) renderCompletion() string {
	if !m.completion.active || len(m.completion.items) == 0 {
		return ""
	}
	items := m.completion.items
	start := 0
	if len(items) > maxCompRows {
		start = min(max(m.completion.sel-maxCompRows/2, 0), len(items)-maxCompRows)
	}
	end := min(start+maxCompRows, len(items))

	var b strings.Builder
	for i := start; i < end; i++ {
		it := items[i]
		var line string
		if i == m.completion.sel {
			line = accent("› ") + compSelStyle.Render(it.label)
		} else {
			line = "  " + it.label
		}
		if it.hint != "" {
			line += "  " + dim(it.hint)
		}
		b.WriteString(padCompletionLine(line, m.width))
		b.WriteByte('\n')
	}
	// A key-hint footer so users discover Tab — many won't know it accepts a
	// completion, let alone descends into a folder.
	hint := i18n.M.CompHintSlash
	if m.completion.kind == compAt {
		hint = i18n.M.CompHintFile
	}
	b.WriteString(padCompletionLine(dim(hint), m.width))
	return b.String()
}
