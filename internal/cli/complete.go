package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/control"
	"reasonix/internal/fileref"
	"reasonix/internal/i18n"
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
	// completionPanelRows is the picker height the slash/arg menus render as a
	// bottom sheet, with a draggable scrollbar on the right when the list
	// overflows.
	completionPanelRows = 10
	// maxCompItems caps how many entries a single directory contributes, so a
	// pathologically large directory can't blow up the menu — we read only one
	// level (os.ReadDir), never the whole tree.
	maxCompItems = 200
	// maxFileSearchItems caps basename search results for bare @tokens.
	maxFileSearchItems = 20
)

// slashItems is the full set of slash commands offered for completion: the
// built-in verbs, custom commands, and MCP prompts. Installed skills are NOT
// listed here — they are reached via "/skills " which pops the same picker.
func (m *chatTUI) slashItems() []compItem {
	items := builtinSlashItems()
	for _, c := range m.commands {
		if c.Hidden {
			continue
		}
		items = append(items, compItem{label: "/" + c.Name, insert: "/" + c.Name + " ", hint: customCommandHint(c)})
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
		if items, from, ok := m.explicitSubcommandItems(val); ok && len(items) > 0 {
			m.setCompletion(compSlashArg, items, from)
			return
		}
		if !strings.ContainsAny(val, " \t\n") {
			// Still naming the command itself.
			if items := fuzzyFilterSlash(m.slashItems(), val); len(items) > 0 {
				m.setCompletion(compSlash, items, 0)
				return
			}
		} else if items, from, ok := m.slashArgItems(val); ok && len(items) > 0 {
			// Past the command word — complete its structured arguments. This
			// is how "/skills " and "/mcp " pop their subcommand picker.
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
	if items, from, ok := m.workModeArgItems(val); ok {
		return items, from, len(items) > 0
	}
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
// same menu kind stays open.
func (m *chatTUI) setCompletion(kind compKind, items []compItem, replaceFrom int) {
	sel := 0
	if m.completion.active && m.completion.kind == kind && m.completion.sel < len(items) {
		sel = m.completion.sel
	}
	m.completion = completion{active: true, kind: kind, items: items, sel: sel, replaceFrom: replaceFrom}
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

// activeAtToken finds the @-reference token ending at the cursor (assumed at the
// input's end). The '@' must start the line or follow whitespace, so emails
// like "a@b" don't trigger it. A backslash-escaped space or tab is part of the
// token (the form EscapeRefPath inserts for paths with spaces), so completion
// can descend through such directories. Returns the '@' offset and the text
// after it.
func activeAtToken(val string) (int, string, bool) {
	for i := len(val) - 1; i >= 0; i-- {
		switch val[i] {
		case ' ', '\t':
			if i > 0 && val[i-1] == '\\' {
				i-- // escaped whitespace stays inside the token
				continue
			}
			return 0, "", false // hit whitespace before an '@' → no active token
		case '\n':
			return 0, "", false
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
		remaining := maxCompItems - len(items)
		if remaining > maxFileSearchItems {
			remaining = maxFileSearchItems
		}
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
	if m.completion.replaceFrom > len(val) {
		return false
	}
	return val[m.completion.replaceFrom:] == m.completion.items[m.completion.sel].insert
}

// acceptCompletion applies the selected item to the input, then recomputes the
// menu from the new value: it re-opens one level deeper (a descended directory
// or a freshly completed command's arguments) or closes when nothing applies.
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
	if it.descend || strings.HasSuffix(it.insert, " ") {
		m.updateCompletion()
		return
	}
	m.updateCompletion() // re-filter for arg completion (e.g. /resume → numbered sessions)
	// If the completion re-opened with the same single item the user just
	// selected (i.e. the token was already typed), close it so the next Enter
	// submits the command rather than being captured again by acceptCompletion.
	if m.completion.active && len(m.completion.items) == 1 {
		tok := m.input.Value()[m.completion.replaceFrom:]
		if tok == m.completion.items[0].insert {
			m.completion = completion{}
		}
	}
}

var compSelStyle lipgloss.Style

// pickerSheetStyle tints the bottom-sheet picker rows (slash menu and every
// subcommand picker) with a slightly lighter surface than the chat background
// so the pop-up reads as a distinct overlay; the selected row wears the
// accent band (compSelStyle) instead.
var pickerSheetStyle lipgloss.Style

// userBubbleStyle paints user turns in the transcript as a solid band on the
// user-bubble surface, separating them from the assistant's plain text flow.
var userBubbleStyle lipgloss.Style

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

// pickerSheetRow is one selectable row of a bottom-sheet picker.
type pickerSheetRow struct {
	label string
	hint  string
}

// renderPickerSheet renders the shared bottom-sheet picker used by the slash
// menu, every subcommand picker (/mcp, /skills, /model, …), the quick pickers
// (/resume, /model, /provider), and the rewind picker: an optional title and
// search line, rows windowed around the selection, the selected row as a
// full-width accent band, a dim hint footer, and a draggable scrollbar on the
// right edge when the list overflows. Rows sit on a slightly lighter surface
// than the chat background (pickerSheetStyle) so the pop-up reads as a
// distinct overlay. Every line is padded to `width` with non-clearable blank
// cells so bubbletea's delta renderer has no ordinary trailing-space run to
// collapse into EL/ECH erase sequences — that avoids ghost cells on terminals
// (mintty) with unreliable erases after wide CJK glyphs.
func renderPickerSheet(title, search string, items []pickerSheetRow, sel, rows, width int, hint string) string {
	if len(items) == 0 {
		return ""
	}
	start := completionWindowStart(sel, len(items), rows)
	end := start + rows
	if end > len(items) {
		end = len(items)
	}

	// The scrollbar column appears only when the list overflows the panel.
	showScrollbar := len(items) > rows
	thumbStart, thumbSize := 0, 0
	if showScrollbar {
		thumbStart, thumbSize = scrollbarThumb(rows, start, len(items))
	}
	contentW := width
	if showScrollbar {
		contentW--
	}
	if contentW < 10 {
		contentW = 10
	}

	var b strings.Builder
	if title != "" {
		b.WriteString(accent(title) + "\n")
	}
	if search != "" {
		b.WriteString("  " + dim("Search: ") + search + "\n")
	}
	for i := start; i < end; i++ {
		it := items[i]
		body := it.label
		if it.hint != "" {
			body += "  " + dim(it.hint)
		}
		// Clamp long labels/hints (model names, skill descriptions) to the
		// content width so the sheet never overflows the terminal. The
		// selected row adds two padding cells on each side, so the body
		// budget reserves them.
		body = ansi.Truncate(body, max(contentW-4, 1), "…")
		bar := ""
		if showScrollbar {
			if rel := i - start; rel >= thumbStart && rel < thumbStart+thumbSize {
				bar = scrollThumbStyle.Render("█")
			} else {
				bar = scrollTrackStyle.Render("│")
			}
		}
		if i == sel {
			// Full-width selection band: label, hint, and the NBSP padding all
			// wear the accent background so the highlight is a solid row across
			// the whole sheet (no floating chip).
			b.WriteString(compSelStyle.Render(padCompletionLine("  "+body+"  ", contentW) + bar))
		} else {
			b.WriteString(pickerSheetStyle.Render(padCompletionLine("  "+body, contentW) + bar))
		}
		b.WriteByte('\n')
	}
	b.WriteString(pickerSheetStyle.Render(padCompletionLine(ansi.Truncate(dim(hint), width, "…"), width)))
	return b.String()
}

// renderCompletion draws the picker sheet for the active completion menu. The
// slash menu and every subcommand picker share renderPickerSheet, so all
// command pop-ups look and behave identically (same panel, same scrollbar).
func (m chatTUI) renderCompletion() string {
	if !m.completion.active || len(m.completion.items) == 0 {
		return ""
	}
	hint := i18n.M.CompHintSlash
	if m.completion.kind == compAt {
		hint = i18n.M.CompHintFile
	}
	rows := make([]pickerSheetRow, len(m.completion.items))
	for i, it := range m.completion.items {
		rows[i] = pickerSheetRow{label: it.label, hint: it.hint}
	}
	return renderPickerSheet("", "", rows, m.completion.sel, m.completionPanelRows(), m.contentWidth(), hint)
}

// completionPanelRows is the picker's item-row count: the tall bottom sheet,
// shrunk to leave room for the composer + status rows on short terminals, and
// never larger than the item list itself.
func (m chatTUI) completionPanelRows() int {
	rows := completionPanelRows
	if m.height > 0 {
		if max := m.height - 10; max < rows { // reserve composer + status + working
			rows = max
		}
	}
	if rows < 4 {
		rows = 4
	}
	if n := len(m.completion.items); rows > n {
		rows = n
	}
	if rows < 1 {
		rows = 1
	}
	return rows
}

// completionWindowStart returns the first visible item index for the current
// selection: the menu windows around the selection, clamped to the list.
func completionWindowStart(sel, total, rows int) int {
	if total <= rows {
		return 0
	}
	start := sel - rows/2
	if start < 0 {
		start = 0
	}
	if start > total-rows {
		start = total - rows
	}
	return start
}

// bottomSheetBounds returns the screen row span [top, bottom) a bottom sheet
// of `rows` total rows occupies above the composer/status block. Every bottom
// sheet (completion menu, quick picker, rewind picker) renders in the same
// slot, so they share this geometry. (0, 0) when the terminal size is unknown.
func (m chatTUI) bottomSheetBounds(rows int) (top, bottom int) {
	bottom = m.height - m.statusLineCount
	if !m.hideComposer() {
		bottom -= m.input.Height() + 2
	}
	if qi := m.renderQueueIndicator(); qi != "" {
		bottom -= strings.Count(qi, "\n") + 1
	}
	if footer := m.renderMainManagerFooter(); footer != "" {
		bottom -= strings.Count(footer, "\n") + 1
	}
	if m.state == tuiRunning {
		bottom-- // working line
	}
	if m.nativeScrollback {
		if main := m.renderMainManager(); main != "" {
			bottom -= strings.Count(main, "\n") + 1
		}
	}
	top = bottom - rows
	if top < 0 {
		top = 0
	}
	if bottom < top {
		bottom = top
	}
	return top, bottom
}

// sheetDragSel maps a scrollbar drag row to a selection index: the thumb
// tracks the visible window, and the selection leads the window by half a
// panel so the highlight stays under the pointer.
func sheetDragSel(rows, total, rowInMenu, grabOffset int) int {
	windowTop := scrollbarYOffset(rows, rowInMenu, total, grabOffset)
	sel := windowTop + rows/2
	if sel > total-1 {
		sel = total - 1
	}
	return sel
}

// completionMenuBounds returns the screen row span [top, bottom) of the
// completion panel, including its hint footer, mirroring the bottom-region
// layout View() paints. (0, 0) when no menu is shown.
func (m chatTUI) completionMenuBounds() (top, bottom int) {
	if !m.completion.active || len(m.completion.items) == 0 {
		return 0, 0
	}
	return m.bottomSheetBounds(m.completionPanelRows() + 1) // items + hint footer
}

// inCompletionScrollbar reports whether (x, y) is on the completion panel's
// scrollbar column (only when the list overflows; the hint row is excluded).
func (m chatTUI) inCompletionScrollbar(x, y int) bool {
	if !m.completion.active || len(m.completion.items) == 0 {
		return false
	}
	if len(m.completion.items) <= m.completionPanelRows() {
		return false
	}
	top, bottom := m.completionMenuBounds()
	if y < top || y >= bottom-1 { // last row is the hint footer
		return false
	}
	return x == m.contentWidth()-1
}

// completionScrollbarGrabRowOffset mirrors scrollbarGrabRowOffset for the
// completion panel: where inside the thumb the drag grabbed, so the thumb
// doesn't jump to the cursor on click.
func (m chatTUI) completionScrollbarGrabRowOffset(row int) int {
	rows := m.completionPanelRows()
	top, _ := m.completionMenuBounds()
	rowInMenu := row - top
	if rowInMenu < 0 {
		rowInMenu = 0
	}
	return sheetScrollbarGrabRowOffset(rowInMenu, rows, len(m.completion.items), completionWindowStart(m.completion.sel, len(m.completion.items), rows))
}

// sheetScrollbarGrabRowOffset returns where inside the thumb a click at
// rowInMenu (relative to the item rows) grabbed, so the thumb doesn't jump to
// the cursor on click.
func sheetScrollbarGrabRowOffset(rowInMenu, rows, total, windowStart int) int {
	thumbStart, thumbSize := scrollbarThumb(rows, windowStart, total)
	if rowInMenu >= thumbStart && rowInMenu < thumbStart+thumbSize {
		return rowInMenu - thumbStart
	}
	return thumbSize / 2
}

// dragCompletionScrollbar maps a drag row to a list position and moves the
// selection there. The render window centers on the selection, so the
// selection leads the scrollbar window by half a panel to keep the thumb
// under the pointer.
func (m *chatTUI) dragCompletionScrollbar(row int) {
	rows := m.completionPanelRows()
	total := len(m.completion.items)
	if total <= rows {
		return
	}
	top, _ := m.completionMenuBounds()
	rowInMenu := row - top
	if rowInMenu < 0 {
		rowInMenu = 0
	}
	if rowInMenu >= rows {
		rowInMenu = rows - 1
	}
	grab := 0
	if m.sheetScrollbar != nil {
		grab = m.sheetScrollbar.grab
	}
	m.completion.sel = sheetDragSel(rows, total, rowInMenu, grab)
}

// dragSheetScrollbar dispatches an in-flight scrollbar drag to the active
// bottom sheet's model.
func (m *chatTUI) dragSheetScrollbar(row int) {
	if m.sheetScrollbar == nil {
		return
	}
	switch m.sheetScrollbar.panel {
	case "quick":
		m.dragQuickPickerScrollbar(row)
	case "rewind":
		m.dragRewindScrollbar(row)
	default:
		m.dragCompletionScrollbar(row)
	}
}
