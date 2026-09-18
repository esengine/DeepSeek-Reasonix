package cli

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// viActive reports whether the composer uses the vi command mode, enabled by
// ui.commandmode = "vi". When off the composer edits insert-only as before.
func (m chatTUI) viActive() bool {
	return m.cfg != nil && m.cfg.UICommandMode()
}

// viInCommand reports whether the composer is currently in vi command (normal)
// mode rather than insert mode.
func (m chatTUI) viInCommand() bool {
	return m.viActive() && m.viCmd
}

// viLines splits the composer into its logical lines so command mode can move a
// caret across pasted multi-line content with rune-accurate columns.
func (m chatTUI) viLines() [][]rune {
	raw := strings.Split(m.input.Value(), "\n")
	lines := make([][]rune, 0, len(raw))
	for _, l := range raw {
		lines = append(lines, []rune(l))
	}
	return lines
}

// viMoveLeft moves the caret one rune left, crossing to the end of the previous
// logical line when it is at the very start.
func (m *chatTUI) viMoveLeft() {
	col := m.input.Column()
	if col > 0 {
		m.input.SetCursorColumn(col - 1)
		return
	}
	if m.input.Line() <= 0 {
		return
	}
	m.input.CursorUp()
	lines := m.viLines()
	if r := m.input.Line(); r < len(lines) {
		m.input.SetCursorColumn(len(lines[r]))
	}
}

// viMoveRight moves the caret one rune right, crossing to the start of the next
// logical line when it is at the very end.
func (m *chatTUI) viMoveRight() {
	lines := m.viLines()
	row := m.input.Line()
	if row >= len(lines) {
		return
	}
	if m.input.Column() < len(lines[row]) {
		m.input.SetCursorColumn(m.input.Column() + 1)
		return
	}
	if row+1 < len(lines) {
		m.input.CursorDown()
		m.input.SetCursorColumn(0)
	}
}

// viMoveFirstNonBlank puts the caret on the first non-whitespace rune of the
// current line (the vi "^"), falling back to column 0 on blank lines.
func (m *chatTUI) viMoveFirstNonBlank() {
	lines := m.viLines()
	row := m.input.Line()
	if row >= len(lines) {
		return
	}
	line := lines[row]
	col := 0
	for col < len(line) && unicode.IsSpace(line[col]) {
		col++
	}
	m.input.SetCursorColumn(col)
}

// viCommandRune handles a single printable key while the composer is in vi
// command mode. Command-mode keys are consumed and acted on; any other key is
// ignored rather than inserting text. The returned bool reports whether the key
// was consumed (always true — insert only happens through i/a).
func (m chatTUI) viCommandRune(msg tea.KeyPressMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "h":
		m.viMoveLeft()
	case "l":
		m.viMoveRight()
	case "j":
		m.input.CursorDown()
	case "k":
		m.input.CursorUp()
	case "0":
		m.input.SetCursorColumn(0)
	case "$":
		m.input.CursorEnd()
	case "^":
		m.viMoveFirstNonBlank()
	case "i":
		m.viCmd = false
	case "a":
		m.viMoveRight()
		m.viCmd = false
	case "x":
		m.viDeleteChar()
		return m, nil
	case "I":
		m.viMoveFirstNonBlank()
		m.viCmd = false
	case "A":
		m.input.CursorEnd()
		m.viCmd = false
	case "p":
		cmds = append(cmds, pasteClipboardText())
		return m, finalize(m, cmds)
	}
	m.followComposerCursor()
	return m, nil
}

// viDeleteChar removes the character under the caret (the rune to its right) in
// command mode, the vi "x". If the caret sits past the end of a line there is
// nothing to delete and the text is left untouched.
func (m *chatTUI) viDeleteChar() {
	lines := m.viLines()
	row := m.input.Line()
	if row < 0 || row >= len(lines) {
		return
	}
	line := lines[row]
	col := m.input.Column()
	if col >= len(line) {
		return
	}
	lines[row] = append(line[:col], line[col+1:]...)
	m.setViLines(lines, row, col)
	m.growInputToFit()
}

// setViLines rewrites the composer from its logical lines and places the caret
// at (row, col), clamping col to the resulting line length.
func (m *chatTUI) setViLines(lines [][]rune, row, col int) {
	value := strings.Builder{}
	for i, l := range lines {
		if i > 0 {
			value.WriteByte('\n')
		}
		value.WriteString(string(l))
	}
	m.input.SetValue(value.String())
	if row < 0 || row >= len(lines) {
		return
	}
	if col > len(lines[row]) {
		col = len(lines[row])
	}
	m.setComposerCursor(viOffsetFor(lines, row, col))
}

// viOffsetFor converts a logical (row, col) caret into a byte offset over the
// whole composer value.
func viOffsetFor(lines [][]rune, row, col int) int {
	offset := 0
	for r := 0; r < row && r < len(lines); r++ {
		offset += len(lines[r]) + 1
	}
	return offset + col
}
