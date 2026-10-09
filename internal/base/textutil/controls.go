package textutil

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func hiddenControl(r rune) bool {
	switch {
	case r == '\n' || r == '\t':
		return false
	case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
		return true
	case r == 0x061c || r == 0x200e || r == 0x200f:
		return true
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}

// HasHiddenControls reports whether s holds a control character other than
// newline and tab, or a bidirectional override: text that renders differently
// from what it says.
func HasHiddenControls(s string) bool { return strings.IndexFunc(s, hiddenControl) >= 0 }

// StripHiddenControls removes escape sequences and what HasHiddenControls finds.
func StripHiddenControls(s string) string {
	return strings.Map(func(r rune) rune {
		if hiddenControl(r) {
			return -1
		}
		return r
	}, ansi.Strip(s))
}
