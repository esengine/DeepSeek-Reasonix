package cli

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	themeSweepInterval = 16 * time.Millisecond
	themeSweepFrames   = 18
	themeSweepMinWidth = 24
)

// themeSweep wipes the incoming palette across the frame from left to right.
// Both frames are rendered once up front: the transcript is frozen for the
// duration, so each step is column slicing only.
type themeSweep struct {
	before []string
	after  []string
	col    int
	step   int
	width  int
}

type themeSweepTickMsg struct{}

func themeSweepTick() tea.Cmd {
	return tea.Tick(themeSweepInterval, func(time.Time) tea.Msg { return themeSweepTickMsg{} })
}

// startThemeSweep returns nil when the terminal or the moment cannot carry the
// animation, and the caller keeps the plain instant switch.
func (m *chatTUI) startThemeSweep(from, to cliPalette) tea.Cmd {
	if !colorOn() || m.width < themeSweepMinWidth || m.state == tuiRunning {
		return nil
	}
	if from.name == to.name && from.style == to.style {
		return nil
	}
	before := strings.Split(m.frameWithTheme(from), "\n")
	after := strings.Split(m.frameWithTheme(to), "\n")
	if len(before) != len(after) {
		return nil
	}
	step := max(m.width/themeSweepFrames, 1)
	m.themeSweep = &themeSweep{
		before: before,
		after:  after,
		col:    step,
		step:   step,
		width:  m.width,
	}
	return themeSweepTick()
}

// frameWithTheme renders the whole view under a palette other than the active
// one. The value receiver keeps the swapped-in sweep state local to this call.
func (m chatTUI) frameWithTheme(p cliPalette) string {
	m.themeSweep = nil
	prev := activeCLITheme
	activeCLITheme = p
	refreshCLIStyles()
	defer func() {
		activeCLITheme = prev
		refreshCLIStyles()
	}()
	return m.View().Content
}

func (s *themeSweep) advance() bool {
	s.col += s.step
	return s.col < s.width
}

func (s *themeSweep) render() string {
	rows := make([]string, len(s.after))
	for i := range s.after {
		rows[i] = s.composeRow(s.after[i], s.before[i])
	}
	return strings.Join(rows, "\n")
}

// composeRow builds the row to an exact cell count. Slicing on an odd column
// cannot split a double-width rune, so both sides are re-clamped and padded;
// otherwise a CJK transcript pushes the boundary out of vertical alignment.
func (s *themeSweep) composeRow(after, before string) string {
	lead := min(max(s.col, 0), s.width)
	row := padCells(ansi.Truncate(after, lead, ""), lead)
	if lead == s.width {
		return row
	}
	tailWidth := s.width - lead
	tail := ansi.Truncate(ansi.TruncateLeft(before, lead, ""), tailWidth, "")
	return row + padCells(tail, tailWidth)
}

// padCells restores the exact column count after a truncation that landed on a
// double-width cell.
func padCells(s string, w int) string {
	if d := w - visibleWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}
