package tui

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/frontend/termrender"
)

// viActive reports whether the composer uses the vi command mode, enabled by
// ui.commandmode = "vi". When off the composer edits insert-only as before.
func (m *model) viActive() bool {
	return m.opts.CommandMode
}

// viInCommand reports whether the composer is currently in vi command (normal)
// mode rather than insert mode.
func (m *model) viInCommand() bool {
	return m.viActive() && m.viCmd
}

// viModeTag is the footer's vi mode marker: NORMAL while command mode owns the
// keys, INSERT while the composer edits text. Empty when vi mode is off.
func (m *model) viModeTag() string {
	if !m.viActive() {
		return ""
	}
	if m.viInCommand() {
		return termrender.Badge(tagViNormal, tagLight, "NORMAL")
	}
	return termrender.Badge(tagViInsert, tagLight, "INSERT")
}

// interruptKey names the key that cancels a running turn: Ctrl+C in vi mode,
// where Esc enters command mode instead, and Esc otherwise.
func (m *model) interruptKey() string {
	if m.viActive() {
		return "Ctrl+C"
	}
	return "Esc"
}

// viHint picks the card hint that names the keys which actually work: in vi
// mode Esc is ignored on the approval, ask and plan cards, so their hints drop
// it (or name Ctrl+C) and the plain string serves the default mode.
func (m *model) viHint(plain, vi string) string {
	if m.viActive() {
		return vi
	}
	return plain
}

// viKey takes the keys vi command mode owns: the interrupt keys whose meaning
// differs from insert mode, and the single printable keys that drive command
// mode. It reports whether the key was handled; with vi off it takes nothing.
func (m *model) viKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !m.viActive() {
		return nil, false
	}
	empty := m.composer.Value() == ""
	switch msg.String() {
	case "esc":
		// vi: Esc never interrupts a stream; it enters command mode and steps
		// the caret one rune left, as leaving insert mode does in vim. Only ^C
		// interrupts, a running turn or not.
		if m.shell && empty {
			m.shell = false
			return nil, true
		}
		if !m.viCmd {
			m.viMoveLeft()
		}
		m.viCmd = true
		return nil, true
	case "ctrl+c":
		// vi: Ctrl+C interrupts a stream. With any non-empty text it clears the
		// prompt and leaves shell mode, saving a trimmed draft when there is
		// one; truly empty, it leaves command mode.
		switch {
		case m.tr.Running:
			return m.interrupt(), true
		case !empty:
			if draft := strings.TrimSpace(m.composer.Value()); draft != "" {
				m.history = append(m.history, draft)
				m.histAt = len(m.history)
			}
			m.composer.Reset()
			m.shell = false
			m.viCmd = false
			return nil, true
		case m.viCmd:
			m.viCmd = false
		}
		return nil, true
	case "ctrl+d":
		// vi: Ctrl+D exits only while insert-mode editing a truly empty prompt.
		if empty && !m.tr.Running && !m.viCmd {
			return tea.Quit, true
		}
		return nil, true
	}
	// Command-mode keys drive the caret and editing instead of inserting text;
	// unrecognized ones are ignored. Every key carrying printable text is one,
	// so a multi-byte character (CJK/IME) is consumed rather than inserted.
	if m.viInCommand() && viPrintable(msg) {
		return m.viCommandRune(msg), true
	}
	return nil, false
}

// viPrintable reports whether the key carries text the composer would insert,
// which command mode consumes instead. msg.Text is empty for special keys and
// modifier combinations, and holds the rune(s) for a printable key.
func viPrintable(msg tea.KeyPressMsg) bool {
	for _, r := range msg.Text {
		if unicode.IsPrint(r) {
			return true
		}
	}
	return false
}

// viLines splits the composer into its logical lines so command mode can move a
// caret across pasted multi-line content with rune-accurate columns.
func (m *model) viLines() [][]rune {
	raw := strings.Split(m.composer.Value(), "\n")
	lines := make([][]rune, 0, len(raw))
	for _, l := range raw {
		lines = append(lines, []rune(l))
	}
	return lines
}

