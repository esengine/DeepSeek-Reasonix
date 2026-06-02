package cli

import (
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"
)

func TestConfigureCLIThemeSwitchesModeAndDefaultStyle(t *testing.T) {
	t.Setenv("COLORTERM", "")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	defer restoreThemeForTest(colorEnabled, activeCLITheme)
	colorEnabled = true

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
	t.Setenv("COLORTERM", "")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	defer restoreThemeForTest(colorEnabled, activeCLITheme)
	colorEnabled = true

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
	t.Setenv("COLORTERM", "")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("REASONIX_THEME", "ember")
	t.Setenv("REASONIX_THEME_STYLE", "")
	defer restoreThemeForTest(colorEnabled, activeCLITheme)
	colorEnabled = true

	configureCLIThemeWithStyle("light", "glacier")
	if activeCLITheme.name != "dark" || activeCLITheme.style != "ember" {
		t.Fatalf("REASONIX_THEME override resolved %s/%s, want dark/ember", activeCLITheme.name, activeCLITheme.style)
	}
}

func TestThemeArgCompletion(t *testing.T) {
	defer restoreThemeForTest(colorEnabled, activeCLITheme)
	colorEnabled = true
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
	t.Setenv("COLORTERM", "")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	defer restoreThemeForTest(colorEnabled, activeCLITheme)
	colorEnabled = true
	configureCLIThemeWithStyle("dark", "graphite")

	m := newTestChatTUI()
	m.runThemeSubcommand("/theme aurora")
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
	defer restoreThemeForTest(colorEnabled, activeCLITheme)
	colorEnabled = false

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
	t.Setenv("COLORTERM", "")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	defer restoreThemeForTest(colorEnabled, activeCLITheme)
	colorEnabled = true

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

// TestRuntimeAutoThemeDoesNotProbeStdin guards the fix for a runtime `/theme auto`
// that live-probed the terminal (raw-mode stdin read) while the TUI owned stdin,
// racing bubbletea's input reader. The switch must resolve via the COLORFGBG
// fallback instead, never invoking the probe.
func TestRuntimeAutoThemeDoesNotProbeStdin(t *testing.T) {
	t.Setenv("COLORTERM", "")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	t.Setenv("COLORFGBG", "15;0")
	defer restoreThemeForTest(colorEnabled, activeCLITheme)
	colorEnabled = true

	prev := queryTerminalBackgroundForTheme
	defer func() { queryTerminalBackgroundForTheme = prev }()
	probed := false
	queryTerminalBackgroundForTheme = func() (terminalRGB, bool) {
		probed = true
		return terminalRGB{}, false
	}

	if got := setCLIThemeMode("auto").name; got != "dark" {
		t.Fatalf("auto with COLORFGBG=15;0 resolved %q, want dark", got)
	}
	if probed {
		t.Fatal("runtime /theme auto probed the terminal while the TUI owns stdin")
	}
}

func restoreThemeForTest(prevColor bool, prevTheme cliPalette) {
	colorEnabled = prevColor
	activeCLITheme = prevTheme
	refreshCLIStyles()
}
