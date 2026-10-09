// Display text that came from somewhere we do not control — an MCP server's
// own description of itself, a tool's docstring — reaches a terminal and a
// settings pane. Both need the same thing done to it first, and doing it in
// each frontend is how the two drift.
package textutil

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// SanitizeDisplay strips escape sequences and control characters from external
// text and collapses the remaining whitespace to single spaces, so one string
// cannot repaint a terminal or smuggle line breaks into a one-line row.
func SanitizeDisplay(s string) string {
	s = ansi.Strip(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f:
			// Drop remaining C0 controls and DEL.
		case r >= 0x80 && r <= 0x9f:
			// Drop C1 controls (including after partial decode).
		case unicode.Is(unicode.Cc, r):
			// Other control categories.
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// SanitizeLaunch is SanitizeDisplay for a line the user is asked to approve:
// it also drops format characters (bidi overrides, zero-width), which would
// let the text on screen read differently from what is run.
func SanitizeLaunch(s string) string {
	s = SanitizeDisplay(s)
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}
