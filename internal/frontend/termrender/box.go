package termrender

import (
	"strings"
)

// VisibleWidth returns the printable column width of s: ANSI SGR codes are
// ignored and wide / grapheme-cluster characters (CJK, emoji ZWJ sequences,
// keycaps, flags) are each counted as the cells they occupy. Thin wrapper over
// x/ansi (already in the dep tree via bubbletea/lipgloss) so call sites read
// intent rather than re-deriving the strip-and-measure dance.
func VisibleWidth(s string) int {
	return Cells().StringWidth(s)
}

// PadRight returns s padded with spaces on the right until it occupies w
// terminal columns (visible width, not bytes). Strings already at or beyond
// width are returned unchanged. Use this instead of fmt's %-Ns when content
// may contain CJK or ANSI SGR codes.
func PadRight(s string, w int) string {
	pad := w - VisibleWidth(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

// Boxed wraps content in a rounded box drawn with the brand accent. Width
// auto-fits the longest line plus one column of padding on each side. The
// result always ends with a trailing newline so callers can Print it directly.
func Boxed(lines []string) string {
	inner := 0
	for _, l := range lines {
		if w := VisibleWidth(l); w > inner {
			inner = w
		}
	}
	inner += 2 // one space of padding on each side
	bar := strings.Repeat("─", inner)

	var b strings.Builder
	b.WriteString(Accent("╭" + bar + "╮"))
	b.WriteByte('\n')
	for _, l := range lines {
		gap := max(inner-VisibleWidth(l)-2, 0)
		b.WriteString(Accent("│"))
		b.WriteByte(' ')
		b.WriteString(l)
		b.WriteString(strings.Repeat(" ", gap))
		b.WriteByte(' ')
		b.WriteString(Accent("│"))
		b.WriteByte('\n')
	}
	b.WriteString(Accent("╰" + bar + "╯"))
	b.WriteByte('\n')
	return b.String()
}
