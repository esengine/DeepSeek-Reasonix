package cli

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"reasonix/internal/control"
)

func TestThemeSweepFrameRepaintsWithoutMutatingLiveModel(t *testing.T) {
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.TrueColor
	configureCLITheme("midnight")
	original := activeCLITheme
	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.started = true
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(chatTUI)
	m.commitTranscriptSource(transcriptSource{kind: transcriptSourceUser, raw: "a user message"})
	m.commitTranscriptSource(transcriptSource{kind: transcriptSourceMarkdown, raw: "a response"})
	m.notice("a notice")
	m.refreshRuntimeTheme()
	before, wrapped := slices.Clone(m.transcript), slices.Clone(m.wrappedLines)
	frame := m.View().Content
	light := resolveCLITheme("glacier")
	preview := m.frameWithTheme(light)
	if preview == frame || !strings.Contains(preview, themeFg(light.accent, "› a user message")) || !strings.Contains(preview, themeFg(light.faint, "  · a notice")) {
		t.Fatal("snapshot did not repaint the user message and notice with the requested palette")
	}
	if activeCLITheme != original || !reflect.DeepEqual(before, m.transcript) || !reflect.DeepEqual(wrapped, m.wrappedLines) || m.View().Content != frame {
		t.Fatal("snapshot mutated the live theme, transcript, or viewport")
	}
	if m.startThemeSweep(original, light) == nil {
		t.Fatal("expected a sweep in a color terminal")
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	if next.(chatTUI).themeSweep != nil {
		t.Fatal("resize retained a frame captured at the old width")
	}
}

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
