package cli

import (
	"strings"
	"unicode"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	rw "github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

// composerSelection is an editable textarea selection expressed as rune offsets
// into input.Value(). The value snapshot invalidates stale offsets whenever an
// unrelated path replaces the composer contents.
type composerSelection struct {
	active       bool
	anchor, head int
	value        string
}

// composerPromptWidth is reserved by textarea on every visual row. The first
// row paints "❯ "; continuation rows use the same-width blank gutter.
const composerPromptWidth = 2

const composerWheelRows = 3

type composerLayoutCache struct {
	value string
	width int
	rows  []composerVisualRow
}

func (s composerSelection) ordered() (start, end int) {
	if s.anchor > s.head {
		return s.head, s.anchor
	}
	return s.anchor, s.head
}

func (s composerSelection) empty() bool { return s.anchor == s.head }

type composerCell struct {
	r       rune
	offset  int
	lineCol int
}

type composerVisualRow struct {
	cells                    []composerCell
	logicalRow, logicalStart int
	logicalEnd, visualRow    int
	startOffset, endOffset   int
}

type composerCaret struct {
	offset, logicalRow, logicalCol, visualRow int
}

type composerCluster struct {
	startOffset, endOffset       int
	startLineCol, endLineCol     int
	startVisualCol, endVisualCol int
}

func composerCellsWidth(cells []composerCell) int {
	var b strings.Builder
	for _, cell := range cells {
		b.WriteRune(cell.r)
	}
	return uniseg.StringWidth(b.String())
}

func composerSpaces(cells []composerCell) []composerCell {
	out := make([]composerCell, len(cells))
	for i, cell := range cells {
		cell.r = ' '
		out[i] = cell
	}
	return out
}

// wrapComposerLine mirrors bubbles/textarea.wrap. The dependency does not
// export its visual rows, but mouse hit-testing must use the exact same word
// wrapping and wide-rune rules as the rendered textarea.
func wrapComposerLine(runes []rune, width, logicalRow, logicalStart int) []composerVisualRow {
	if width < 1 {
		width = 1
	}
	lines := [][]composerCell{{}}
	var word, spaces []composerCell
	row := 0
	for col, r := range runes {
		cell := composerCell{r: r, offset: logicalStart + col, lineCol: col}
		if unicode.IsSpace(r) {
			spaces = append(spaces, cell)
		} else {
			word = append(word, cell)
		}

		if len(spaces) > 0 {
			if composerCellsWidth(lines[row])+composerCellsWidth(word)+len(spaces) > width {
				row++
				lines = append(lines, []composerCell{})
			}
			lines[row] = append(lines[row], word...)
			lines[row] = append(lines[row], composerSpaces(spaces)...)
			word = nil
			spaces = nil
		} else if len(word) > 0 {
			lastWidth := rw.RuneWidth(word[len(word)-1].r)
			if composerCellsWidth(word)+lastWidth > width {
				if len(lines[row]) > 0 {
					row++
					lines = append(lines, []composerCell{})
				}
				lines[row] = append(lines[row], word...)
				word = nil
			}
		}
	}

	if composerCellsWidth(lines[row])+composerCellsWidth(word)+len(spaces) >= width {
		row++
		lines = append(lines, append([]composerCell{}, word...))
		lines[row] = append(lines[row], composerSpaces(spaces)...)
	} else {
		lines[row] = append(lines[row], word...)
		lines[row] = append(lines[row], composerSpaces(spaces)...)
	}

	logicalEnd := logicalStart + len(runes)
	// textarea appends one non-value space so a caret can sit at line end.
	lines[row] = append(lines[row], composerCell{r: ' ', offset: -1, lineCol: len(runes)})
	result := make([]composerVisualRow, len(lines))
	for i, cells := range lines {
		start, end := logicalEnd, logicalEnd
		for _, cell := range cells {
			if cell.offset < 0 {
				continue
			}
			if start == logicalEnd {
				start = cell.offset
			}
			end = cell.offset + 1
		}
		result[i] = composerVisualRow{
			cells: cells, logicalRow: logicalRow, logicalStart: logicalStart,
			logicalEnd: logicalEnd, startOffset: start, endOffset: end,
		}
	}
	return result
}

func composerLayout(value string, width int) []composerVisualRow {
	logicalLines := strings.Split(value, "\n")
	rows := make([]composerVisualRow, 0, len(logicalLines))
	offset := 0
	for logicalRow, line := range logicalLines {
		runes := []rune(line)
		wrapped := wrapComposerLine(runes, width, logicalRow, offset)
		for i := range wrapped {
			wrapped[i].visualRow = len(rows)
			rows = append(rows, wrapped[i])
		}
		offset += len(runes)
		if logicalRow+1 < len(logicalLines) {
			offset++ // explicit newline in input.Value()
		}
	}
	return rows
}

func (m *chatTUI) composerRows() []composerVisualRow {
	value, width := m.input.Value(), m.input.Width()
	if m.composerMap.value != value || m.composerMap.width != width || m.composerMap.rows == nil {
		m.composerMap = composerLayoutCache{value: value, width: width, rows: composerLayout(value, width)}
	}
	return m.composerMap.rows
}

func (m chatTUI) composerRowsForRender() []composerVisualRow {
	value, width := m.input.Value(), m.input.Width()
	if m.composerMap.value == value && m.composerMap.width == width && m.composerMap.rows != nil {
		return m.composerMap.rows
	}
	return composerLayout(value, width)
}

// composerViewOffset is the first visual row currently painted in the input.
// Normally bubbles/textarea owns it and keeps the insertion cursor visible. A
// mouse-wheel gesture temporarily detaches the painted viewport while leaving
// that cursor and the textarea's own offset untouched.
func (m chatTUI) composerViewOffset() int {
	rows := m.composerRowsForRender()
	maximum := max(0, len(rows)-m.input.Height())
	offset := m.input.ScrollYOffset()
	if m.composerScrollDetached {
		offset = m.composerScrollOffset
	}
	return min(max(offset, 0), maximum)
}

func (m *chatTUI) followComposerCursor() {
	m.composerScrollDetached = false
	m.composerScrollOffset = m.input.ScrollYOffset()
}

// scrollComposer moves only the composer's painted viewport. It returns false
// at an edge so the caller can continue the same wheel gesture in the transcript.
func (m *chatTUI) scrollComposer(delta int) bool {
	if delta == 0 || m.hideComposer() || m.input.Height() <= 0 {
		return false
	}
	maximum := max(0, len(m.composerRows())-m.input.Height())
	if maximum == 0 {
		return false
	}
	current := m.composerViewOffset()
	next := min(max(current+delta, 0), maximum)
	if next == current {
		return false
	}
	m.composerScrollOffset = next
	m.composerScrollDetached = next != m.input.ScrollYOffset()
	return true
}

func (m chatTUI) mouseOverComposer(screenX, screenY int) bool {
	if m.hideComposer() || screenX < 0 || screenX >= max(m.width, 10) {
		return false
	}
	_, contentY, ok := m.composerOrigin()
	if !ok {
		return false
	}
	// Include both horizontal border rows so a wheel gesture anywhere over the
	// visible composer card has the same target.
	return screenY >= contentY-1 && screenY <= contentY+m.input.Height()
}

// composerCursor maps the textarea's real insertion cursor into the manually
// scrolled viewport. The cursor is hidden when the user has scrolled it out of
// view; typing or a cursor key reattaches the viewport and shows it again.
func (m chatTUI) composerCursor() *tea.Cursor {
	cur := m.input.Cursor()
	if cur == nil || !m.composerScrollDetached {
		return cur
	}
	absoluteRow := m.input.ScrollYOffset() + cur.Y
	cur.Y = absoluteRow - m.composerViewOffset()
	if cur.Y < 0 || cur.Y >= m.input.Height() {
		return nil
	}
	return cur
}

func composerClusters(row composerVisualRow) []composerCluster {
	actual := make([]composerCell, 0, len(row.cells))
	var text strings.Builder
	for _, cell := range row.cells {
		if cell.offset < 0 {
			continue
		}
		actual = append(actual, cell)
		text.WriteRune(cell.r)
	}
	clusters := make([]composerCluster, 0, len(actual))
	graphemes := uniseg.NewGraphemes(text.String())
	cellIndex := 0
	visualCol := 0
	for graphemes.Next() {
		clusterRunes := graphemes.Runes()
		if len(clusterRunes) == 0 || cellIndex >= len(actual) {
			continue
		}
		endIndex := min(cellIndex+len(clusterRunes), len(actual))
		first := actual[cellIndex]
		last := actual[endIndex-1]
		width := graphemes.Width()
		clusters = append(clusters, composerCluster{
			startOffset: first.offset, endOffset: last.offset + 1,
			startLineCol: first.lineCol, endLineCol: last.lineCol + 1,
			startVisualCol: visualCol, endVisualCol: visualCol + width,
		})
		visualCol += width
		cellIndex = endIndex
	}
	return clusters
}

func (row composerVisualRow) caretAt(x int) composerCaret {
	if x < 0 {
		x = 0
	}
	lastOffset := row.startOffset
	lastLineCol := 0
	for _, cluster := range composerClusters(row) {
		width := cluster.endVisualCol - cluster.startVisualCol
		if x <= cluster.startVisualCol ||
			(x < cluster.endVisualCol && x-cluster.startVisualCol < (width+1)/2) {
			return composerCaret{cluster.startOffset, row.logicalRow, cluster.startLineCol, row.visualRow}
		}
		lastOffset = cluster.endOffset
		lastLineCol = cluster.endLineCol
		if x < cluster.endVisualCol {
			return composerCaret{lastOffset, row.logicalRow, lastLineCol, row.visualRow}
		}
	}
	return composerCaret{lastOffset, row.logicalRow, lastLineCol, row.visualRow}
}

func composerCaretForOffset(rows []composerVisualRow, offset int) composerCaret {
	if len(rows) == 0 {
		return composerCaret{}
	}
	for i, row := range rows {
		if offset < row.endOffset || offset == row.startOffset {
			return composerCaret{offset, row.logicalRow, offset - row.logicalStart, row.visualRow}
		}
		if offset == row.endOffset {
			if i+1 < len(rows) && rows[i+1].startOffset == offset {
				continue
			}
			return composerCaret{offset, row.logicalRow, offset - row.logicalStart, row.visualRow}
		}
	}
	last := rows[len(rows)-1]
	return composerCaret{last.logicalEnd, last.logicalRow, last.logicalEnd - last.logicalStart, last.visualRow}
}

func (m chatTUI) validComposerSelection() bool {
	return m.composerSel.active && m.composerSel.value == m.input.Value()
}

func (m chatTUI) selectedComposerText() string {
	if !m.validComposerSelection() || m.composerSel.empty() {
		return ""
	}
	start, end := m.composerSel.ordered()
	runes := []rune(m.input.Value())
	if start < 0 || end > len(runes) {
		return ""
	}
	return string(runes[start:end])
}

// composerOrigin returns the terminal cell occupied by textarea content (after
// the input box's top border, left padding, and prompt gutter). Deriving it from
// the two cursor positions keeps hit-testing aligned with every optional panel
// above the box.
func (m chatTUI) composerOrigin() (x, y int, ok bool) {
	if m.hideComposer() {
		return 0, 0, false
	}
	local := m.input.Cursor()
	// Derive the stable layout origin from the normal caret-following frame even
	// while the manually scrolled frame has hidden the insertion cursor.
	normal := m
	normal.composerScrollDetached = false
	view := normal.View()
	if local == nil || view.Cursor == nil {
		return 0, 0, false
	}
	return view.Cursor.X - local.X + composerPromptWidth, view.Cursor.Y - local.Y, true
}

func (m *chatTUI) composerCaretAt(screenX, screenY int, clamp bool) (composerCaret, bool) {
	x, y, ok := m.composerOrigin()
	if !ok {
		return composerCaret{}, false
	}
	relY := screenY - y
	if !clamp && (relY < 0 || relY >= m.input.Height()) {
		return composerCaret{}, false
	}
	if relY < 0 {
		relY = 0
	}
	if relY >= m.input.Height() {
		relY = m.input.Height() - 1
	}
	rows := m.composerRows()
	visualRow := m.composerViewOffset() + relY
	if visualRow < 0 {
		visualRow = 0
	}
	if visualRow >= len(rows) {
		visualRow = len(rows) - 1
	}
	return rows[visualRow].caretAt(screenX - x), true
}

func (m *chatTUI) setComposerCursor(offset int) {
	m.followComposerCursor()
	rows := m.composerRows()
	caret := composerCaretForOffset(rows, offset)
	m.input.MoveToBegin()
	for i := 0; i < caret.visualRow; i++ {
		m.input.CursorDown()
	}
	m.input.SetCursorColumn(caret.logicalCol)
}

func (m *chatTUI) deleteComposerSelection() bool {
	if !m.validComposerSelection() || m.composerSel.empty() {
		m.composerSel = composerSelection{}
		return false
	}
	start, end := m.composerSel.ordered()
	runes := []rune(m.input.Value())
	if start < 0 || end > len(runes) {
		m.composerSel = composerSelection{}
		return false
	}
	m.input.SetValue(string(runes[:start]) + string(runes[end:]))
	m.composerSel = composerSelection{}
	m.setComposerCursor(start)
	return true
}

func composerSelectionDeletes(msg tea.KeyPressMsg, keyMap textarea.KeyMap) bool {
	return key.Matches(msg, keyMap.DeleteAfterCursor) ||
		key.Matches(msg, keyMap.DeleteBeforeCursor) ||
		key.Matches(msg, keyMap.DeleteCharacterBackward) ||
		key.Matches(msg, keyMap.DeleteCharacterForward) ||
		key.Matches(msg, keyMap.DeleteWordBackward) ||
		key.Matches(msg, keyMap.DeleteWordForward)
}

func composerSelectionReplaces(msg tea.KeyPressMsg, keyMap textarea.KeyMap) bool {
	if key.Matches(msg, keyMap.InsertNewline) {
		return true
	}
	if msg.Text == "" {
		return false
	}
	commandMods := tea.ModCtrl | tea.ModMeta | tea.ModHyper | tea.ModSuper
	return msg.Key().Mod&commandMods == 0
}

func composerRowSelectionSpan(row composerVisualRow, start, end int) (lo, hi int, ok bool) {
	visualCol := 0
	for _, cluster := range composerClusters(row) {
		if cluster.endOffset > start && cluster.startOffset < end {
			if !ok {
				lo = cluster.startVisualCol
				ok = true
			}
			hi = cluster.endVisualCol
		}
		visualCol = cluster.endVisualCol
	}
	// Make an explicitly selected newline visible, including on blank lines:
	// it occupies the textarea's trailing caret space, one cell past the row
	// content (after the loop visualCol is the row's full content width).
	if end > row.logicalEnd && start <= row.logicalEnd && row.endOffset == row.logicalEnd {
		if !ok {
			lo = visualCol
			ok = true
		}
		hi = max(hi, visualCol+1)
	}
	return lo, hi, ok
}

func (m chatTUI) renderComposerInput() string {
	view := m.input.View()
	visualStart := m.input.ScrollYOffset()
	if m.composerScrollDetached {
		view = m.renderDetachedComposerInput()
		visualStart = m.composerViewOffset()
	}
	if !m.validComposerSelection() || m.composerSel.empty() {
		return view
	}
	start, end := m.composerSel.ordered()
	rows := m.composerRowsForRender()
	lines := strings.Split(view, "\n")
	for i := range lines {
		visualRow := visualStart + i
		if visualRow >= len(rows) {
			break
		}
		if lo, hi, ok := composerRowSelectionSpan(rows[visualRow], start, end); ok {
			lines[i] = lipgloss.StyleRanges(lines[i], lipgloss.NewRange(
				lo+composerPromptWidth,
				hi+composerPromptWidth,
				selStyle,
			))
		}
	}
	return strings.Join(lines, "\n")
}

// renderDetachedComposerInput asks the same textarea implementation to render
// the manually selected slice. A temporary model preserves exact wrapping,
// padding, prompt styling, and wide-rune behavior without mutating the real
// textarea's cursor or viewport.
func (m chatTUI) renderDetachedComposerInput() string {
	display := textarea.New()
	configureChatTextarea(&display)
	display.SetStyles(m.input.Styles())
	display.DynamicHeight = false
	display.SetWidth(m.input.Width() + composerPromptWidth)
	display.SetHeight(m.input.Height())
	display.SetValue(m.input.Value())
	// textarea's public cursor motions scroll its embedded viewport only after
	// that viewport has content. Seed it once, then position the throwaway cursor
	// on the last row of the requested slice so caret-following yields the exact
	// top offset we want to paint.
	_ = display.View()
	display.MoveToBegin()

	rows := m.composerRowsForRender()
	targetRow := min(m.composerViewOffset()+m.input.Height()-1, len(rows)-1)
	for range max(targetRow, 0) {
		display.CursorDown()
	}
	return display.View()
}
