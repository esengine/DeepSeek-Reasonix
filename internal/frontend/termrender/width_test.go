package termrender

import (
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
		if got := VisibleWidth(c.s); got != c.want {
			t.Errorf("%s: VisibleWidth(%q) = %d, want %d", c.name, c.s, got, c.want)
		}
	}
}
