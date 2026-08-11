package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	"reasonix/internal/control"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestConfigureCLIThemeSwitchesModeAndDefaultStyle(t *testing.T) {
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256

	configureCLITheme("light")
	if activeCLITheme.name != "light" || activeCLITheme.style != "sandstone" {
		t.Fatalf("light theme = %s/%s, want light/sandstone", activeCLITheme.name, activeCLITheme.style)
	}
	if got := accent("x"); !strings.HasPrefix(got, "\033[38;5;173m") {
		t.Fatalf("light default accent = %q, want sandstone xterm 173", got)
	}

	configureCLITheme("dark")
	if activeCLITheme.name != "dark" || activeCLITheme.style != "graphite" {
		t.Fatalf("dark theme = %s/%s, want dark/graphite", activeCLITheme.name, activeCLITheme.style)
	}
	if got := accent("x"); !strings.HasPrefix(got, ansiAccent) {
		t.Fatalf("dark accent = %q, want %q", got, ansiAccent)
	}
}

func TestConfigureCLIThemeStyleOverride(t *testing.T) {
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256

	configureCLIThemeWithStyle("dark", "aurora")
	if activeCLITheme.name != "dark" || activeCLITheme.style != "aurora" {
		t.Fatalf("theme = %s/%s, want dark/aurora", activeCLITheme.name, activeCLITheme.style)
	}
	if got := accent("x"); !strings.HasPrefix(got, "\033[38;5;79m") {
		t.Fatalf("aurora accent = %q, want xterm 79", got)
	}

	configureCLITheme("glacier")
	if activeCLITheme.name != "light" || activeCLITheme.style != "glacier" {
		t.Fatalf("theme style command resolved %s/%s, want light/glacier", activeCLITheme.name, activeCLITheme.style)
	}
}

func TestConfigureCLIThemeHonorsEnvOverride(t *testing.T) {
	t.Setenv("REASONIX_THEME", "ember")
	t.Setenv("REASONIX_THEME_STYLE", "")
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256

	configureCLIThemeWithStyle("light", "glacier")
	if activeCLITheme.name != "dark" || activeCLITheme.style != "ember" {
		t.Fatalf("REASONIX_THEME override resolved %s/%s, want dark/ember", activeCLITheme.name, activeCLITheme.style)
	}
}

func TestThemeRendersAtProfileFidelity(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	configureCLIThemeWithStyle("dark", "graphite")

	activeColorProfile = colorprofile.TrueColor
	if got := accent("x"); !strings.HasPrefix(got, "\033[38;2;217;119;87m") {
		t.Fatalf("truecolor accent = %q, want 24-bit #d97757", got)
	}

	activeColorProfile = colorprofile.ANSI256
	if got := accent("x"); !strings.HasPrefix(got, ansiAccent) {
		t.Fatalf("256-colour accent = %q, want %q", got, ansiAccent)
	}

	activeColorProfile = colorprofile.NoTTY
	if got := accent("x"); got != "x" {
		t.Fatalf("no-tty accent = %q, want unstyled text", got)
	}
}

func TestThemeArgCompletion(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256
	configureCLIThemeWithStyle("dark", "graphite")

	m := newTestChatTUI()
	items, _, ok := m.slashArgItems("/theme ")
	if !ok || len(items) == 0 {
		t.Fatalf("/theme arg completion should offer themes, ok=%v n=%d", ok, len(items))
	}
	if !hasLabel(items, "auto") || !hasLabel(items, "graphite") || !hasLabel(items, "aurora") {
		t.Fatalf("/theme completion missing expected themes: %v", labels(items))
	}
}

func TestRunThemeSubcommandSwitchesAccentAndTextarea(t *testing.T) {
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256
	configureCLIThemeWithStyle("dark", "graphite")

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	if cmd := m.runThemeSubcommand("/theme aurora"); cmd == nil {
		t.Fatal("a real theme change should start the sweep")
	}
	if activeCLITheme.name != "dark" || activeCLITheme.style != "aurora" {
		t.Fatalf("current theme = %s/%s, want dark/aurora", activeCLITheme.name, activeCLITheme.style)
	}
	if got := accent("x"); !strings.HasPrefix(got, "\033[38;5;79m") {
		t.Fatalf("accent = %q, want aurora xterm color", got)
	}
	if m.input.Styles().Cursor.Color == nil {
		t.Fatal("textarea cursor color was not refreshed")
	}
}

func TestParseOSC11Response(t *testing.T) {
	for _, tt := range []struct {
		name  string
		in    string
		want  terminalRGB
		light bool
	}{
		{
			name:  "black-rgb",
			in:    "\x1b]11;rgb:0000/0000/0000\a",
			want:  terminalRGB{0, 0, 0},
			light: false,
		},
		{
			name:  "white-rgb",
			in:    "\x1b]11;rgb:ffff/ffff/ffff\x1b\\",
			want:  terminalRGB{255, 255, 255},
			light: true,
		},
		{
			name:  "hex",
			in:    "\x1b]11;#f8f8f8\a",
			want:  terminalRGB{248, 248, 248},
			light: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseOSC11Response(tt.in)
			if !ok {
				t.Fatalf("parseOSC11Response returned !ok")
			}
			if got != tt.want {
				t.Fatalf("rgb = %+v, want %+v", got, tt.want)
			}
			if got.looksLight() != tt.light {
				t.Fatalf("looksLight = %v, want %v", got.looksLight(), tt.light)
			}
		})
	}
}

