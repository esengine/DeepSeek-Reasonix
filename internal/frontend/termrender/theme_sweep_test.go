package termrender

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
)

func testSweepRows(width int) (before, after []string) {
	// mix wide runes, pre-styled text and plain text
	for range 4 {
		before = append(before, strings.Repeat("宽", width/2), strings.Repeat("a", width),
			ThemeFg(activeTheme.Warn, strings.Repeat("s", width)))
		after = append(after, strings.Repeat("窄", width/2), strings.Repeat("b", width),
			ThemeFg(activeTheme.Info, strings.Repeat("t", width)))
	}
	return before, after
}

func TestThemeSweepHoldsExactRowWidth(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeTheme)
	configureThemeWithStyle("dark", "graphite")

	for _, profile := range []colorprofile.Profile{colorprofile.ANSI256, colorprofile.TrueColor} {
		activeColorProfile = profile
		const width = 40
		before, after := testSweepRows(width)
		s := &themeSweep{before: before, after: after, step: 3, width: width}
		for s.col = 0; s.col <= width; s.col++ {
			for i, row := range strings.Split(s.render(), "\n") {
				if got := VisibleWidth(row); got != width {
					t.Fatalf("%v col=%d row=%d width=%d, want %d: %q", profile, s.col, i, got, width, row)
				}
			}
		}
	}
}

func TestThemeSweepAdvanceTerminates(t *testing.T) {
	s := &themeSweep{before: []string{"a"}, after: []string{"b"}, step: 3, width: 80}
	steps := 0
	for s.advance() {
		steps++
		if steps > themeSweepFrames*4 {
			t.Fatal("sweep never reached the right edge")
		}
	}
	if s.col < s.width {
		t.Fatalf("sweep stopped at col %d, want >= %d", s.col, s.width)
	}
}
