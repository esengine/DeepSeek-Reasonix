package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/frontend/termrender"
)

// bottom is the pinned region under the transcript: the task list, a prompt
// waiting on the user, the completion menu, the working line, the composer,
// and the footer. composerAt is the composer's first row, or -1 when hidden.
type bottom struct {
	rows       []string
	composerAt int
}

func (m *model) bottomLines() bottom {
	var rows []string
	rows = append(rows, m.todoLines()...)
	open := m.tr.OpenPrompt()
	switch {
	case m.skills != nil:
		rows = append(rows, m.skillsPanel()...)
	case m.quick != nil:
		rows = append(rows, m.quickPanel()...)
	case m.mcp != nil:
		rows = append(rows, m.mcpPanelLines()...)
	case m.picker != nil:
		rows = append(rows, m.pickerPanel()...)
	case m.rewind != nil:
		rows = append(rows, m.rewindPanel()...)
	case m.copying != nil:
		rows = append(rows, m.copyPanel()...)
	case m.clearing != nil:
		rows = append(rows, m.clearPanel()...)
	case m.setup != nil:
		rows = append(rows, m.setupPanel()...)
	case open == nil:
	case open.Kind == ItemAsk:
		rows = append(rows, m.askPanel(open)...)
	default:
		rows = append(rows, m.approvalPanel(open)...)
	}
	rows = append(rows, m.menuLines()...)
	if w := m.workingLine(); w != "" {
		rows = append(rows, w)
	}
	at := -1
	if m.composerShown() {
		at = len(rows)
		rows = append(rows, m.composerLines()...)
	}
	return bottom{rows: append(rows, m.statusBlock()...), composerAt: at}
}

// composerShown reports whether the composer is drawn: no panel holds the
// bottom region, or the open ask card is taking a typed answer.
func (m *model) composerShown() bool {
	open := m.tr.OpenPrompt()
	return m.picker == nil && m.skills == nil && m.quick == nil && m.mcp == nil && m.rewind == nil && m.copying == nil && m.clearing == nil && m.setup == nil && (open == nil || (open.Kind == ItemAsk && m.ask != nil && m.ask.entering()))
}

// View draws the frame. Full screen, the transcript is a viewport above the
// bottom region; otherwise everything settled is already in the terminal's
// scrollback and only what is still changing is drawn above it. A terminal
// counting per rune is drawn stand-ins for what it folds, or a row it draws
// wider than counted wraps and every row below it shifts.
func (m *model) View() tea.View {
	v := m.frame()
	if termrender.Cells() == ansi.WcWidth {
		v.Content = splitClusters(v.Content)
	}
	v.Content = m.glyphs.apply(v.Content)
	return v
}

func (m *model) frame() tea.View {
	b := m.bottomLines()
	if m.scr != nil {
		return m.fullView(b.rows, b.composerAt)
	}
	// The live region is redrawn in place, so the whole frame must stay inside
	// the screen: a frame taller than the screen scrolls, and the rows that
	// scrolled off can no longer be cleared before the next print.
	live := m.liveLines()
	room := max(min(m.height/2, m.height-len(b.rows)-1), 0)
	if len(live) > room {
		live = live[len(live)-room:]
	}
	lines := append(live, b.rows...)
	// A row as wide as the terminal wraps on its own, which adds a row the
	// renderer does not know it drew.
	for i, l := range lines {
		lines[i] = termrender.Truncate(l, max(m.width-1, 1), "")
	}
	m.frameRows = len(lines)
	v := tea.NewView(strings.Join(lines, "\n"))
	if c := m.composer.Cursor(); c != nil && b.composerAt >= 0 {
		c.X += 3
		c.Y += len(live) + b.composerAt + 1
		v.Cursor = c
	}
	return v
}

func (m *model) liveLines() []string { return m.liveLinesRail(m.scrollbarHidden()) }

// liveLinesRail draws what is still changing at the width it will occupy: the
// transcript's beside the scrollbar column full screen, and the live frame's
// otherwise, so a row is not wrapped once more when it joins the transcript.
func (m *model) liveLinesRail(hideRail bool) []string {
	width := m.contentWidth()
	var out []string
	for i := range m.tr.Items {
		it := &m.tr.Items[i]
		if m.committed[it.ID] {
			continue
		}
		switch {
		case it.Kind == ItemUser && it.Pending:
			out = append(out, termrender.Dim("  ⧗ "+oneLine(it.Text, width-6)))
		case it.Kind == ItemSay:
			shown := m.sayShown[it.ID]
			if rest := it.Text[min(shown, len(it.Text)):]; rest != "" {
				out = append(out, strings.Split(renderSayPart(rest, shown == 0, width, hideRail), "\n")...)
			}
		case (it.Kind == ItemApproval || it.Kind == ItemAsk) && it.Verdict == "":
		case m.hidden(it) || it.bookkeeping():
		default:
			if r := renderItem(it, width, 0, hideRail); r != "" {
				out = append(out, strings.Split(r, "\n")...)
			}
		}
	}
	return out
}
