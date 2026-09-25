package tui

import (
	"os"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/base/i18n"
	"reasonix/internal/frontend/termrender"
)

const (
	wheelRows    = 3
	flashFor     = 2 * time.Second
	scrollbarCol = 1
)

// screen is the full-screen transcript: settled rows kept here rather than in
// the terminal's scrollback, drawn through a viewport with its own scrollbar,
// wheel and selection. Nil means the rows go to the terminal's scrollback.
type screen struct {
	blocks []block
	yoff   int
	follow bool
	// mouseOff hands the mouse back to the terminal for its own selection.
	mouseOff bool
	sel      selection
	drag     bool
	grab     int
	flash    string
	flashAt  time.Time
	// edge is the direction a selection held against the top or bottom of
	// the viewport scrolls it, and dragX the column that drag is at.
	edge  int
	dragX int
}

// block is one settled print, kept as how to draw it so a resize redraws the
// transcript at the new width rather than keeping the old wrapping.
type block struct {
	render func(width int) string
	width  int
	lines  []string
	// row is the settled row the block draws, when it draws one: a shell
	// call's output opens and shuts through it.
	row *Item
}

func (b *block) at(width int) []string {
	if b.lines == nil || b.width != width {
		b.width, b.lines = width, wrapLines(b.render(width), width)
	}
	return b.lines
}

type selPos struct{ line, col int }

type selection struct {
	active       bool
	anchor, head selPos
}

func (s selection) ordered() (selPos, selPos) {
	a, h := s.anchor, s.head
	if h.line < a.line || (h.line == a.line && h.col < a.col) {
		return h, a
	}
	return a, h
}

func (s selection) empty() bool { return s.anchor == s.head }

type (
	flashDoneMsg struct{}
	edgeMsg      struct{}
)

const (
	edgeEvery = 80 * time.Millisecond
	// printGap lets the renderer redraw between two pieces of one print: it
	// places each piece from where the last redraw left the frame.
	printGap = 40 * time.Millisecond
)

// wrapLines splits out into rows no wider than width, each padded to it so
// the scrollbar column stays put.
func wrapLines(out string, width int) []string {
	if out == "" {
		return nil
	}
	var rows []string
	for l := range strings.SplitSeq(out, "\n") {
		for r := range strings.SplitSeq(ansi.Hardwrap(termrender.ExpandTabs(l), width, true), "\n") {
			rows = append(rows, termrender.PadRight(r, width))
		}
	}
	return rows
}

// settledPrint is one settled piece of the transcript: how to draw it, and
// the row it draws when it draws one.
type settledPrint struct {
	render func(width int) string
	row    *Item
}

// settledRow keeps a copy of the row to draw from. Full screen, a shell
// call's output starts shut and can open later.
func (m *model) settledRow(row Item, shown int) settledPrint {
	if m.scr != nil && row.Kind == ItemTool {
		row.Fold = foldShut
	}
	return settledPrint{render: func(w int) string { return renderItem(&row, w, shown) }, row: &row}
}

// publish sends what settled where this screen keeps it: blocks of the full
// screen transcript, or one print into the terminal's scrollback.
func (m *model) publish(out []settledPrint) tea.Cmd {
	if m.scr != nil {
		for _, p := range out {
			m.scr.blocks = append(m.scr.blocks, block{render: p.render, row: p.row})
		}
		return nil
	}
	parts := make([]string, 0, len(out))
	for _, p := range out {
		if s := p.render(m.width); s != "" {
			parts = append(parts, s)
		}
	}
	return m.printAbove(strings.Join(parts, "\n"))
}

func (m *model) emit(render func(int) string) tea.Cmd {
	return m.publish([]settledPrint{{render: render}})
}

// fillScreen pushes whatever the terminal shows into its scrollback before a
// burst of prints. The renderer places a print by scrolling it in above a
// frame it takes to sit at the bottom of the screen; on a screen not yet full
// the frame sits higher, and prints that arrive before a redraw land out of
// order.
func (m *model) fillScreen() tea.Cmd {
	if m.scr != nil {
		return nil
	}
	return tea.Println(strings.Repeat("\n", max(m.height-2, 0)))
}

