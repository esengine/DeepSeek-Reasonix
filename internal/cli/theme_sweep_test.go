package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
)

func testSweepRows(width int) (before, after []string) {
	// mix wide runes, pre-styled text and plain text
	for range 4 {
		before = append(before, strings.Repeat("宽", width/2), strings.Repeat("a", width),
			themeFg(activeCLITheme.warn, strings.Repeat("s", width)))
		after = append(after, strings.Repeat("窄", width/2), strings.Repeat("b", width),
			themeFg(activeCLITheme.info, strings.Repeat("t", width)))
	}
	return before, after
}

func TestThemeSweepHoldsExactRowWidth(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	configureCLIThemeWithStyle("dark", "graphite")

	for _, profile := range []colorprofile.Profile{colorprofile.ANSI256, colorprofile.TrueColor} {
		activeColorProfile = profile
		const width = 40
		before, after := testSweepRows(width)
		s := &themeSweep{before: before, after: after, step: 3, width: width}
		for s.col = 0; s.col <= width; s.col++ {
			for i, row := range strings.Split(s.render(), "\n") {
				if got := visibleWidth(row); got != width {
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

func TestThemeSweepSkippedWhenTerminalCannotCarryIt(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	configureCLIThemeWithStyle("dark", "graphite")
	dark := activeCLITheme
	light := resolveCLIThemeWithStyle("light", "sandstone")

	for _, tt := range []struct {
		name    string
		profile colorprofile.Profile
		width   int
		state   tuiState
		from    cliPalette
		to      cliPalette
	}{
		{name: "no colour", profile: colorprofile.NoTTY, width: 80, from: dark, to: light},
		{name: "too narrow", profile: colorprofile.ANSI256, width: 8, from: dark, to: light},
		{name: "turn running", profile: colorprofile.ANSI256, width: 80, state: tuiRunning, from: dark, to: light},
		{name: "same theme", profile: colorprofile.ANSI256, width: 80, from: dark, to: dark},
	} {
		t.Run(tt.name, func(t *testing.T) {
			activeColorProfile = tt.profile
			m := newTestChatTUI()
			m.width = tt.width
			m.state = tt.state
			if cmd := m.startThemeSweep(tt.from, tt.to); cmd != nil {
				t.Fatal("sweep should be skipped, switch must stay instant")
			}
			if m.themeSweep != nil {
				t.Fatal("skipped sweep must not freeze the frame")
			}
		})
	}
}
