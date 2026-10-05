package tui

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
	"reasonix/internal/frontend/termrender"
)

const menuRows = 8

// menu is the composer's open completion list for the line it was asked
// about; an answer for any other line is stale and dropped.
type menu struct {
	line string
	c    Completion
	sel  int
}

type completionMsg struct {
	line string
	c    Completion
	err  error
}

// cursorByte is the composer cursor as a byte offset into its value.
func (m *model) cursorByte() int {
	value := m.composer.Value()
	lines := strings.Split(value, "\n")
	row := min(m.composer.Line(), len(lines)-1)
	off := 0
	for _, l := range lines[:row] {
		off += len(l) + 1
	}
	cur := []rune(lines[row])
	return off + len(string(cur[:min(m.composer.Column(), len(cur))]))
}

// utf16At is the UTF-16 offset of byte offset b in s: the unit /complete
// counts in, since a browser indexes the same string that way.
func utf16At(s string, b int) int {
	return len(utf16.Encode([]rune(s[:min(b, len(s))])))
}

// byteAt is the byte offset of UTF-16 offset u in s.
func byteAt(s string, u int) int {
	units := 0
	for i, r := range s {
		if units >= u {
			return i
		}
		units += utf16.RuneLen(r)
	}
	return len(s)
}

// wantsMenu reports a line whose cursor sits in a token the menu answers
// without being asked: a bare slash command, or an @-reference.
func wantsMenu(line string, cursor int) bool {
	if strings.HasPrefix(line, "/") && !strings.ContainsAny(line, " \n") {
		return true
	}
	start := strings.LastIndexAny(line[:cursor], " \n\t") + 1
	return strings.HasPrefix(line[start:cursor], "@")
}

func (m *model) fetchCompletion() tea.Cmd {
	line := m.composer.Value()
	cursor := utf16At(line, m.cursorByte())
	return func() tea.Msg {
		c, err := m.client.Complete(m.ctx, line, cursor)
		return completionMsg{line: line, c: c, err: err}
	}
}

// refreshMenu follows the composer after an edit: it asks again where the
// menu answers on its own, and closes the menu anywhere else.
func (m *model) refreshMenu() tea.Cmd {
	line := m.composer.Value()
	if !wantsMenu(line, m.cursorByte()) {
		m.menu = nil
		return nil
	}
	return m.fetchCompletion()
}

func (m *model) onCompletion(msg completionMsg) {
	if msg.err != nil || msg.line != m.composer.Value() {
		m.menu = nil
		return
	}
	c := msg.c
	c.Items = append(m.localCommands(msg.line), c.Items...)
	if c.Typed != nil {
		c.Items = append([]CompletionItem{*c.Typed}, c.Items...)
	}
	if len(c.Items) == 0 {
		m.menu = nil
		return
	}
	if len(msg.c.Items) == 0 {
		c.From, c.To = 0, utf16At(msg.line, len(msg.line))
	}
	m.menu = &menu{line: msg.line, c: c}
}

