package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/frontend/termrender"
)

// clusterReport is what a terminal answering the mode 2027 query says when it
// draws grapheme clusters; the renderer switches its count on it, so a
// terminal measured to cluster gets the same switch without answering.
var clusterReport = tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeSet}

// noteCells follows the renderer's own switch: bubbletea counts per rune
// until the terminal answers the mode 2027 query, grapheme clusters after.
func noteCells(msg tea.ModeReportMsg) {
	if msg.Mode != ansi.ModeUnicodeCore {
		return
	}
	switch msg.Value {
	case ansi.ModeReset, ansi.ModeSet, ansi.ModePermanentlySet:
		termrender.SetCells(ansi.GraphemeWidth)
	}
}

// cursorColumn reads the 1-based column out of a cursor position report
// (CSI row ; col R) anywhere in s.
func cursorColumn(s string) (int, bool) {
	for {
		_, rest, ok := strings.Cut(s, "\x1b[")
		if !ok {
			return 0, false
		}
		body, _, ok := strings.Cut(rest, "R")
		if !ok {
			return 0, false
		}
		if row, col, found := strings.Cut(body, ";"); found {
			if _, err := strconv.Atoi(row); err == nil {
				if c, err := strconv.Atoi(col); err == nil && c > 0 {
					return c, true
				}
			}
		}
		s = rest
	}
}
