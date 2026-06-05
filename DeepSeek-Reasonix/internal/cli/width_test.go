package cli

import (
	"strings"
	"testing"
)

func TestVisibleWidthGraphemeClusters(t *testing.T) {
	cases := []struct {
		name string
		s    string
		want int
	}{
		{"ascii", "abc", 3},
		{"cjk", "中文", 4},
		{"emoji", "🔥", 2},
		// x/ansi counts the VS16 keycap as 2 (emoji presentation). Terminals
		// disagree on VS16 width, but the point is consistency: wrapAnsi /
		// clampWidth now measure via the same x/ansi, so box rails and wrapping
		// agree — which a mixed uniseg(1)/ansi(2) split would break.
		{"keycap", "1️⃣", 2},
		// The regression that motivated the switch: a ZWJ family is one cluster
		// occupying one emoji's width, not the rune-by-rune sum (which was 8).
		{"zwj-family", "👨‍👩‍👧‍👦", 2},
		{"ansi-stripped", "\x1b[31mab\x1b[0m", 2},
		{"mixed", "a中🔥", 5},
	}
	for _, c := range cases {
		if got := visibleWidth(c.s); got != c.want {
			t.Errorf("%s: visibleWidth(%q) = %d, want %d", c.name, c.s, got, c.want)
		}
	}
}

// TestClampWidthHardwrap verifies clampWidth (a wrapper over ansi.Hardwrap) keeps
// every wrapped line within the column budget, hard-breaks CJK at the boundary,
// preserves ANSI escapes as zero width, and leaves in-width lines untouched.
func TestClampWidthHardwrap(t *testing.T) {
	// CJK hard-breaks at the column boundary (each is 2 cols); no line over width.
	for _, line := range strings.Split(clampWidth("中文字", 4), "\n") {
		if visibleWidth(line) > 4 {
			t.Errorf("cjk line %q exceeds width 4 (got %d)", line, visibleWidth(line))
		}
	}

	// A line already within width is returned byte-for-byte.
	if got := clampWidth("ab", 10); got != "ab" {
		t.Errorf("in-width line altered: %q", got)
	}

	// ANSI SGR escapes are zero width, so two visible chars fit a width-2 line.
	styled := clampWidth("\x1b[31mab\x1b[0m", 2)
	if visibleWidth(styled) > 2 {
		t.Errorf("styled line exceeds width 2 (got %d): %q", visibleWidth(styled), styled)
	}

	// Every wrapped line stays within the column budget.
	for _, line := range strings.Split(clampWidth(strings.Repeat("中", 10), 6), "\n") {
		if visibleWidth(line) > 6 {
			t.Errorf("line %q exceeds width 6 (got %d)", line, visibleWidth(line))
		}
	}
}
