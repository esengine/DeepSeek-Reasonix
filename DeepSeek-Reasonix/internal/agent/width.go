package agent

import (
	"regexp"
	"strings"

	"github.com/mattn/go-runewidth"
)

// ansiSGR matches ANSI Select-Graphic-Rendition sequences (\e[…m). Width
// measurement strips these so styled streamed text still gets counted by its
// visible column footprint.
var ansiSGR = regexp.MustCompile("\x1b\\[[0-9;]*m")

// visibleWidth returns the column count of s after stripping ANSI SGR codes.
// Delegates to go-runewidth so emoji, fullwidth forms, and ZWJ sequences all
// measure correctly — a hand-rolled CJK-only table missed every emoji range
// and made the streamed-text row count drift on emoji-heavy answers.
func visibleWidth(s string) int {
	return runewidth.StringWidth(ansiSGR.ReplaceAllString(s, ""))
}

// streamedRows counts how many rows the cursor has descended after raw text
// of length s was printed at the given terminal width. Used by the markdown
// redraw to know how far up to move before clearing. Each \n descends one
// row; lines whose visible width exceeds the terminal width descend an extra
// row per wrap. A line exactly the terminal width does not wrap on its own —
// terminals "lazy-wrap" only when the next visible character lands.
func streamedRows(s string, width int) int {
	if width <= 0 {
		width = 80
	}
	rows := 0
	for _, line := range strings.Split(s, "\n") {
		if w := visibleWidth(line); w > 0 {
			rows += (w - 1) / width
		}
	}
	rows += strings.Count(s, "\n")
	return rows
}