// foldable reports a block whose shell output has more than its preview.
func (b *block) foldable() bool {
	r := b.row
	if r == nil || r.Kind != ItemTool || r.Tool == nil || !termrender.IsShellTool(r.Tool.Name) {
		return false
	}
	return strings.Count(strings.TrimRight(r.Tool.Output, "\n"), "\n")+1 > shellPreviewLines
}

func (b *block) toggle() {
	b.row.Fold = foldShut + foldOpen - b.row.Fold
	b.lines = nil
}

// toggleLatestShell opens or shuts the newest shell output that has more to
// show than its preview.
func (m *model) toggleLatestShell() {
	for i := range slices.Backward(m.scr.blocks) {
		if b := &m.scr.blocks[i]; b.foldable() {
			b.toggle()
			return
		}
	}
}

// blockEndingAt is the settled block whose last row is transcript row idx.
func (m *model) blockEndingAt(idx int) *block {
	cw, at := m.contentWidth(), 0
	for i := range m.scr.blocks {
		at += len(m.scr.blocks[i].at(cw))
		if at-1 == idx {
			return &m.scr.blocks[i]
		}
		if at > idx {
			return nil
		}
	}
	return nil
}

// printAbove prints out into the terminal's scrollback in pieces the
// renderer can place. It makes room for a print by scrolling it in and then
// climbing over the frame, so a print taller than the rows above the frame
// climbs past the top of the screen and lands out of order.
func (m *model) printAbove(out string) tea.Cmd {
	if out == "" {
		return nil
	}
	var prints []tea.Cmd
	for i, piece := range pieces(out, max(min(m.height-m.frameRows-1, m.height/2), 1), m.width) {
		if i > 0 {
			prints = append(prints, tea.Tick(printGap, func(time.Time) tea.Msg { return nil }))
		}
		prints = append(prints, tea.Println(piece))
	}
	return tea.Sequence(prints...)
}

// pieces splits out into runs of at most room terminal rows, counting a line
// wider than width as the rows it wraps to. A single line taller than room
// still goes whole.
func pieces(out string, room, width int) []string {
	lines := strings.Split(out, "\n")
	var outs []string
	for len(lines) > 0 {
		n := 0
		for rows := 0; n < len(lines); n++ {
			rows += 1 + ansi.StringWidth(lines[n])/max(width, 1)
			if rows > room && n > 0 {
				break
			}
		}
		outs = append(outs, strings.Join(lines[:n], "\n"))
		lines = lines[n:]
	}
	return outs
}

func (m *model) contentWidth() int { return max(m.width-scrollbarCol, 10) }

// content is every transcript row: the settled blocks, then what is live.
func (m *model) content(live []string) []string {
	cw := m.contentWidth()
	var rows []string
	for i := range m.scr.blocks {
		rows = append(rows, m.scr.blocks[i].at(cw)...)
	}
	return append(rows, wrapLines(strings.Join(live, "\n"), cw)...)
}

// fullView draws the viewport over the transcript with the bottom region
// pinned under it.
func (m *model) fullView(bottom []string, composerAt int) tea.View {
	s := m.scr
	h := max(m.height-len(bottom), 1)
	rows := m.content(m.liveLines())
	total := len(rows)
	if s.follow {
		s.yoff = total - h
	}
	s.yoff = max(min(s.yoff, total-h), 0)
	cw := m.contentWidth()
	blank := strings.Repeat(" ", cw)
	thumbStart, thumbSize := scrollbarThumb(h, s.yoff, total)
	lo, hi := s.sel.ordered()
	out := make([]string, 0, h+len(bottom))
	for r := range h {
		idx := s.yoff + r
		line := blank
		if idx < total {
			line = rows[idx]
		}
		if s.sel.active && !s.sel.empty() {
			if a, b, ok := selSpan(idx, lo, hi, cw); ok {
				line = lipgloss.StyleRanges(line, lipgloss.NewRange(a, b, lipgloss.NewStyle().Reverse(true)))
			}
		}
		out = append(out, line+scrollbarCell(r, total, h, thumbStart, thumbSize))
	}
	for _, l := range bottom {
		out = append(out, ansi.Truncate(l, max(m.width-1, 1), ""))
	}
	v := tea.NewView(strings.Join(out, "\n"))
	v.AltScreen = true
	if !s.mouseOff {
		v.MouseMode = tea.MouseModeCellMotion
	}
	if c := m.composer.Cursor(); c != nil && composerAt >= 0 {
		c.X += 3
		c.Y += h + composerAt + 1
		v.Cursor = c
	}
	return v
}