// viMoveLeft moves the caret one rune left, crossing to the end of the previous
// logical line when it is at the very start.
func (m *model) viMoveLeft() {
	col := m.composer.Column()
	if col > 0 {
		m.composer.SetCursorColumn(col - 1)
		return
	}
	if m.composer.Line() <= 0 {
		return
	}
	m.composer.CursorUp()
	lines := m.viLines()
	if r := m.composer.Line(); r < len(lines) {
		m.composer.SetCursorColumn(len(lines[r]))
	}
}

// viMoveRight moves the caret one rune right, crossing to the start of the next
// logical line when it is at the very end.
func (m *model) viMoveRight() {
	lines := m.viLines()
	row := m.composer.Line()
	if row >= len(lines) {
		return
	}
	if m.composer.Column() < len(lines[row]) {
		m.composer.SetCursorColumn(m.composer.Column() + 1)
		return
	}
	if row+1 < len(lines) {
		m.composer.CursorDown()
		m.composer.SetCursorColumn(0)
	}
}

// viMoveFirstNonBlank puts the caret on the first non-whitespace rune of the
// current line (the vi "^"), falling back to column 0 on blank lines.
func (m *model) viMoveFirstNonBlank() {
	lines := m.viLines()
	row := m.composer.Line()
	if row >= len(lines) {
		return
	}
	line := lines[row]
	col := 0
	for col < len(line) && unicode.IsSpace(line[col]) {
		col++
	}
	m.composer.SetCursorColumn(col)
}

// viCommandRune handles a single printable key while the composer is in vi
// command mode. Command-mode keys are consumed and acted on; any other key is
// ignored rather than inserting text.
func (m *model) viCommandRune(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "h":
		m.viMoveLeft()
	case "l":
		m.viMoveRight()
	case "j":
		m.composer.CursorDown()
	case "k":
		m.composer.CursorUp()
	case "0":
		m.composer.SetCursorColumn(0)
	case "$":
		m.composer.CursorEnd()
	case "^":
		m.viMoveFirstNonBlank()
	case "i":
		m.viCmd = false
	case "a":
		m.viMoveRight()
		m.viCmd = false
	case "x":
		m.viDeleteChar()
	case "D":
		m.viDeleteToEnd()
	case "I":
		m.viMoveFirstNonBlank()
		m.viCmd = false
	case "A":
		m.composer.CursorEnd()
		m.viCmd = false
	case "p":
		return m.pasteClipboardText()
	}
	return nil
}

// viDeleteChar removes the character under the caret (the rune to its right) in
// command mode, the vi "x". If the caret sits past the end of a line there is
// nothing to delete and the text is left untouched.
func (m *model) viDeleteChar() {
	lines := m.viLines()
	row := m.composer.Line()
	if row < 0 || row >= len(lines) {
		return
	}
	line := lines[row]
	col := m.composer.Column()
	if col >= len(line) {
		return
	}
	lines[row] = append(line[:col], line[col+1:]...)
	m.setViLines(lines, row, col)
}

// viDeleteToEnd removes from the caret to the end of the current line (the vi
// "D"), leaving the caret at the resulting end of line. With the caret already
// past the last rune there is nothing to delete and the text is left untouched.
func (m *model) viDeleteToEnd() {
	lines := m.viLines()
	row := m.composer.Line()
	if row < 0 || row >= len(lines) {
		return
	}
	line := lines[row]
	col := m.composer.Column()
	if col >= len(line) {
		return
	}
	lines[row] = line[:col]
	m.setViLines(lines, row, col)
}

// setViLines rewrites the composer from its logical lines and places the caret
// at (row, col), clamping col to the resulting line length.
func (m *model) setViLines(lines [][]rune, row, col int) {
	value := strings.Builder{}
	for i, l := range lines {
		if i > 0 {
			value.WriteByte('\n')
		}
		value.WriteString(string(l))
	}
	m.composer.SetValue(value.String())
	if row < 0 || row >= len(lines) {
		return
	}
	if col > len(lines[row]) {
		col = len(lines[row])
	}
	m.viSetCursor(row, col)
}

// viSetCursor places the composer caret at logical (row, col) by walking down
// from the start, since the textarea exposes no absolute cursor setter.
func (m *model) viSetCursor(row, col int) {
	m.composer.MoveToBegin()
	for m.composer.Line() < row {
		m.composer.CursorDown()
	}
	m.composer.SetCursorColumn(col)
}