// localCommands are the slash commands this screen answers itself, offered
// beside the kernel's while a bare command is being typed.
func (m *model) localCommands(line string) []CompletionItem {
	if !strings.HasPrefix(line, "/") || strings.ContainsAny(line, " \n") {
		return nil
	}
	cmds := []CompletionItem{{Label: "/resume", Insert: "/resume", Hint: i18n.M.CmdResume}}
	for _, c := range []struct{ name, hint string }{
		{"/queue", i18n.M.CmdQueue}, {"/steer", i18n.M.CmdSteer}, {"/takeover", i18n.M.CmdTakeover},
		{"/status", i18n.M.CmdStatus}, {"/export", i18n.M.CmdExport}, {"/copy", i18n.M.CmdCopy},
	} {
		cmds = append(cmds, CompletionItem{Label: c.name, Insert: c.name, Hint: c.hint})
	}
	cmds = append(cmds, CompletionItem{Label: "/setup", Insert: "/setup", Hint: i18n.M.CmdSetup},
		CompletionItem{Label: "/version", Insert: "/version", Hint: i18n.M.CmdVersion},
		CompletionItem{Label: "/help", Insert: "/help", Hint: i18n.M.CmdHelp},
		CompletionItem{Label: "/paste-image", Insert: "/paste-image", Hint: i18n.M.CmdPasteImage})
	cmds = append(cmds,
		CompletionItem{Label: "/cls", Insert: "/cls", Hint: i18n.M.CmdCls},
		CompletionItem{Label: "/todo", Insert: "/todo", Hint: i18n.M.CmdTodo},
		CompletionItem{Label: "/verbose", Insert: "/verbose", Hint: i18n.M.CmdVerbose},
		CompletionItem{Label: "/diff-fold", Insert: "/diff-fold", Hint: i18n.M.CmdDiffFold},
		CompletionItem{Label: "/theme", Insert: "/theme ", Hint: i18n.M.CmdTheme, Descend: true})
	if m.scr != nil {
		cmds = append(cmds, CompletionItem{Label: "/mouse", Insert: "/mouse", Hint: i18n.M.CmdMouse})
	}
	var out []CompletionItem
	for _, c := range cmds {
		if strings.HasPrefix(c.Label, line) {
			out = append(out, c)
		}
	}
	return out
}

// menuKey takes the keys an open menu owns.
func (m *model) menuKey(k string) (tea.Cmd, bool) {
	if m.menu == nil {
		return nil, false
	}
	n := len(m.menu.c.Items)
	switch k {
	case "tab", "down":
		m.menu.sel = (m.menu.sel + 1) % n
	case "shift+tab", "up":
		m.menu.sel = (m.menu.sel + n - 1) % n
	case "esc":
		m.menu = nil
	case "enter":
		if m.menuChoiceTyped() {
			m.menu = nil
			return nil, false
		}
		return m.acceptCompletion(), true
	default:
		return nil, false
	}
	return nil, true
}

// menuChoiceTyped reports a line that already says what the highlighted row
// would make it — the command's name, or the argument in place — so Enter sends
// it instead of completing it again.
func (m *model) menuChoiceTyped() bool {
	item, line := m.menu.c.Items[m.menu.sel], m.menu.line
	if strings.TrimSpace(line) == item.Label {
		return true
	}
	from, to := byteAt(line, m.menu.c.From), byteAt(line, m.menu.c.To)
	return from < to && line[from:to] == item.Insert
}

// acceptCompletion replaces the token the menu answered with the chosen item.
// A directory keeps the menu open one level down.
func (m *model) acceptCompletion() tea.Cmd {
	c, item := m.menu.c, m.menu.c.Items[m.menu.sel]
	line := m.menu.line
	from, to := byteAt(line, c.From), byteAt(line, c.To)
	head := line[:from] + item.Insert
	m.composer.SetValue(head + line[to:])
	if !strings.Contains(line, "\n") {
		m.composer.SetCursorColumn(utf8.RuneCountInString(head))
	}
	m.menu = nil
	if item.Descend {
		return m.fetchCompletion()
	}
	return nil
}

func (m *model) menuLines() []string {
	if m.menu == nil {
		return nil
	}
	items := m.menu.c.Items
	first := max(0, min(m.menu.sel-menuRows/2, len(items)-menuRows))
	last := min(len(items), first+menuRows)
	lines := make([]string, 0, last-first+1)
	for i := first; i < last; i++ {
		it := items[i]
		label := oneLine(it.Label, m.width/2)
		row := "    " + label
		if i == m.menu.sel {
			row = termrender.Accent("  › ") + termrender.Bold(label)
		}
		if it.Hint != "" {
			row += termrender.Dim("  " + oneLine(it.Hint, max(m.width-8-termrender.VisibleWidth(label), 10)))
		}
		lines = append(lines, row)
	}
	hint := i18n.M.CompHintSlash
	if m.menu.c.Kind == "ref" {
		hint = i18n.M.CompHintFile
	}
	return append(lines, termrender.Dim("    "+hint))
}
