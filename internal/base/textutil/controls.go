package textutil

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func hiddenControl(r rune) bool {
	return r != '\n' && r != '\t' && (isControl(r) || isBidi(r))
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