func (m *model) viewportHeight() int {
	return max(m.height-len(m.bottomLines().rows), 1)
}

func scrollbarThumb(height, yoff, total int) (start, size int) {
	if total <= height {
		return 0, 0
	}
	size = max(height*height/total, 1)
	start = min(yoff*(height-size)/(total-height), height-size)
	return start, size
}

func scrollbarCell(row, total, height, thumbStart, thumbSize int) string {
	if total <= height {
		return " "
	}
	if row >= thumbStart && row < thumbStart+thumbSize {
		return termrender.Accent("█")
	}
	return termrender.Dim("│")
}

// selSpan is the [lo, hi) cell span the selection covers on row idx.
func selSpan(idx int, start, end selPos, cw int) (lo, hi int, ok bool) {
	if idx < start.line || idx > end.line {
		return 0, 0, false
	}
	lo, hi = 0, cw
	if idx == start.line {
		lo = start.col
	}
	if idx == end.line {
		hi = min(end.col, cw)
	}
	return lo, hi, lo < hi
}

// scrollBy moves the viewport and follows the tail again once it reaches it.
func (m *model) scrollBy(n int) {
	s := m.scr
	h := m.viewportHeight()
	total := len(m.content(m.liveLines()))
	s.yoff = max(min(s.yoff+n, total-h), 0)
	s.follow = s.yoff >= total-h
}

// scrollKey takes the keys that move the transcript; they are never text.
func (m *model) scrollKey(k string) bool {
	if m.scr == nil {
		return false
	}
	page := max(m.viewportHeight()-1, 1)
	half := max(m.viewportHeight()/2, 1)
	switch k {
	case "pgup":
		m.scrollBy(-page)
	case "pgdown":
		m.scrollBy(page)
	case "shift+pgup":
		m.scrollBy(-half)
	case "shift+pgdown":
		m.scrollBy(half)
	case "shift+up":
		m.scrollBy(-1)
	case "shift+down":
		m.scrollBy(1)
	case "ctrl+home":
		m.scr.yoff, m.scr.follow = 0, false
	case "ctrl+end":
		m.scr.follow = true
	case "ctrl+b":
		m.toggleLatestShell()
	default:
		return false
	}
	return true
}

func (m *model) onMouse(msg tea.MouseMsg) tea.Cmd {
	s := m.scr
	if s == nil || s.mouseOff {
		return nil
	}
	mouse := msg.Mouse()
	h := m.viewportHeight()
	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.scrollBy(-wheelRows)
		case tea.MouseWheelDown:
			m.scrollBy(wheelRows)
		}
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseRight && s.sel.active && !s.sel.empty() {
			return m.copySelection()
		}
		if msg.Button != tea.MouseLeft || mouse.Y >= h {
			return nil
		}
		s.sel = selection{}
		if mouse.X >= m.contentWidth() {
			s.drag = true
			s.grab = m.thumbGrab(mouse.Y, h)
			m.dragScrollbar(mouse.Y, h)
			return nil
		}
		if b := m.blockEndingAt(s.yoff + mouse.Y); b != nil && b.foldable() {
			b.toggle()
			return nil
		}
		at := m.caret(mouse.X, mouse.Y)
		s.sel = selection{active: true, anchor: at, head: at}
	case tea.MouseMotionMsg:
		switch {
		case s.drag:
			m.dragScrollbar(mouse.Y, h)
		case s.sel.active:
			s.sel.head = m.caret(mouse.X, min(max(mouse.Y, 0), h-1))
			prev := s.edge
			s.edge, s.dragX = edgeDir(mouse.Y, h), mouse.X
			if s.edge != 0 && prev == 0 {
				return edgeTick()
			}
		}
	case tea.MouseReleaseMsg:
		s.edge = 0
		if s.drag {
			s.drag = false
			return nil
		}
		if s.sel.active {
			if s.sel.empty() {
				s.sel = selection{}
				return nil
			}
			return m.copySelection()
		}
	}
	return nil
}

func (m *model) caret(x, y int) selPos {
	return selPos{line: m.scr.yoff + y, col: min(max(x, 0), m.contentWidth())}
}

