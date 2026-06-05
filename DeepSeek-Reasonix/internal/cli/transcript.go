package cli

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"
)

// wrapTranscript wraps the joined transcript to width for the viewport, keeping
// SGR balanced across wrap points. ansi.Hardwrap leaves a style that spans a
// break open at the line end (e.g. a wrapped dim link tail), which bleeds the
// attribute into the padding and the next row on stricter terminals (Warp).
// lipgloss closes the active style at each line end and reopens it at the next.
func wrapTranscript(s string, width int) string {
	if width <= 0 {
		return s
	}
	return lipgloss.NewStyle().Width(width).Render(s)
}

// clipboardWriteAll is the platform clipboard writer; a var so tests can force
// the failure path (the tmux / SSH scenario) without a real display server.
var clipboardWriteAll = clipboard.WriteAll

// copyToClipboard writes text to the system clipboard. It first tries the
// platform tool (xclip / xsel / wl-copy / pbcopy) via atotto; when that fails —
// typically inside tmux or over SSH where the display server is unreachable — it
// falls back to OSC 52, which tmux and modern terminals forward to the host
// clipboard. tea.SetClipboard's command is *run* here so the message it yields
// (handled by the runtime) reaches the event loop; returning the command itself
// would be dropped as an unrecognized message and emit nothing.
func copyToClipboard(text string) tea.Cmd {
	return func() tea.Msg {
		if err := clipboardWriteAll(text); err != nil {
			return tea.SetClipboard(text)()
		}
		return nil
	}
}

// autoScrollMsg drives one step of edge-drag scrolling while a selection is held
// against the top or bottom of the transcript.
type autoScrollMsg struct{}

func autoScrollTick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg { return autoScrollMsg{} })
}

// edgeScrollDir reports the auto-scroll direction for a drag at screen row y in
// a viewport of `height` rows: -1 at the top edge, +1 at the bottom, 0 between.
func edgeScrollDir(y, height int) int {
	switch {
	case y <= 0:
		return -1
	case y >= height-1:
		return 1
	default:
		return 0
	}
}

// selPos is a caret position in the wrapped transcript: a content-line index
// (absolute, scroll-independent) and a visual column.
type selPos struct{ line, col int }

// selection is the live left-drag text selection over the transcript. anchor is
// where the drag began, head where it currently is; active gates rendering and
// copy. Coordinates are absolute content lines so scrolling never moves them.
type selection struct {
	active       bool
	anchor, head selPos
}

func (s selection) ordered() (start, end selPos) {
	if s.anchor.line > s.head.line || (s.anchor.line == s.head.line && s.anchor.col > s.head.col) {
		return s.head, s.anchor
	}
	return s.anchor, s.head
}

func (s selection) empty() bool { return s.anchor == s.head }

var (
	selStyle         = lipgloss.NewStyle().Reverse(true)
	scrollThumbStyle lipgloss.Style
	scrollTrackStyle lipgloss.Style
)

// renderTranscript draws the viewport's visible window with a scrollbar in the
// last column and the active selection reverse-highlighted. The content lines
// (m.wrappedLines) are already padded to cw by wrapTranscript, so this stays
// cheap per frame — important because a drag re-renders on every mouse move.
func (m chatTUI) renderTranscript() string {
	h := m.viewport.Height()
	if h <= 0 {
		return ""
	}
	cw := m.viewport.Width() // content width; the scrollbar occupies one more column
	lines := m.wrappedLines
	total := len(lines)
	yoff := m.viewport.YOffset()
	start, end := m.sel.ordered()
	thumbStart, thumbSize := scrollbarThumb(h, yoff, total)
	blank := strings.Repeat(" ", cw)

	rows := make([]string, h)
	bar := make([]string, h)
	for r := 0; r < h; r++ {
		idx := yoff + r
		line := blank // off-content rows fill to width
		if idx >= 0 && idx < total {
			line = lines[idx] // already cw-wide from wrapTranscript
		}
		if m.sel.active && !m.sel.empty() {
			if lo, hi, ok := selSpan(idx, start, end, cw); ok {
				line = lipgloss.StyleRanges(line, lipgloss.NewRange(lo, hi, selStyle))
			}
		}
		rows[r] = line
		bar[r] = scrollbarCell(r, total, h, thumbStart, thumbSize)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(rows, "\n"), strings.Join(bar, "\n"))
}

// selSpan returns the [lo, hi) visual-column span of the selection on content
// line idx (false when the line is outside the selection). cw bounds the span
// so a multi-line selection highlights through the right edge.
func selSpan(idx int, start, end selPos, cw int) (lo, hi int, ok bool) {
	if idx < start.line || idx > end.line {
		return 0, 0, false
	}
	lo, hi = 0, cw
	if idx == start.line {
		lo = start.col
	}
	if idx == end.line {
		hi = end.col
	}
	if hi > cw {
		hi = cw
	}
	if lo >= hi {
		return 0, 0, false
	}
	return lo, hi, true
}

// selectedText is the plain (ANSI-stripped) text of the active selection, lines
// joined with '\n', for the clipboard.
func (m chatTUI) selectedText() string {
	if !m.sel.active || m.sel.empty() {
		return ""
	}
	lines := m.wrappedLines
	start, end := m.sel.ordered()
	var out []string
	for idx := start.line; idx <= end.line && idx < len(lines); idx++ {
		lo, hi := 0, ansi.StringWidth(lines[idx])
		if idx == start.line {
			lo = start.col
		}
		if idx == end.line {
			hi = end.col
		}
		out = append(out, strings.TrimRight(ansi.Strip(ansi.Cut(lines[idx], lo, hi)), " "))
	}
	return strings.Join(out, "\n")
}

// scrollbarThumb returns the thumb's [start, start+size) row span for a viewport
// of `height` rows showing `total` content lines scrolled to `yoff`.
func scrollbarThumb(height, yoff, total int) (start, size int) {
	if total <= height {
		return 0, 0 // no overflow → no thumb
	}
	size = height * height / total
	if size < 1 {
		size = 1
	}
	maxYoff := total - height
	start = yoff * (height - size) / maxYoff
	if start > height-size {
		start = height - size
	}
	return start, size
}

func scrollbarCell(row, total, height, thumbStart, thumbSize int) string {
	if total <= height {
		return " "
	}
	if row >= thumbStart && row < thumbStart+thumbSize {
		return scrollThumbStyle.Render("█")
	}
	return scrollTrackStyle.Render("│")
}

// transcriptCaret maps a screen cell (x, y) in the transcript region to an
// absolute content position, clamping to the visible window.
func (m chatTUI) transcriptCaret(x, y int) selPos {
	h := m.viewport.Height()
	if y < 0 {
		y = 0
	}
	if y > h-1 {
		y = h - 1
	}
	if x < 0 {
		x = 0
	}
	if cw := m.viewport.Width(); x > cw {
		x = cw
	}
	return selPos{line: m.viewport.YOffset() + y, col: x}
}
