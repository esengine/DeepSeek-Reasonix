package termrender

import (
	"strings"
	"time"

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
	if d := w - VisibleWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}