func TestAutoThemeFallsBackToColorFGBG(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.NoTTY

	t.Setenv("COLORFGBG", "0;15")
	if got := resolveCLITheme("auto").name; got != "light" {
		t.Fatalf("COLORFGBG light fallback resolved %q, want light", got)
	}

	t.Setenv("COLORFGBG", "15;0")
	if got := resolveCLITheme("auto").name; got != "dark" {
		t.Fatalf("COLORFGBG dark fallback resolved %q, want dark", got)
	}
}

func TestApplyTextareaThemeClearsCursorLineBackground(t *testing.T) {
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256

	for _, mode := range []string{"dark", "light", "auto"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "auto" {
				t.Setenv("COLORFGBG", "0;15")
			} else {
				t.Setenv("COLORFGBG", "")
			}
			configureCLITheme(mode)

			ti := textarea.New()
			applyTextareaTheme(&ti)
			styles := ti.Styles()
			emptyBG := lipgloss.NewStyle().GetBackground()

			if bg := styles.Focused.CursorLine.GetBackground(); !reflect.DeepEqual(bg, emptyBG) {
				t.Fatalf("focused cursor line background = %v, want empty", bg)
			}
			if bg := styles.Blurred.CursorLine.GetBackground(); !reflect.DeepEqual(bg, emptyBG) {
				t.Fatalf("blurred cursor line background = %v, want empty", bg)
			}
			if bg := styles.Focused.EndOfBuffer.GetBackground(); !reflect.DeepEqual(bg, emptyBG) {
				t.Fatalf("end-of-buffer background = %v, want empty", bg)
			}
			if styles.Cursor.Color == nil {
				t.Fatal("cursor color is nil with color enabled")
			}
		})
	}
}

func TestApplyTextareaThemeHonorsCursorShape(t *testing.T) {
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	prevShape := cliCursorShape
	defer func() { cliCursorShape = prevShape }()
	activeColorProfile = colorprofile.ANSI256
	configureCLITheme("dark")

	for _, tt := range []struct {
		name string
		in   string
		want tea.CursorShape
	}{
		{name: "default", in: "", want: tea.CursorBar},
		{name: "underline", in: "underline", want: tea.CursorUnderline},
		{name: "block", in: "block", want: tea.CursorBlock},
		{name: "bar", in: "bar", want: tea.CursorBar},
		{name: "unknown", in: "unknown", want: tea.CursorBar},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cliCursorShape = tt.in
			ti := textarea.New()
			applyTextareaTheme(&ti)
			if got := ti.Styles().Cursor.Shape; got != tt.want {
				t.Fatalf("cursor shape = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComposerBorderAndCursorTrackThemeAccent(t *testing.T) {
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256

	for _, theme := range cliThemeStyles {
		t.Run(theme.name, func(t *testing.T) {
			configureCLITheme(theme.name)
			want := themeLipColor(activeCLITheme.accent)
			if got := inputBoxStyle.GetBorderTopForeground(); !reflect.DeepEqual(got, want) {
				t.Fatalf("composer top border color = %v, want theme accent %v", got, want)
			}
			if got := inputBoxStyle.GetBorderBottomForeground(); !reflect.DeepEqual(got, want) {
				t.Fatalf("composer bottom border color = %v, want theme accent %v", got, want)
			}

			ti := textarea.New()
			applyTextareaTheme(&ti)
			if got := ti.Styles().Cursor.Color; !reflect.DeepEqual(got, want) {
				t.Fatalf("composer cursor color = %v, want theme accent %v", got, want)
			}
		})
	}

	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")
	empty := lipgloss.NewStyle().GetBorderTopForeground()
	if got := inputBoxStyle.GetBorderTopForeground(); !reflect.DeepEqual(got, empty) {
		t.Fatalf("NO_COLOR composer top border color = %v, want no color", got)
	}
	if got := inputBoxStyle.GetBorderBottomForeground(); !reflect.DeepEqual(got, empty) {
		t.Fatalf("NO_COLOR composer bottom border color = %v, want no color", got)
	}
}

// TestRuntimeAutoThemeDoesNotProbeStdin guards the fix for a runtime `/theme auto`
// that live-probed the terminal (raw-mode stdin read) while the TUI owned stdin,
// racing bubbletea's input reader. The switch must resolve via the COLORFGBG
// fallback instead, never invoking the probe.
func TestRuntimeAutoThemeDoesNotProbeStdin(t *testing.T) {
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	t.Setenv("COLORFGBG", "15;0")
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256

	probed := false
	defer func(prev func() (terminalRGB, bool)) { terminalProbe = prev }(terminalProbe)
	terminalProbe = func() (terminalRGB, bool) {
		probed = true
		return terminalRGB{255, 255, 255}, true
	}

	if got := setCLIThemeMode("auto").name; got != "dark" {
		t.Fatalf("auto with COLORFGBG=15;0 resolved %q, want dark", got)
	}
	if probed {
		t.Fatal("runtime /theme auto probed the terminal while the TUI owns stdin")
	}

	withTerminalProbe(func() {
		if got := resolveCLITheme("auto").name; got != "light" {
			t.Fatalf("opted-in probe resolved %q, want light", got)
		}
	})
	if !probed {
		t.Fatal("withTerminalProbe should be the one path that reaches the terminal")
	}
}

func restoreThemeForTest(prevColor colorprofile.Profile, prevTheme cliPalette) {
	activeColorProfile = prevColor
	activeCLITheme = prevTheme
	refreshCLIStyles()
}
