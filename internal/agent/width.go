package agent

import (
	"regexp"

	"github.com/mattn/go-runewidth"
)

// ansiSGR matches ANSI Select-Graphic-Rendition sequences (\e[…m). Width
// measurement strips these so styled streamed text still gets counted by its
// visible column footprint.
var ansiSGR = regexp.MustCompile("\x1b\\[[0-9;]*m")

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
	
	var rows int
	var currentLineWidth int
	var inEscape bool
	
	for _, r := range s {
		switch {
		case r == '\n':
			// End of current line
			if currentLineWidth > 0 {
				rows += (currentLineWidth - 1) / width
				currentLineWidth = 0
			}
			rows++
		case r == '\x1b':
			// Start of ANSI escape sequence
			inEscape = true
		case inEscape && r == 'm':
			// End of ANSI escape sequence
			inEscape = false
		default:
			if !inEscape {
				// Only count visible characters
				w := runewidth.RuneWidth(r)
				currentLineWidth += w
			}
		}
	}
	
	// Add remaining line
	if currentLineWidth > 0 {
		rows += (currentLineWidth - 1) / width
	}
	
	return rows
}