func (m *model) thumbGrab(row, h int) int {
	total := len(m.content(m.liveLines()))
	start, size := scrollbarThumb(h, m.scr.yoff, total)
	if row >= start && row < start+size {
		return row - start
	}
	return size / 2
}

func (m *model) dragScrollbar(row, h int) {
	total := len(m.content(m.liveLines()))
	_, size := scrollbarThumb(h, 0, total)
	maxTop := h - size
	if total <= h || maxTop <= 0 {
		return
	}
	top := min(max(row-m.scr.grab, 0), maxTop)
	m.scr.yoff = (top*(total-h) + maxTop/2) / maxTop
	m.scr.follow = m.scr.yoff >= total-h
}

// copySelection puts the selected text on the clipboard; the highlight stays
// as the cue for what was copied.
func (m *model) copySelection() tea.Cmd {
	return termrender.CopyToClipboard(m.selectedText())
}

func (m *model) selectedText() string {
	rows := m.content(m.liveLines())
	lo, hi := m.scr.sel.ordered()
	var picked []string
	for i := lo.line; i <= hi.line && i < len(rows); i++ {
		a, b, ok := selSpan(i, lo, hi, m.contentWidth())
		if !ok {
			continue
		}
		picked = append(picked, strings.TrimRight(ansi.Strip(ansi.Cut(rows[i], a, b)), " "))
	}
	return strings.Join(picked, "\n")
}

// onCopied reports a copy in the footer for a moment.
func (m *model) onCopied(msg termrender.ClipboardCopyMsg) tea.Cmd {
	if msg.Err != nil {
		m.tr.AddNotice("error", "copy: "+msg.Err.Error())
		return m.commit()
	}
	cmds := []tea.Cmd{m.showFlash(i18n.M.MouseCopiedHint)}
	if msg.OSC52 {
		cmds = append(cmds, tea.SetClipboard(msg.Text))
	}
	return tea.Batch(cmds...)
}

func (m *model) showFlash(text string) tea.Cmd {
	if m.scr == nil {
		return nil
	}
	m.scr.flash, m.scr.flashAt = text, time.Now()
	return tea.Tick(flashFor, func(time.Time) tea.Msg { return flashDoneMsg{} })
}

func (m *model) clearFlash() {
	if m.scr != nil {
		m.scr.flash = ""
	}
}

func (m *model) flashText() string {
	if m.scr == nil || m.scr.flash == "" || time.Since(m.scr.flashAt) >= flashFor {
		return ""
	}
	return m.scr.flash
}

// toggleMouse gives the mouse back to the terminal, or takes it again.
// mouseCaptureOffByDefault hands the mouse to the terminal over SSH, where the
// native selection reaches the user's clipboard and capture cannot. 1.x's
// REASONIX_DISABLE_MOUSE decides instead when set: 0 captures, anything else not.
func mouseCaptureOffByDefault() bool {
	if v := strings.TrimSpace(os.Getenv("REASONIX_DISABLE_MOUSE")); v != "" {
		return v != "0"
	}
	return termrender.RemoteClipboardSession()
}

func (m *model) toggleMouse() tea.Cmd {
	if m.scr == nil {
		return nil
	}
	s := m.scr
	s.mouseOff, s.sel, s.drag = !s.mouseOff, selection{}, false
	if s.mouseOff {
		return m.showFlash(i18n.M.MouseCaptureOffHint)
	}
	return m.showFlash(i18n.M.MouseCaptureOnHint)
}

func edgeTick() tea.Cmd { return tea.Tick(edgeEvery, func(time.Time) tea.Msg { return edgeMsg{} }) }

// edgeDir is -1 for a drag at the viewport's top row, 1 at its bottom row.
func edgeDir(y, h int) int {
	switch {
	case y <= 0:
		return -1
	case y >= h-1:
		return 1
	}
	return 0
}

// onEdge scrolls a selection held against an edge one row and keeps going
// until the drag leaves the edge or the transcript runs out.
func (m *model) onEdge() tea.Cmd {
	s := m.scr
	if s == nil || !s.sel.active || s.edge == 0 {
		return nil
	}
	before := s.yoff
	m.scrollBy(s.edge)
	row := 0
	if s.edge > 0 {
		row = m.viewportHeight() - 1
	}
	s.sel.head = m.caret(s.dragX, row)
	if s.yoff == before {
		s.edge = 0
		return nil
	}
	return edgeTick()
}
